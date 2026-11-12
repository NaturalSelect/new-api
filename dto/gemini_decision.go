package dto

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var geminiDecisionFileTypes = map[string]types.FileType{
	"image":    types.FileTypeImage,
	"audio":    types.FileTypeAudio,
	"video":    types.FileTypeVideo,
	"document": types.FileTypeFile,
}

// NOTE: The body is relayed as written because the question schema differs per type and keeps evolving.
type GeminiDecisionRequest struct {
	Model       string
	RequestJSON []byte
}

func (r *GeminiDecisionRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *GeminiDecisionRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts []string
	var files []*types.FileMeta

	collectGeminiDecisionContent(gjson.GetBytes(r.RequestJSON, "input"), &texts, &files)
	gjson.GetBytes(r.RequestJSON, "questions").ForEach(func(_, question gjson.Result) bool {
		collectGeminiDecisionContent(question.Get("instructions"), &texts, &files)
		question.Get("criteria").ForEach(func(_, description gjson.Result) bool {
			texts = append(texts, description.String())
			return true
		})
		return true
	})

	return &types.TokenCountMeta{
		TokenType:   types.TokenTypeTokenizer,
		CombineText: strings.Join(texts, "\n"),
		Files:       files,
	}
}

// NOTE: Only the model value changes, so every other byte of the request stays as the client wrote it.
func (r *GeminiDecisionRequest) SetModelName(modelName string) {
	if modelName == "" || modelName == r.Model {
		return
	}
	updated, err := sjson.SetBytes(r.RequestJSON, "model", modelName)
	if err != nil {
		return
	}
	r.RequestJSON = updated
	r.Model = modelName
}

func (r *GeminiDecisionRequest) MarshalJSON() ([]byte, error) {
	return r.RequestJSON, nil
}

func ParseGeminiDecisionRequest(c *gin.Context) (*GeminiDecisionRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	return NewGeminiDecisionRequest(raw)
}

// NOTE: Questions, criteria and limits are validated by Google, whose 400 errors reach the client.
func NewGeminiDecisionRequest(raw []byte) (*GeminiDecisionRequest, error) {
	if !gjson.ValidBytes(raw) {
		return nil, errors.New("request body must be valid JSON")
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return nil, errors.New("request body must be a JSON object")
	}
	model := root.Get("model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, errors.New("model is required")
	}
	return &GeminiDecisionRequest{Model: model.String(), RequestJSON: raw}, nil
}

// NOTE: input and instructions each accept a string, one content block, or an array of blocks.
func collectGeminiDecisionContent(value gjson.Result, texts *[]string, files *[]*types.FileMeta) {
	switch {
	case value.Type == gjson.String:
		*texts = append(*texts, value.String())
	case value.IsArray():
		value.ForEach(func(_, block gjson.Result) bool {
			collectGeminiDecisionContent(block, texts, files)
			return true
		})
	case value.IsObject():
		blockType := value.Get("type").String()
		if blockType == "text" {
			*texts = append(*texts, value.Get("text").String())
			return
		}
		fileType, ok := geminiDecisionFileTypes[blockType]
		if !ok {
			return
		}
		var source types.FileSource
		if data := value.Get("data").String(); data != "" {
			source = types.NewBase64FileSource(data, value.Get("mime_type").String())
		} else if uri := value.Get("uri").String(); uri != "" {
			source = types.NewURLFileSource(uri)
		}
		if source != nil {
			*files = append(*files, types.NewFileMeta(fileType, source))
		}
	}
}
