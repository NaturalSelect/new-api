package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var systemOneImageFormats = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
}

type decisionContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type decisionNoulCriteria struct {
	TrueDescription  string `json:"true_description,omitempty"`
	FalseDescription string `json:"false_description,omitempty"`
}

type decisionQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type decisionRequest struct {
	Model     string                      `json:"model"`
	Input     any                         `json:"input"`
	Questions map[string]decisionQuestion `json:"questions"`
}

type systemOneUsage struct {
	InputTokens  int   `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type decisionAnswerField struct {
	name string
	kind gjson.Type
}

// NOTE: System One answers carry no confidence for noul, and Google's other answer fields are dropped.
var decisionAnswerFields = map[string][]decisionAnswerField{
	"noul":   {{"noul", gjson.Number}},
	"choice": {{"choice", gjson.String}, {"confidence", gjson.Number}, {"probabilities", gjson.JSON}},
	"score":  {{"score", gjson.Number}, {"confidence", gjson.Number}, {"probabilities", gjson.JSON}},
}

type systemOneAsset struct {
	literal string
	escaped string
	content decisionContent
	used    bool
}

// NOTE: Placeholders inside a JSON text (an object or array state) appear in their escaped form.
func (a *systemOneAsset) needles(inJSONText bool) []string {
	if inJSONText && a.escaped != a.literal {
		return []string{a.literal, a.escaped}
	}
	return []string{a.literal}
}

type systemOneSegment struct {
	text  string
	asset *systemOneAsset
}

// ConvertSystemOneToDecision rewrites a System One request as a Decisions request.
// Every error comes from the client's input.
func ConvertSystemOneToDecision(c *gin.Context, request *dto.TypeSafeRequest, upstreamModel string) ([]byte, error) {
	root := gjson.ParseBytes(request.RequestJSON)

	state := root.Get("state")
	stateText := state.Raw
	stateIsString := state.Type == gjson.String
	if stateIsString {
		stateText = state.String()
	}

	assets, err := readSystemOneAssets(c, request, root.Get("assets"))
	if err != nil {
		return nil, err
	}

	questions := make(map[string]decisionQuestion)
	var questionErr error
	root.Get("questions").ForEach(func(id, question gjson.Result) bool {
		converted, err := convertSystemOneQuestion(id.String(), question)
		if err != nil {
			questionErr = err
			return false
		}
		for _, asset := range assets {
			for _, needle := range asset.needles(true) {
				if strings.Contains(question.Get("instructions").Raw, needle) || strings.Contains(question.Get("criteria").Raw, needle) {
					questionErr = errors.New("assets inside questions are not supported on this channel")
					return false
				}
			}
		}
		questions[id.String()] = converted
		return true
	})
	if questionErr != nil {
		return nil, questionErr
	}

	var input any = stateText
	if len(assets) > 0 {
		blocks := make([]decisionContent, 0, len(assets)+1)
		for _, segment := range splitStateByAssets(stateText, !stateIsString, assets) {
			if segment.asset != nil {
				blocks = append(blocks, segment.asset.content)
				continue
			}
			blocks = append(blocks, decisionContent{Type: "text", Text: segment.text})
		}
		for _, asset := range assets {
			if !asset.used {
				return nil, fmt.Errorf("asset %q is not used in state", asset.literal)
			}
		}
		input = blocks
	}

	return common.Marshal(decisionRequest{Model: upstreamModel, Input: input, Questions: questions})
}

func readSystemOneAssets(c *gin.Context, request *dto.TypeSafeRequest, node gjson.Result) ([]*systemOneAsset, error) {
	if !node.Exists() || node.Type == gjson.Null {
		return nil, nil
	}
	if !node.IsObject() {
		return nil, errors.New("assets must be an object")
	}

	type entry struct {
		key   string
		value gjson.Result
	}
	var entries []entry
	node.ForEach(func(key, value gjson.Result) bool {
		entries = append(entries, entry{key: key.String(), value: value})
		return true
	})

	var form *multipart.Form
	defer func() {
		if form != nil {
			_ = form.RemoveAll()
		}
	}()

	assets := make([]*systemOneAsset, 0, len(entries))
	for _, e := range entries {
		if e.key == "" {
			return nil, errors.New("asset placeholders must not be empty")
		}
		if !e.value.IsObject() {
			return nil, fmt.Errorf("asset %q must be an object", e.key)
		}
		if kind := e.value.Get("type"); kind.Exists() && kind.String() != "image" {
			return nil, fmt.Errorf("asset %q has type %q, only image assets are supported on this channel", e.key, kind.String())
		}

		content := decisionContent{Type: "image"}
		if part := e.value.Get("part").String(); part != "" {
			if !request.Multipart {
				return nil, fmt.Errorf("asset %q uses part %q, but the request is not multipart", e.key, part)
			}
			if form == nil {
				var err error
				form, err = common.ParseMultipartFormReusable(c)
				if err != nil {
					return nil, fmt.Errorf("invalid multipart body: %w", err)
				}
			}
			data, mimeType, err := readSystemOneImagePart(form, part)
			if err != nil {
				return nil, err
			}
			content.Data, content.MimeType = data, mimeType
		} else {
			mimeType, ok := systemOneImageFormats[strings.ToLower(e.value.Get("format").String())]
			if !ok {
				return nil, fmt.Errorf("asset %q needs format png, jpg, or jpeg", e.key)
			}
			data := e.value.Get("data_base64").String()
			if data == "" {
				return nil, fmt.Errorf("asset %q needs data_base64 or part", e.key)
			}
			content.Data, content.MimeType = data, mimeType
		}

		escaped, err := common.Marshal(e.key)
		if err != nil {
			return nil, err
		}
		assets = append(assets, &systemOneAsset{
			literal: e.key,
			escaped: strings.Trim(string(escaped), `"`),
			content: content,
		})
	}
	return assets, nil
}

