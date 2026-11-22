package claude

import (
	"encoding/base64"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

const claudeThinkingMarker = "claude-thinking-v1:"

type claudeThinkingBlock struct {
	Model     string `json:"model"`
	Type      string `json:"type"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

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

func decodeClaudeThinkingBlock(encrypted string) (claudeThinkingBlock, bool) {
	rest, ok := strings.CutPrefix(encrypted, claudeThinkingMarker)
	if !ok {
		return claudeThinkingBlock{}, false
	}
	// NOTE: Bedrock model IDs contain colons, so split on the last colon.
	sep := strings.LastIndex(rest, ":")
	if sep <= 0 {
		return claudeThinkingBlock{}, false
	}
	model, encoded := rest[:sep], rest[sep+1:]
	if encoded == "" {
		return claudeThinkingBlock{}, false
	}

	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return claudeThinkingBlock{}, false
		}
	}

	var payload claudeThinkingPayload
	if err := common.Unmarshal(raw, &payload); err != nil {
		return claudeThinkingBlock{}, false
	}
	switch payload.Type {
	case "thinking":
		if payload.Signature == "" {
			return claudeThinkingBlock{}, false
		}
	case "redacted_thinking":
		if payload.Data == "" {
			return claudeThinkingBlock{}, false
		}
	default:
		return claudeThinkingBlock{}, false
	}

	return claudeThinkingBlock{
		Model:     model,
		Type:      payload.Type,
		Thinking:  payload.Thinking,
		Signature: payload.Signature,
		Data:      payload.Data,
	}, true
}

func (b claudeThinkingBlock) toClaudeMediaMessage() dto.ClaudeMediaMessage {
	if b.Type == "redacted_thinking" {
		return dto.ClaudeMediaMessage{
			Type: "redacted_thinking",
			Data: b.Data,
		}
	}
	text := b.Thinking
	return dto.ClaudeMediaMessage{
		Type:      "thinking",
		Thinking:  &text,
		Signature: b.Signature,
	}
}

func canonicalClaudeModelName(model string) string {
	s := strings.ToLower(strings.TrimSpace(model))
	if s == "" {
		return ""
	}
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "anthropic."); idx >= 0 {
		s = s[idx+len("anthropic."):]
	}
	s = strings.ReplaceAll(s, "@", "-")
	s = trimTrailingVersionSuffix(s)
	if trimmed, ok := strings.CutSuffix(s, "-thinking"); ok && trimmed != "" {
		s = trimmed
	} else if base, _, ok := reasoning.TrimEffortSuffix(s); ok && base != "" {
		s = base
	}
	s = trimTrailingVersionSuffix(s)
	if trimmed, ok := strings.CutSuffix(s, "-latest"); ok && trimmed != "" {
		s = trimmed
	}
	if idx := strings.LastIndex(s, "-"); idx > 0 && isAllASCII8Digits(s[idx+1:]) {
		s = s[:idx]
	}
	return s
}

func trimTrailingVersionSuffix(s string) string {
	idx := strings.LastIndex(s, "-v")
	if idx <= 0 || idx+2 >= len(s) {
		return s
	}
	for i := idx + 2; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s
		}
	}
	return s[:idx]
}

func isAllASCII8Digits(s string) bool {
	if len(s) != 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func claudeModelsMatch(sigModel, targetModel string) bool {
	a := canonicalClaudeModelName(sigModel)
	b := canonicalClaudeModelName(targetModel)
	return a != "" && a == b
}

func isClaudeAdaptiveThinkingModel(model string) bool {
	canonical := canonicalClaudeModelName(model)
	return strings.HasPrefix(canonical, "claude-opus-4-6") ||
		strings.HasPrefix(canonical, "claude-opus-4-7") ||
		strings.HasPrefix(canonical, "claude-sonnet-4-6")
}

func isClaudeOpus47Model(model string) bool {
	return strings.HasPrefix(canonicalClaudeModelName(model), "claude-opus-4-7")
}
