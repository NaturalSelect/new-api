package claude

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/gin-gonic/gin"
)

func RequestResponses2ClaudeMessage(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	chatReq, err := openaicompat.ResponsesRequestToChatCompletionsRequestWithSignatures(&req, decodeClaudeThinking)
	if err != nil {
		return nil, err
	}
	claudeReq, err := RequestOpenAI2ClaudeMessage(c, *chatReq)
	if err != nil {
		return nil, err
	}
	if info != nil {
		info.ReasoningEffort = EffortFromRequest(*chatReq, claudeReq)
	}
	ensureClaudeThinkingReplayable(c, claudeReq)
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyClaudeSignatureModel, claudeReq.Model)
	}
	return claudeReq, nil
}

// NOTE: Anthropic rejects tool-use continuations with thinking enabled when the preceding assistant
// message does not start with a signed thinking or redacted_thinking block; disable thinking and
// strip replayed blocks as a graceful fallback instead of returning 400.
func ensureClaudeThinkingReplayable(c *gin.Context, claudeReq *dto.ClaudeRequest) {
	if claudeReq == nil || claudeReq.Thinking == nil || len(claudeReq.Messages) < 2 {
		return
	}
	lastMsg := claudeReq.Messages[len(claudeReq.Messages)-1]
	prevMsg := claudeReq.Messages[len(claudeReq.Messages)-2]
	if lastMsg.Role != "user" || prevMsg.Role != "assistant" {
		return
	}
	if !claudeMessageHasBlockType(lastMsg, "tool_result") || !claudeMessageHasBlockType(prevMsg, "tool_use") {
		return
	}
	if claudeMessageStartsWithThinking(prevMsg) {
		return
	}
	claudeReq.Thinking = nil
	stripClaudeThinkingBlocks(claudeReq)
	if c != nil {
		logger.LogInfo(c, "disabled claude thinking for responses tool loop without replayable thinking block")
	}
}

func claudeMessageHasBlockType(msg dto.ClaudeMessage, blockType string) bool {
	blocks, ok := msg.Content.([]dto.ClaudeMediaMessage)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if b.Type == blockType {
			return true
		}
	}
	return false
}

func claudeMessageStartsWithThinking(msg dto.ClaudeMessage) bool {
	blocks, ok := msg.Content.([]dto.ClaudeMediaMessage)
	if !ok || len(blocks) == 0 {
		return false
	}
	return blocks[0].Type == "thinking" || blocks[0].Type == "redacted_thinking"
}

func stripClaudeThinkingBlocks(claudeReq *dto.ClaudeRequest) {
	for i := range claudeReq.Messages {
		blocks, ok := claudeReq.Messages[i].Content.([]dto.ClaudeMediaMessage)
		if !ok {
			continue
		}
		filtered := make([]dto.ClaudeMediaMessage, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "thinking" || b.Type == "redacted_thinking" {
				continue
			}
			filtered = append(filtered, b)
		}
		if len(filtered) == 0 {
			claudeReq.Messages[i].Content = "..."
		} else {
			claudeReq.Messages[i].Content = filtered
		}
	}
}
