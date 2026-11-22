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
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-4-7"}}

		thought := "first turn thought"
		enc := encodeClaudeThinking("claude-opus-4-7", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &thought,
			Signature: "sig_turn_1",
		})
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-7",
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
		require.Empty(t, claudeReq.GetEfforts())
		require.Empty(t, info.ReasoningEffort)
		require.Equal(t, "claude-opus-4-7", common.GetContextKeyString(c, constant.ContextKeyClaudeSignatureModel))

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

	t.Run("checks earlier assistant turns in multi-step tool loop", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		thought2 := "step 2 thought"
		enc2 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &thought2,
			Signature: "sig_step_2",
		})
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "run two tools"},
				{"type": "function_call", "call_id": "call_1", "name": "step_one", "arguments": "{}"},
				{"type": "function_call_output", "call_id": "call_1", "output": "r1"},
				{"type": "reasoning", "id": "rs_2", "encrypted_content": "`+enc2+`"},
				{"type": "function_call", "call_id": "call_2", "name": "step_two", "arguments": "{}"},
				{"type": "function_call_output", "call_id": "call_2", "output": "r2"}
			],
			"tools": [
				{"type": "function", "name": "step_one", "parameters": {"type": "object"}},
				{"type": "function", "name": "step_two", "parameters": {"type": "object"}}
			]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(c, &relaycommon.RelayInfo{}, req)
		require.NoError(t, err)
		require.Nil(t, claudeReq.Thinking)
	})

	t.Run("replays cross-channel signatures across Anthropic, Bedrock, and Vertex model IDs", func(t *testing.T) {
		thought := "cross channel thought"
		enc := encodeClaudeThinking("claude-sonnet-4-5-20250929", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &thought,
			Signature: "sig_cross",
		})

		for _, targetModel := range []string{
			"us.anthropic.claude-sonnet-4-5-20250929-v1:0",
			"claude-sonnet-4-5@20250929",
		} {
			req := parseResponsesRequestForTest(t, `{
				"model": "`+targetModel+`",
				"reasoning": {"effort": "medium"},
				"input": [
					{"type": "message", "role": "user", "content": "hi"},
					{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc+`"},
					{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{}"},
					{"type": "function_call_output", "call_id": "call_1", "output": "done"}
				],
				"tools": [{"type": "function", "name": "lookup", "parameters": {"type": "object"}}]
			}`)
			claudeReq, err := RequestResponses2ClaudeMessage(nil, nil, req)
			require.NoError(t, err, targetModel)
			require.NotNil(t, claudeReq.Thinking, targetModel)
			blocks := claudeReq.Messages[1].Content.([]dto.ClaudeMediaMessage)
			require.Len(t, blocks, 2, targetModel)
			require.Equal(t, "thinking", blocks[0].Type, targetModel)
			require.Equal(t, "sig_cross", blocks[0].Signature, targetModel)
		}
	})

	t.Run("preserves interleaved thinking and tool calls in exact order", func(t *testing.T) {
		t1, t2 := "first thought", "second thought"
		enc1 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t1, Signature: "sig_1"})
		enc2 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t2, Signature: "sig_2"})
		encRed := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "redacted_thinking", Data: "red_1"})

		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "do both"},
				{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc1+`"},
				{"type": "reasoning", "id": "rs_red", "encrypted_content": "`+encRed+`"},
				{"type": "function_call", "call_id": "call_1", "name": "tool_a", "arguments": "{}"},
				{"type": "reasoning", "id": "rs_2", "encrypted_content": "`+enc2+`"},
				{"type": "function_call", "call_id": "call_2", "name": "tool_b", "arguments": "{}"},
				{"type": "function_call_output", "call_id": "call_1", "output": "ok1"},
				{"type": "function_call_output", "call_id": "call_2", "output": "ok2"}
			],
			"tools": [
				{"type": "function", "name": "tool_a", "parameters": {"type": "object"}},
				{"type": "function", "name": "tool_b", "parameters": {"type": "object"}}
			]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(nil, nil, req)
		require.NoError(t, err)
		require.Len(t, claudeReq.Messages, 3)

		blocks := claudeReq.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, blocks, 5)
		require.Equal(t, "thinking", blocks[0].Type)
		require.Equal(t, "sig_1", blocks[0].Signature)
		require.Equal(t, "redacted_thinking", blocks[1].Type)
		require.Equal(t, "red_1", blocks[1].Data)
		require.Equal(t, "tool_use", blocks[2].Type)
		require.Equal(t, "call_1", blocks[2].Id)
		require.Equal(t, "thinking", blocks[3].Type)
		require.Equal(t, "sig_2", blocks[3].Signature)
		require.Equal(t, "tool_use", blocks[4].Type)
		require.Equal(t, "call_2", blocks[4].Id)
	})

	t.Run("collapses consecutive regular thinking blocks to latest while keeping redacted blocks", func(t *testing.T) {
		t1, t2 := "stale thought", "latest thought"
		enc1 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t1, Signature: "sig_old"})
		enc2 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t2, Signature: "sig_new"})

		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "hi"},
				{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc1+`"},
				{"type": "reasoning", "id": "rs_2", "encrypted_content": "`+enc2+`"},
				{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "hello"}]},
				{"type": "message", "role": "user", "content": "next"}
			]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(nil, nil, req)
		require.NoError(t, err)
		blocks := claudeReq.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, blocks, 2)
		require.Equal(t, "thinking", blocks[0].Type)
		require.Equal(t, "latest thought", *blocks[0].Thinking)
		require.Equal(t, "sig_new", blocks[0].Signature)
		require.Equal(t, "text", blocks[1].Type)
	})

	t.Run("preserves pure-thinking assistant turn before user message and strips trailing thinking on final assistant", func(t *testing.T) {
		t1, t2 := "truncated turn", "trailing thought"
		enc1 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t1, Signature: "sig_mid"})
		enc2 := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &t2, Signature: "sig_end"})

		reqMid := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "start"},
				{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc1+`"},
				{"type": "message", "role": "user", "content": "continue"}
			]
		}`)
		outMid, err := RequestResponses2ClaudeMessage(nil, nil, reqMid)
		require.NoError(t, err)
		require.Len(t, outMid.Messages, 3)
		require.Equal(t, "assistant", outMid.Messages[1].Role)
		midBlocks := outMid.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, midBlocks, 1)
		require.Equal(t, "thinking", midBlocks[0].Type)
		require.Equal(t, "sig_mid", midBlocks[0].Signature)

		reqTrailing := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "start"},
				{"type": "reasoning", "id": "rs_1", "encrypted_content": "`+enc1+`"},
				{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "partial"}]},
				{"type": "reasoning", "id": "rs_2", "encrypted_content": "`+enc2+`"}
			]
		}`)
		outTrailing, err := RequestResponses2ClaudeMessage(nil, nil, reqTrailing)
		require.NoError(t, err)
		require.Len(t, outTrailing.Messages, 2)
		trailBlocks := outTrailing.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, trailBlocks, 2)
		require.Equal(t, "thinking", trailBlocks[0].Type)
		require.Equal(t, "text", trailBlocks[1].Type)

		reqOnlyTrailing := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "high"},
			"input": [
				{"type": "message", "role": "user", "content": "start"},
				{"type": "reasoning", "id": "rs_2", "encrypted_content": "`+enc2+`"}
			]
		}`)
		outOnlyTrailing, err := RequestResponses2ClaudeMessage(nil, nil, reqOnlyTrailing)
		require.NoError(t, err)
		require.Len(t, outOnlyTrailing.Messages, 1)
		require.Equal(t, "user", outOnlyTrailing.Messages[0].Role)
	})

	t.Run("maps adaptive effort, summary display, sampling cleanup, and forced tool_choice", func(t *testing.T) {
		// Opus 4.7 with xhigh + temperature + top_p -> adaptive, effort=xhigh, display=summarized, sampling cleared.
		req47 := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-7",
			"reasoning": {"effort": "xhigh"},
			"temperature": 0.7,
			"top_p": 0.9,
			"input": "solve"
		}`)
		out47, err := RequestResponses2ClaudeMessage(nil, nil, req47)
		require.NoError(t, err)
		require.NotNil(t, out47.Thinking)
		require.Equal(t, "adaptive", out47.Thinking.Type)
		require.Equal(t, "summarized", out47.Thinking.Display)
		require.Nil(t, out47.Thinking.BudgetTokens)
		require.Equal(t, "xhigh", out47.GetEfforts())
		require.Nil(t, out47.Temperature)
		require.Nil(t, out47.TopP)

		// Opus 4.6 with xhigh + summary=none -> adaptive, effort=max, display=omitted.
		req46 := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-6",
			"reasoning": {"effort": "xhigh", "summary": "none"},
			"temperature": 0.5,
			"input": "solve"
		}`)
		out46, err := RequestResponses2ClaudeMessage(nil, nil, req46)
		require.NoError(t, err)
		require.NotNil(t, out46.Thinking)
		require.Equal(t, "adaptive", out46.Thinking.Type)
		require.Equal(t, "omitted", out46.Thinking.Display)
		require.Equal(t, "max", out46.GetEfforts())
		require.Nil(t, out46.Temperature)

		// Adaptive with effort=none -> disabled, no effort in output_config.
		reqNone := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-6",
			"reasoning": {"effort": "none"},
			"input": "solve"
		}`)
		outNone, err := RequestResponses2ClaudeMessage(nil, nil, reqNone)
		require.NoError(t, err)
		require.NotNil(t, outNone.Thinking)
		require.Equal(t, "disabled", outNone.Thinking.Type)
		require.Empty(t, outNone.GetEfforts())

		// Non-adaptive with effort=max and explicit small max_output_tokens -> clamps budget below max_tokens.
		reqClamp := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"reasoning": {"effort": "max"},
			"max_output_tokens": 2000,
			"temperature": 0.4,
			"top_p": 0.8,
			"input": "solve"
		}`)
		outClamp, err := RequestResponses2ClaudeMessage(nil, nil, reqClamp)
		require.NoError(t, err)
		require.NotNil(t, outClamp.Thinking)
		require.Equal(t, "enabled", outClamp.Thinking.Type)
		require.Equal(t, 1999, outClamp.Thinking.GetBudgetTokens())
		require.Equal(t, uint(2000), *outClamp.MaxTokens)
		require.Nil(t, outClamp.Temperature)
		require.Nil(t, outClamp.TopP)

		// Summary=auto without effort activates thinking with summarized display.
		reqSummaryOnly := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-7",
			"reasoning": {"summary": "auto"},
			"input": "solve"
		}`)
		outSummaryOnly, err := RequestResponses2ClaudeMessage(nil, nil, reqSummaryOnly)
		require.NoError(t, err)
		require.NotNil(t, outSummaryOnly.Thinking)
		require.Equal(t, "adaptive", outSummaryOnly.Thinking.Type)
		require.Equal(t, "summarized", outSummaryOnly.Thinking.Display)

		// Forced tool_choice disables thinking to avoid Anthropic 400.
		reqForcedTool := parseResponsesRequestForTest(t, `{
			"model": "claude-opus-4-7",
			"reasoning": {"effort": "high"},
			"tool_choice": "required",
			"tools": [{"type": "function", "name": "lookup", "parameters": {"type": "object"}}],
			"input": "solve"
		}`)
		outForcedTool, err := RequestResponses2ClaudeMessage(nil, nil, reqForcedTool)
		require.NoError(t, err)
		require.Nil(t, outForcedTool.Thinking)
		require.Empty(t, outForcedTool.GetEfforts())
	})

	t.Run("converts instructions, developer messages, custom tools, unpaired outputs, and web search", func(t *testing.T) {
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"instructions": "Be concise.",
			"input": [
				{"type": "message", "role": "developer", "content": "Follow style."},
				{"type": "message", "role": "user", "content": "patch file"},
				{"type": "custom_tool_call", "call_id": "call_custom_1", "name": "apply_patch", "input": "*** Begin Patch"},
				{"type": "custom_tool_call_output", "call_id": "call_custom_1", "output": "patched"},
				{"type": "function_call_output", "call_id": "call_orphan", "output": "stray output"}
			],
			"tools": [
				{"type": "custom", "name": "apply_patch", "description": "Apply a patch"},
				{"type": "web_search_preview", "search_context_size": "medium"}
			]
		}`)

		claudeReq, err := RequestResponses2ClaudeMessage(nil, nil, req)
		require.NoError(t, err)

		systemBlocks, ok := claudeReq.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, systemBlocks, 2)
		require.Equal(t, "Be concise.", *systemBlocks[0].Text)
		require.Equal(t, "Follow style.", *systemBlocks[1].Text)

		tools, ok := claudeReq.Tools.([]any)
		require.True(t, ok)
		require.Len(t, tools, 2)
		customTool, ok := tools[0].(*dto.Tool)
		require.True(t, ok)
		require.Equal(t, "apply_patch", customTool.Name)
		wsTool, ok := tools[1].(*dto.ClaudeWebSearchTool)
		require.True(t, ok)
		require.Equal(t, WebSearchMaxUsesMedium, wsTool.MaxUses)

		require.Len(t, claudeReq.Messages, 3)
		asstBlocks := claudeReq.Messages[1].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, asstBlocks, 1)
		require.Equal(t, "tool_use", asstBlocks[0].Type)
		require.Equal(t, "call_custom_1", asstBlocks[0].Id)
		require.Equal(t, map[string]any{"input": "*** Begin Patch"}, asstBlocks[0].Input)

		userOutBlocks := claudeReq.Messages[2].Content.([]dto.ClaudeMediaMessage)
		require.Len(t, userOutBlocks, 2)
		require.Equal(t, "tool_result", userOutBlocks[0].Type)
		require.Equal(t, "call_custom_1", userOutBlocks[0].ToolUseId)
		require.Equal(t, "patched", userOutBlocks[0].Content)
		require.Equal(t, "text", userOutBlocks[1].Type)
		require.Equal(t, "[tool_output_missing_call] stray output", *userOutBlocks[1].Text)
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
