package openaicompat

import (
	"encoding/json"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const includeEncryptedReasoning = "reasoning.encrypted_content"

// ClaudeThinkingBlock holds an Anthropic thinking or redacted_thinking block
// carried inside extra_content.anthropic.thinking_blocks during Responses -> Chat -> Claude conversion.
type ClaudeThinkingBlock struct {
	Model     string `json:"model"`
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

func ClaudeThinkingBlocksFromExtraContent(raw json.RawMessage) []ClaudeThinkingBlock {
	if len(raw) == 0 {
		return nil
	}
	var extra struct {
		Anthropic struct {
			ThinkingBlocks []ClaudeThinkingBlock `json:"thinking_blocks"`
		} `json:"anthropic"`
	}
	if err := common.Unmarshal(raw, &extra); err != nil {
		return nil
	}
	return extra.Anthropic.ThinkingBlocks
}

func withAnthropicExtra(raw json.RawMessage, key string, value any) json.RawMessage {
	root := map[string]any{}
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &root); err != nil || root == nil {
			root = map[string]any{}
		}
	}
	anthropic, _ := root["anthropic"].(map[string]any)
	if anthropic == nil {
		anthropic = map[string]any{}
	}
	anthropic[key] = value
	root["anthropic"] = anthropic
	out, err := common.Marshal(root)
	if err != nil {
		return raw
	}
	return out
}

func ResponsesIncludesEncryptedReasoning(req *dto.OpenAIResponsesRequest) bool {
	if req == nil || len(req.Include) == 0 {
		return false
	}
	var include []string
	if err := common.Unmarshal(req.Include, &include); err != nil {
		return false
	}
	return slices.Contains(include, includeEncryptedReasoning)
}
