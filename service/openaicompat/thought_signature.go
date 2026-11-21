package openaicompat

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// ThoughtSignatureFromExtraContent reads the Gemini thought signature that the OpenAI
// compatible layer attaches to a tool call as extra_content.google.thought_signature.
func ThoughtSignatureFromExtraContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var extra struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	}
	if err := common.Unmarshal(raw, &extra); err != nil {
		return ""
	}
	return extra.Google.ThoughtSignature
}

// ExtraContentFromThoughtSignature is the inverse of ThoughtSignatureFromExtraContent.
func ExtraContentFromThoughtSignature(signature string) json.RawMessage {
	if signature == "" {
		return nil
	}
	raw, err := common.Marshal(map[string]any{"google": map[string]any{"thought_signature": signature}})
	if err != nil {
		return nil
	}
	return raw
}
