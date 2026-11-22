package claude

import (
	"encoding/base64"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
)

const claudeThinkingMarker = "claude-thinking-v1:"

type claudeThinkingPayload struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

func encodeClaudeThinking(model string, block dto.ClaudeMediaMessage) string {
	if model == "" {
		return ""
	}
	var payload claudeThinkingPayload
	switch block.Type {
	case "thinking":
		if block.Signature == "" {
			return ""
		}
		var thinking string
		if block.Thinking != nil {
			thinking = *block.Thinking
		}
		payload = claudeThinkingPayload{
			Type:      "thinking",
			Thinking:  thinking,
			Signature: block.Signature,
		}
	case "redacted_thinking":
		if block.Data == "" {
			return ""
		}
		payload = claudeThinkingPayload{
			Type: "redacted_thinking",
			Data: block.Data,
		}
	default:
		return ""
	}
	raw, err := common.Marshal(payload)
	if err != nil {
		return ""
	}
	return claudeThinkingMarker + model + ":" + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeClaudeThinking(encrypted string) (string, openaicompat.ReasoningSignatureKind) {
	rest, ok := strings.CutPrefix(encrypted, claudeThinkingMarker)
	if !ok {
		return "", openaicompat.ReasoningSignatureNone
	}
	// NOTE: Bedrock model IDs contain colons, so split on the last colon.
	sep := strings.LastIndex(rest, ":")
	if sep <= 0 {
		return "", openaicompat.ReasoningSignatureNone
	}
	model, encoded := rest[:sep], rest[sep+1:]
	if encoded == "" {
		return "", openaicompat.ReasoningSignatureNone
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return "", openaicompat.ReasoningSignatureNone
		}
	}

	var payload claudeThinkingPayload
	if err := common.Unmarshal(raw, &payload); err != nil {
		return "", openaicompat.ReasoningSignatureNone
	}
	switch payload.Type {
	case "thinking":
		if payload.Signature == "" {
			return "", openaicompat.ReasoningSignatureNone
		}
	case "redacted_thinking":
		if payload.Data == "" {
			return "", openaicompat.ReasoningSignatureNone
		}
	default:
		return "", openaicompat.ReasoningSignatureNone
	}

	block := openaicompat.ClaudeThinkingBlock{
		Model:     model,
		Type:      payload.Type,
		Thinking:  payload.Thinking,
		Signature: payload.Signature,
		Data:      payload.Data,
	}
	out, err := common.Marshal(block)
	if err != nil {
		return "", openaicompat.ReasoningSignatureNone
	}
	return string(out), openaicompat.ReasoningSignatureForBlock
}
