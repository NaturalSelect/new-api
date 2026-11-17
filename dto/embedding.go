package dto

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type EmbeddingOptions struct {
	Seed             int      `json:"seed,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopK             int      `json:"top_k,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	NumPredict       int      `json:"num_predict,omitempty"`
	NumCtx           int      `json:"num_ctx,omitempty"`
}

type EmbeddingRequest struct {
	Model            string   `json:"model"`
	Input            any      `json:"input"`
	EncodingFormat   string   `json:"encoding_format,omitempty"`
	Dimensions       *int     `json:"dimensions,omitempty"`
	User             string   `json:"user,omitempty"`
	Seed             *float64 `json:"seed,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
}

func (r *EmbeddingRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts []string
	var files []*types.FileMeta

	items, err := r.ParseMediaInputs()
	if err != nil {
		// NOTE: Inputs this parser rejects, such as token id arrays, still count as text for estimation.
		texts = r.ParseInput()
	} else {
		for _, parts := range items {
			for _, part := range parts {
				if part.Type == ContentTypeText {
					texts = append(texts, part.Text)
				} else if meta := part.ToFileMeta(); meta != nil {
					files = append(files, meta)
				}
			}
		}
	}

	return &types.TokenCountMeta{
		CombineText: strings.Join(texts, "\n"),
		Files:       files,
	}
}

func (r *EmbeddingRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *EmbeddingRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *EmbeddingRequest) ParseInput() []string {
	if r.Input == nil {
		return make([]string, 0)
	}
	var input []string
	switch r.Input.(type) {
	case string:
		input = []string{r.Input.(string)}
	case []any:
		input = make([]string, 0, len(r.Input.([]any)))
		for _, item := range r.Input.([]any) {
			if str, ok := item.(string); ok {
				input = append(input, str)
			}
		}
	}
	return input
}

// ParseMediaInputs returns one entry per embedding to produce, in input order.
// Each entry holds the content parts embedded together as a single vector, so
// an array of parts nested inside an input item yields one aggregated embedding.
// Items use the chat completions content part shapes: text, image_url,
// input_audio, file and video_url.
func (r *EmbeddingRequest) ParseMediaInputs() ([][]MediaContent, error) {
	switch input := r.Input.(type) {
	case nil:
		return nil, nil
	case string:
		return [][]MediaContent{{{Type: ContentTypeText, Text: input}}}, nil
	case map[string]any:
		part, err := parseEmbeddingContentPart(input)
		if err != nil {
			return nil, err
		}
		return [][]MediaContent{{part}}, nil
	case []any:
		items := make([][]MediaContent, 0, len(input))
		for i, item := range input {
			parts, err := parseEmbeddingInputItem(item)
			if err != nil {
				return nil, fmt.Errorf("input[%d]: %w", i, err)
			}
			items = append(items, parts)
		}
		return items, nil
	}
	return nil, fmt.Errorf("unsupported input type %T", r.Input)
}

func parseEmbeddingInputItem(item any) ([]MediaContent, error) {
	switch v := item.(type) {
	case string:
		return []MediaContent{{Type: ContentTypeText, Text: v}}, nil
	case map[string]any:
		part, err := parseEmbeddingContentPart(v)
		if err != nil {
			return nil, err
		}
		return []MediaContent{part}, nil
	case []any:
		if len(v) == 0 {
			return nil, errors.New("content part list is empty")
		}
		parts := make([]MediaContent, 0, len(v))
		for i, elem := range v {
			switch e := elem.(type) {
			case string:
				parts = append(parts, MediaContent{Type: ContentTypeText, Text: e})
			case map[string]any:
				part, err := parseEmbeddingContentPart(e)
				if err != nil {
					return nil, fmt.Errorf("part[%d]: %w", i, err)
				}
				parts = append(parts, part)
			default:
				return nil, fmt.Errorf("part[%d]: unsupported type %T", i, elem)
			}
		}
		return parts, nil
	}
	return nil, fmt.Errorf("unsupported type %T", item)
}

func parseEmbeddingContentPart(m map[string]any) (MediaContent, error) {
	part := MediaContent{
		Type:       common.Interface2String(m["type"]),
		Text:       common.Interface2String(m["text"]),
		ImageUrl:   m["image_url"],
		InputAudio: m["input_audio"],
		File:       m["file"],
		VideoUrl:   m["video_url"],
	}
	switch part.Type {
	case ContentTypeText:
	case ContentTypeImageURL, ContentTypeInputAudio, ContentTypeFile, ContentTypeVideoUrl:
		if part.ToFileSource() == nil {
			return MediaContent{}, fmt.Errorf("%s content part has no data", part.Type)
		}
	default:
		return MediaContent{}, fmt.Errorf("unsupported content part type %q", part.Type)
	}
	return part, nil
}

type EmbeddingResponseItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingResponse struct {
	Object string                  `json:"object"`
	Data   []EmbeddingResponseItem `json:"data"`
	Model  string                  `json:"model"`
	Usage  `json:"usage"`
}
