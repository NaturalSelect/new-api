package openaicompat

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// ResponsesCallSignatures maps the call id of each replayed tool call to the signature carried by
// the reasoning item right in front of it. decode turns a reasoning item's encrypted_content into
// a signature and reports whether the channel recognizes it.
func ResponsesCallSignatures(input json.RawMessage, decode func(encrypted string) (string, bool)) map[string]string {
	if common.GetJsonType(input) != "array" {
		return nil
	}
	var items []map[string]any
	if err := common.Unmarshal(input, &items); err != nil {
		return nil
	}

	var signatures map[string]string
	pending := ""
	for _, item := range items {
		switch asString(item["type"]) {
		case "reasoning":
			if signature, ok := decode(asString(item["encrypted_content"])); ok {
				pending = signature
			}
		case "function_call", "custom_tool_call":
			if callID := responsesCallID(item); pending != "" && callID != "" {
				if signatures == nil {
					signatures = make(map[string]string)
				}
				signatures[callID] = pending
			}
			pending = ""
		default:
			pending = ""
		}
	}
	return signatures
}

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