func readSystemOneImagePart(form *multipart.Form, part string) (string, string, error) {
	files := form.File[part]
	if len(files) != 1 {
		return "", "", fmt.Errorf("image part %q is missing", part)
	}
	file, err := files[0].Open()
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", "", err
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/png" && mimeType != "image/jpeg" {
		return "", "", fmt.Errorf("image part %q must be a PNG or JPEG image", part)
	}
	return base64.StdEncoding.EncodeToString(data), mimeType, nil
}

// NOTE: Google joins adjacent text blocks without a separator, so the pieces of a JSON state read as one text again.
func splitStateByAssets(text string, inJSONText bool, assets []*systemOneAsset) []systemOneSegment {
	var segments []systemOneSegment
	for text != "" {
		var best *systemOneAsset
		bestIndex, bestLen := -1, 0
		for _, asset := range assets {
			for _, needle := range asset.needles(inJSONText) {
				index := strings.Index(text, needle)
				if index < 0 {
					continue
				}
				if best == nil || index < bestIndex || (index == bestIndex && len(needle) > bestLen) {
					best, bestIndex, bestLen = asset, index, len(needle)
				}
			}
		}
		if best == nil {
			break
		}
		if bestIndex > 0 {
			segments = append(segments, systemOneSegment{text: text[:bestIndex]})
		}
		segments = append(segments, systemOneSegment{asset: best})
		best.used = true
		text = text[bestIndex+bestLen:]
	}
	if text != "" {
		segments = append(segments, systemOneSegment{text: text})
	}
	return segments
}

func convertSystemOneQuestion(id string, question gjson.Result) (decisionQuestion, error) {
	kind := question.Get("type").String()
	converted := decisionQuestion{Type: kind}
	switch kind {
	case "noul", "choice", "score":
	case "box":
		return converted, errors.New("box questions are not supported on this channel")
	default:
		return converted, fmt.Errorf("question %q has unsupported type %q", id, kind)
	}

	converted.Instructions = systemOneText(question.Get("instructions"))
	if converted.Instructions == "" {
		return converted, fmt.Errorf("question %q needs instructions on this channel", id)
	}

	criteria := question.Get("criteria")
	if criteria.Type == gjson.Null {
		return converted, nil
	}
	switch kind {
	case "noul":
		if !criteria.IsObject() {
			return converted, fmt.Errorf("question %q criteria must be an object", id)
		}
		noul := decisionNoulCriteria{
			TrueDescription:  systemOneText(criteria.Get("true")),
			FalseDescription: systemOneText(criteria.Get("false")),
		}
		if noul != (decisionNoulCriteria{}) {
			converted.Criteria = noul
		}
	case "choice":
		if !criteria.IsObject() {
			return converted, fmt.Errorf("question %q criteria must be an object", id)
		}
		options := make(map[string]string)
		criteria.ForEach(func(option, description gjson.Result) bool {
			options[option.String()] = systemOneText(description)
			return true
		})
		converted.Criteria = options
	case "score":
		if !criteria.IsArray() {
			return converted, fmt.Errorf("question %q criteria must be an array", id)
		}
		levels := make([]string, 0, len(criteria.Array()))
		criteria.ForEach(func(_, description gjson.Result) bool {
			levels = append(levels, systemOneText(description))
			return true
		})
		converted.Criteria = levels
	}
	return converted, nil
}

