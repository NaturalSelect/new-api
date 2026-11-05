package dto

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	TypeSafeMaxInputTokens         = 65792
	TypeSafeLongContextInputTokens = 262400
	TypeSafeLongContextHeader      = "TypeSafe-Danger-Zone-Long-Context"
	TypeSafeImagesHeader           = "TypeSafe-Danger-Zone-Images"

	typeSafeRawBodyKey = "typesafe_raw_body"
)

var typeSafeRequestFields = map[string]struct{}{
	"model":     {},
	"state":     {},
	"questions": {},
	"assets":    {},
}

// TypeSafeRequest keeps the client's JSON text untouched: TypeSafe reads the
// state object in member order, so the request is never decoded into a map or
// struct and re-marshaled.
type TypeSafeRequest struct {
	Model       string
	RequestJSON []byte
	Multipart   bool
	LongContext bool
	NeedsImages bool
}

type typeSafeRawBody struct {
	raw       []byte
	multipart bool
}

func (r *TypeSafeRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *TypeSafeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		TokenType:   types.TokenTypeTokenizer,
		CombineText: string(r.RequestJSON),
	}
}

// SetModelName replaces only the value of the model member, so every other
// byte of the request, including member order, stays as the client wrote it.
func (r *TypeSafeRequest) SetModelName(modelName string) {
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

func (r *TypeSafeRequest) MarshalJSON() ([]byte, error) {
	return r.RequestJSON, nil
}

// ReadTypeSafeRequestJSON returns the JSON request text: the raw body for a
// JSON request, or the "request" field of a multipart request.
func ReadTypeSafeRequestJSON(c *gin.Context) (raw []byte, multipart bool, err error) {
	if cached, ok := c.Get(typeSafeRawBodyKey); ok {
		if body, ok := cached.(typeSafeRawBody); ok {
			return body.raw, body.multipart, nil
		}
	}

	if strings.HasPrefix(strings.ToLower(c.ContentType()), "multipart/") {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, false, fmt.Errorf("invalid multipart body: %w", err)
		}
		defer form.RemoveAll()
		fields := form.Value["request"]
		if len(fields) != 1 {
			return nil, false, errors.New("a multipart request needs one request field with the JSON request")
		}
		raw, multipart = []byte(fields[0]), true
	} else {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, false, err
		}
		raw, err = storage.Bytes()
		if err != nil {
			return nil, false, err
		}
	}

	c.Set(typeSafeRawBodyKey, typeSafeRawBody{raw: raw, multipart: multipart})
	return raw, multipart, nil
}

func ParseTypeSafeRequest(c *gin.Context) (*TypeSafeRequest, error) {
	raw, multipart, err := ReadTypeSafeRequestJSON(c)
	if err != nil {
		return nil, err
	}
	longContext := strings.EqualFold(strings.TrimSpace(c.GetHeader(TypeSafeLongContextHeader)), "true")
	return NewTypeSafeRequest(raw, multipart, longContext)
}

// NewTypeSafeRequest checks only the top level of the request. Question
// criteria are left to TypeSafe, whose {"detail": ...} errors reach the client
// as sent.
func NewTypeSafeRequest(raw []byte, multipart, longContext bool) (*TypeSafeRequest, error) {
	if !gjson.ValidBytes(raw) {
		return nil, errors.New("request body must be valid JSON")
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return nil, errors.New("request body must be a JSON object")
	}

	var fieldErr error
	root.ForEach(func(key, _ gjson.Result) bool {
		if _, ok := typeSafeRequestFields[key.String()]; !ok {
			fieldErr = fmt.Errorf("TypeSafe System One does not accept the request field %s", key.String())
			return false
		}
		return true
	})
	if fieldErr != nil {
		return nil, fieldErr
	}

	model := root.Get("model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return nil, errors.New("model is required")
	}

	state := root.Get("state")
	if state.Type != gjson.String && !state.IsObject() && !state.IsArray() {
		return nil, errors.New("state must be text, an object, or an array")
	}

	questions := root.Get("questions")
	if !questions.IsObject() {
		return nil, errors.New("questions must be a nonempty object")
	}
	needsImages := root.Get("assets").Exists()
	questionCount := 0
	var questionErr error
	questions.ForEach(func(_, question gjson.Result) bool {
		questionCount++
		questionType := question.Get("type")
		if !question.IsObject() || questionType.Type != gjson.String {
			questionErr = errors.New("each question must have type choice, score, noul, or box")
			return false
		}
		switch questionType.String() {
		case "box":
			needsImages = true
		case "choice", "score", "noul":
		default:
			questionErr = errors.New("each question must have type choice, score, noul, or box")
			return false
		}
		return true
	})
	if questionErr != nil {
		return nil, questionErr
	}
	if questionCount == 0 {
		return nil, errors.New("questions must be a nonempty object")
	}

	return &TypeSafeRequest{
		Model:       model.String(),
		RequestJSON: raw,
		Multipart:   multipart,
		LongContext: longContext,
		NeedsImages: needsImages,
	}, nil
}
