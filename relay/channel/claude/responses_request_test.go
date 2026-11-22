package claude

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func parseResponsesRequestForTest(t *testing.T, body string) dto.OpenAIResponsesRequest {
	t.Helper()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return req
}

func TestRequestResponses2ClaudeMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("replays encrypted thinking in tool loop and preserves thinking config", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4-5"}}

		thought := "checking weather tool"
		enc := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &thought,
			Signature: "sig_tool_1",
		})
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "weather in Tokyo?"},
				{"type": "reasoning", "id": "rs_1", "summary": [{"type": "summary_text", "text": "checking weather tool"}], "encrypted_content": "`+enc+`"},
				{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"},
				{"type": "function_call_output", "call_id": "call_1", "output": "Sunny 22C"}
			],
			"tools": [{"type": "function", "name": "get_weather", "parameters": {"type": "object"}}]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(c, info, req)
		require.NoError(t, err)
		require.NotNil(t, claudeReq.Thinking)
		require.Equal(t, "high", info.ReasoningEffort)
		require.Equal(t, "claude-sonnet-4-5", common.GetContextKeyString(c, constant.ContextKeyClaudeSignatureModel))

		require.Len(t, claudeReq.Messages, 3)
		userBlocks, ok := claudeReq.Messages[0].Content.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, userBlocks, 1)
		require.Equal(t, "text", userBlocks[0].Type)
		require.Equal(t, "weather in Tokyo?", *userBlocks[0].Text)

		assistantBlocks, ok := claudeReq.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, assistantBlocks, 2)
		require.Equal(t, "thinking", assistantBlocks[0].Type)
		require.NotNil(t, assistantBlocks[0].Thinking)
		require.Equal(t, thought, *assistantBlocks[0].Thinking)
		require.Equal(t, "sig_tool_1", assistantBlocks[0].Signature)
		require.Equal(t, "tool_use", assistantBlocks[1].Type)
		require.Equal(t, "call_1", assistantBlocks[1].Id)

		userToolBlocks, ok := claudeReq.Messages[2].Content.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, userToolBlocks, 1)
		require.Equal(t, "tool_result", userToolBlocks[0].Type)
		require.Equal(t, "call_1", userToolBlocks[0].ToolUseId)
	})

	t.Run("disables thinking and strips earlier thinking blocks when tool loop lacks replayable block", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4-5"}}

		thought := "first turn thought"
		enc := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &thought,
			Signature: "sig_turn_1",
		})
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "turn 1"},
				{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc+`"},
				{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "ans 1"}]},
				{"type": "message", "role": "user", "content": "call tool"},
				{"type": "function_call", "call_id": "call_2", "name": "get_weather", "arguments": "{}"},
				{"type": "function_call_output", "call_id": "call_2", "output": "ok"}
			],
			"tools": [{"type": "function", "name": "get_weather", "parameters": {"type": "object"}}]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(c, info, req)
		require.NoError(t, err)
		require.Nil(t, claudeReq.Thinking)
		require.Equal(t, "claude-sonnet-4-5", common.GetContextKeyString(c, constant.ContextKeyClaudeSignatureModel))

		for _, msg := range claudeReq.Messages {
			blocks, ok := msg.Content.([]dto.ClaudeMediaMessage)
			if !ok {
				continue
			}
			for _, b := range blocks {
				require.NotEqual(t, "thinking", b.Type)
				require.NotEqual(t, "redacted_thinking", b.Type)
			}
		}
	})

	t.Run("rejects previous_response_id", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"previous_response_id": "resp_123",
			"input": "hello"
		}`)
		_, err := RequestResponses2ClaudeMessage(c, &relaycommon.RelayInfo{}, req)
		require.Error(t, err)
	})
}

func TestAdaptorConvertOpenAIResponsesRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4-5"}}
	req := parseResponsesRequestForTest(t, `{"model":"claude-sonnet-4-5","input":"hello"}`)

	adaptor := &Adaptor{}
	out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	claudeReq, ok := out.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Equal(t, "claude-sonnet-4-5", claudeReq.Model)
}