// NOTE: Google reads text only, so an object or array description is sent as its JSON text.
func systemOneText(value gjson.Result) string {
	switch value.Type {
	case gjson.Null:
		return ""
	case gjson.String:
		return value.String()
	default:
		return value.Raw
	}
}

// SystemOneHandler turns a Decisions answer into a System One response and
// bills the input tokens Google reports.
func SystemOneHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	request, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return nil, types.NewError(fmt.Errorf("unexpected request type %T", info.Request), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}
	logger.LogDebug(c, "Gemini system one response body: %s", responseBody)

	usage := buildDecisionUsage(c, responseBody, info)
	outputTokens := int64(0)
	if tokens := gjson.GetBytes(responseBody, "usage.total_output_tokens"); tokens.Type == gjson.Number && tokens.Int() > 0 && float64(tokens.Int()) == tokens.Float() {
		outputTokens = tokens.Int()
	}

	converted, err := buildSystemOneResponse(responseBody, request, info.UpstreamModelName, systemOneUsage{InputTokens: usage.PromptTokens, OutputTokens: outputTokens})
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.IOCopyBytesGracefully(c, resp, converted)
	return usage, nil
}

// NOTE: Answers follow the request's question order, and a question id is never used as a gjson path because ids may contain dots.
func buildSystemOneResponse(responseBody []byte, request *dto.TypeSafeRequest, model string, usage systemOneUsage) ([]byte, error) {
	googleAnswers := gjson.GetBytes(responseBody, "answers")
	if !googleAnswers.IsObject() {
		return nil, errors.New("gemini decision response is missing answers")
	}
	answersByID := make(map[string]gjson.Result)
	googleAnswers.ForEach(func(id, answer gjson.Result) bool {
		answersByID[id.String()] = answer
		return true
	})

	modelJSON, err := common.Marshal(model)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString(`{"model":`)
	out.Write(modelJSON)
	out.WriteString(`,"answers":{`)

	var buildErr error
	first := true
	gjson.GetBytes(request.RequestJSON, "questions").ForEach(func(id, question gjson.Result) bool {
		answer, err := convertDecisionAnswer(answersByID[id.String()], question)
		if err == nil {
			var idJSON []byte
			if idJSON, err = common.Marshal(id.String()); err == nil {
				if !first {
					out.WriteByte(',')
				}
				first = false
				out.Write(idJSON)
				out.WriteByte(':')
				out.Write(answer)
			}
		}
		if err != nil {
			buildErr = fmt.Errorf("question %q: %w", id.String(), err)
			return false
		}
		return true
	})
	if buildErr != nil {
		return nil, buildErr
	}

	usageJSON, err := common.Marshal(usage)
	if err != nil {
		return nil, err
	}
	out.WriteString(`},"usage":`)
	out.Write(usageJSON)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func convertDecisionAnswer(answer, question gjson.Result) ([]byte, error) {
	kind := question.Get("type").String()
	if !answer.IsObject() || answer.Get("type").String() != kind {
		return nil, errors.New("gemini decision response has no matching answer")
	}

	kindJSON, err := common.Marshal(kind)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{"type": kindJSON}
	for _, field := range decisionAnswerFields[kind] {
		value := answer.Get(field.name)
		if value.Type != field.kind || (field.kind == gjson.JSON && !value.IsObject()) {
			return nil, fmt.Errorf("gemini decision answer has an invalid %s", field.name)
		}
		fields[field.name] = json.RawMessage(value.Raw)
	}

	if kind == "score" {
		legend, err := systemOneLegend(question.Get("criteria"))
		if err != nil {
			return nil, err
		}
		fields["legend"] = legend
	}
	return common.Marshal(fields)
}

// NOTE: The legend repeats the request's own level descriptions, which Google does not return.
func systemOneLegend(criteria gjson.Result) (json.RawMessage, error) {
	if !criteria.IsArray() {
		return nil, errors.New("score question has no criteria")
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for index, level := range criteria.Array() {
		if index > 0 {
			out.WriteByte(',')
		}
		fmt.Fprintf(&out, `"%d":`, index)
		out.WriteString(level.Raw)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
