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

// ReasoningThought is a thought summary the upstream signed, replayed as a signed thought part.
type ReasoningThought struct {
	Text             string `json:"text"`
	ThoughtSignature string `json:"thought_signature"`
}

// ThoughtsFromExtraContent reads the signed thought summaries stored in extra_content.google.thoughts.
func ThoughtsFromExtraContent(raw json.RawMessage) []ReasoningThought {
	if len(raw) == 0 {
		return nil
	}
	var extra struct {
		Google struct {
			Thoughts []ReasoningThought `json:"thoughts"`
		} `json:"google"`
	}
	if err := common.Unmarshal(raw, &extra); err != nil {
		return nil
	}
	return extra.Google.Thoughts
}

// withGoogleExtra returns raw with extra_content.google.<key> set and every other field kept.
func withGoogleExtra(raw json.RawMessage, key string, value any) json.RawMessage {
	root := map[string]any{}
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &root); err != nil || root == nil {
			root = map[string]any{}
		}
	}
	google, _ := root["google"].(map[string]any)
	if google == nil {
		google = map[string]any{}
	}
	google[key] = value
	root["google"] = google
	out, err := common.Marshal(root)
	if err != nil {
		return raw
	}
	return out
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
