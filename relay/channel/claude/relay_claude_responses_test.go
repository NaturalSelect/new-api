package claude

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesSSEEvent struct {
	name string
	data map[string]any
}

func parseResponsesSSE(t *testing.T, raw string) []responsesSSEEvent {
	t.Helper()
	var events []responsesSSEEvent
	for _, block := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		var ev responsesSSEEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &ev.data))
			}
		}
		if ev.name != "" {
			events = append(events, ev)
		}
	}
	return events
}

func newClaudeResponsesTestCtx(t *testing.T, model string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyClaudeSignatureModel, model)
	return c, rec
}

func newClaudeResponsesTestInfo(model string, req *dto.OpenAIResponsesRequest) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		Request:         req,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAnthropic,
			UpstreamModelName: model,
		},
	}
}

func claudeHTTPJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func claudeHTTPSSEResponse(events ...string) *http.Response {
	var buf bytes.Buffer
	for _, ev := range events {
		buf.WriteString("data: ")
		buf.WriteString(ev)
		buf.WriteString("\n\n")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
	}
}

func TestClaudeResponsesNonStream_AllBlockTypesAndAlwaysEmitsEncryptedContent(t *testing.T) {
	claudePayload := `{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-5",
		"stop_reason": "tool_use",
		"content": [
			{"type": "thinking", "thinking": "analyze request", "signature": "sig_think_1"},
			{"type": "redacted_thinking", "data": "EmwKAhgB_secret"},
			{"type": "text", "text": "Let me look that up."},
			{"type": "tool_use", "id": "toolu_123", "name": "lookup", "input": {"q": "go"}}
		],
		"usage": {
			"input_tokens": 100,
			"cache_read_input_tokens": 40,
			"cache_creation_input_tokens": 10,
			"output_tokens": 25
		}
	}`

	t.Run("with include encrypted_content", func(t *testing.T) {
		req := parseResponsesRequestForTest(t, `{
			"model": "claude-sonnet-4-5",
			"include": ["reasoning.encrypted_content"],
			"input": "hi"
		}`)
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)

		usage, apiErr := ClaudeHandler(c, claudeHTTPJSONResponse(claudePayload), info)
		require.Nil(t, apiErr)
		require.NotNil(t, usage)
		require.Equal(t, "anthropic", usage.UsageSemantic)
		require.Equal(t, 100, usage.PromptTokens)
		require.Equal(t, 25, usage.CompletionTokens)

		var out map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
		require.Equal(t, "completed", out["status"])
		require.Equal(t, "claude-sonnet-4-5", out["model"])

		// OpenAI-style usage includes cached + cache_creation in input_tokens.
		respUsage := out["usage"].(map[string]any)
		require.EqualValues(t, 150, respUsage["input_tokens"])
		require.EqualValues(t, 40, respUsage["input_tokens_details"].(map[string]any)["cached_tokens"])
		require.EqualValues(t, 25, respUsage["output_tokens"])
		require.EqualValues(t, 0, respUsage["output_tokens_details"].(map[string]any)["reasoning_tokens"])
		require.EqualValues(t, 175, respUsage["total_tokens"])

		items := out["output"].([]any)
		require.Len(t, items, 4)

		// 0: thinking reasoning item
		r0 := items[0].(map[string]any)
		require.Equal(t, "reasoning", r0["type"])
		summary0 := r0["summary"].([]any)
		require.Len(t, summary0, 1)
		require.Equal(t, "analyze request", summary0[0].(map[string]any)["text"])
		enc0, _ := r0["encrypted_content"].(string)
		require.NotEmpty(t, enc0)
		blk0, ok0 := decodeClaudeThinkingBlock(enc0)
		require.True(t, ok0)
		require.Equal(t, "thinking", blk0.Type)
		require.Equal(t, "analyze request", blk0.Thinking)
		require.Equal(t, "sig_think_1", blk0.Signature)

		// 1: redacted_thinking reasoning item
		r1 := items[1].(map[string]any)
		require.Equal(t, "reasoning", r1["type"])
		require.Empty(t, r1["summary"].([]any))
		enc1, _ := r1["encrypted_content"].(string)
		require.NotEmpty(t, enc1)
		blk1, ok1 := decodeClaudeThinkingBlock(enc1)
		require.True(t, ok1)
		require.Equal(t, "redacted_thinking", blk1.Type)
		require.Equal(t, "EmwKAhgB_secret", blk1.Data)

		// 2: assistant message
		m2 := items[2].(map[string]any)
		require.Equal(t, "message", m2["type"])
		require.Equal(t, "Let me look that up.", m2["content"].([]any)[0].(map[string]any)["text"])

		// 3: function_call
		f3 := items[3].(map[string]any)
		require.Equal(t, "function_call", f3["type"])
		require.Equal(t, "toolu_123", f3["call_id"])
		require.Equal(t, "lookup", f3["name"])
		require.JSONEq(t, `{"q":"go"}`, f3["arguments"].(string))
	})

	t.Run("without include still emits encrypted_content", func(t *testing.T) {
		req := parseResponsesRequestForTest(t, `{"model": "claude-sonnet-4-5", "input": "hi"}`)
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)

		_, apiErr := ClaudeHandler(c, claudeHTTPJSONResponse(claudePayload), info)
		require.Nil(t, apiErr)

		var out map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
		items := out["output"].([]any)
		require.Len(t, items, 4)
		enc0, hasEnc0 := items[0].(map[string]any)["encrypted_content"].(string)
		enc1, hasEnc1 := items[1].(map[string]any)["encrypted_content"].(string)
		require.True(t, hasEnc0)
		require.True(t, hasEnc1)
		require.NotEmpty(t, enc0)
		require.NotEmpty(t, enc1)
	})
}

func TestClaudeResponsesStream_EventsOrderSequenceAndEncryptedOnDoneOnly(t *testing.T) {
	req := parseResponsesRequestForTest(t, `{
		"model": "claude-sonnet-4-5",
		"stream": true,
		"include": ["reasoning.encrypted_content"],
		"input": "hi"
	}`)
	c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
	info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
	info.IsStream = true

	resp := claudeHTTPSSEResponse(
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":50,"cache_read_input_tokens":10,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"one"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_part_1"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"_part_2"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"red_data"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_9","name":"calc","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`,
	)

	usage, apiErr := ClaudeStreamHandler(c, resp, info)
	require.Nil(t, apiErr)
	require.Equal(t, 50, usage.PromptTokens)
	require.Equal(t, 20, usage.CompletionTokens)

	events := parseResponsesSSE(t, rec.Body.String())
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.name
		require.EqualValues(t, i, ev.data["sequence_number"], "sequence_number mismatch at %d (%s)", i, ev.name)
	}

	require.Equal(t, []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.output_item.done",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	}, names)

	// encrypted_content must not appear on output_item.added, only on output_item.done.
	_, addedHasEnc := events[2].data["item"].(map[string]any)["encrypted_content"]
	require.False(t, addedHasEnc)
	doneThink := events[8].data["item"].(map[string]any)
	encThink, _ := doneThink["encrypted_content"].(string)
	require.NotEmpty(t, encThink)
	blk, ok := decodeClaudeThinkingBlock(encThink)
	require.True(t, ok)
	require.Equal(t, "step one", blk.Thinking)
	require.Equal(t, "sig_part_1_part_2", blk.Signature)

	// Redacted thinking item has encrypted_content only on done and no summary parts.
	_, addedRedHasEnc := events[9].data["item"].(map[string]any)["encrypted_content"]
	require.False(t, addedRedHasEnc)
	doneRed := events[10].data["item"].(map[string]any)
	encRed, _ := doneRed["encrypted_content"].(string)
	require.NotEmpty(t, encRed)

	// No [DONE] sentinel in Responses SSE.
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestClaudeResponsesStream_MaxTokensIncomplete(t *testing.T) {
	req := parseResponsesRequestForTest(t, `{"model":"claude-sonnet-4-5","stream":true,"input":"hi"}`)
	c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
	info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
	info.IsStream = true

	resp := claudeHTTPSSEResponse(
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cut off"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":10}}`,
		`{"type":"message_stop"}`,
	)

	_, apiErr := ClaudeStreamHandler(c, resp, info)
	require.Nil(t, apiErr)

	events := parseResponsesSSE(t, rec.Body.String())
	last := events[len(events)-1]
	require.Equal(t, "response.incomplete", last.name)
	respObj := last.data["response"].(map[string]any)
	require.Equal(t, "incomplete", respObj["status"])
	require.Equal(t, "max_output_tokens", respObj["incomplete_details"].(map[string]any)["reason"])
}

func TestClaudeResponses_RefusalIncomplete(t *testing.T) {
	req := parseResponsesRequestForTest(t, `{"model":"claude-sonnet-4-5","input":"hi"}`)

	t.Run("non-stream", func(t *testing.T) {
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		payload := `{
			"id": "msg_ref_1", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "stop_reason": "refusal",
			"content": [{"type": "text", "text": "cannot help"}],
			"usage": {"input_tokens": 10, "output_tokens": 2}
		}`
		_, apiErr := ClaudeHandler(c, claudeHTTPJSONResponse(payload), info)
		require.Nil(t, apiErr)

		var out map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
		require.Equal(t, "incomplete", out["status"])
		require.Equal(t, "content_filter", out["incomplete_details"].(map[string]any)["reason"])
	})

	t.Run("stream", func(t *testing.T) {
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		info.IsStream = true
		resp := claudeHTTPSSEResponse(
			`{"type":"message_start","message":{"id":"msg_ref_2","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"no"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"refusal"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		)
		_, apiErr := ClaudeStreamHandler(c, resp, info)
		require.Nil(t, apiErr)

		events := parseResponsesSSE(t, rec.Body.String())
		last := events[len(events)-1]
		require.Equal(t, "response.incomplete", last.name)
		respObj := last.data["response"].(map[string]any)
		require.Equal(t, "incomplete", respObj["status"])
		require.Equal(t, "content_filter", respObj["incomplete_details"].(map[string]any)["reason"])
	})
}

func TestClaudeResponses_CustomToolUnwrap(t *testing.T) {
	req := parseResponsesRequestForTest(t, `{
		"model": "claude-sonnet-4-5",
		"input": "patch",
		"tools": [{"type": "custom", "name": "apply_patch", "description": "patch"}]
	}`)

	t.Run("non-stream", func(t *testing.T) {
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		payload := `{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-4-5", "stop_reason": "tool_use",
			"content": [{"type": "tool_use", "id": "toolu_c1", "name": "apply_patch", "input": {"input": "*** Begin Patch"}}],
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		_, apiErr := ClaudeHandler(c, claudeHTTPJSONResponse(payload), info)
		require.Nil(t, apiErr)

		var out map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
		item := out["output"].([]any)[0].(map[string]any)
		require.Equal(t, "custom_tool_call", item["type"])
		require.Equal(t, "toolu_c1", item["call_id"])
		require.Equal(t, "apply_patch", item["name"])
		require.Equal(t, "*** Begin Patch", item["input"])
	})

	t.Run("stream", func(t *testing.T) {
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		info.IsStream = true
		resp := claudeHTTPSSEResponse(
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_c2","name":"apply_patch","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"*** "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"Patch\"}"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`,
		)
		_, apiErr := ClaudeStreamHandler(c, resp, info)
		require.Nil(t, apiErr)

		events := parseResponsesSSE(t, rec.Body.String())
		var deltaFound, doneFound bool
		for _, ev := range events {
			if ev.name == "response.custom_tool_call_input.delta" {
				deltaFound = true
				require.Equal(t, "*** Patch", ev.data["delta"])
			}
			if ev.name == "response.custom_tool_call_input.done" {
				doneFound = true
				require.Equal(t, "*** Patch", ev.data["input"])
			}
		}
		require.True(t, deltaFound)
		require.True(t, doneFound)
	})
}

func TestClaudeResponsesStream_EmptyStreamAndMidStreamError(t *testing.T) {
	req := parseResponsesRequestForTest(t, `{"model":"claude-sonnet-4-5","stream":true,"input":"hi"}`)

	t.Run("empty stream returns ErrorCodeEmptyResponse", func(t *testing.T) {
		c, _ := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		info.IsStream = true

		_, apiErr := ClaudeStreamHandler(c, claudeHTTPSSEResponse(), info)
		require.NotNil(t, apiErr)
		require.Equal(t, types.ErrorCodeEmptyResponse, apiErr.GetErrorCode())
	})

	t.Run("mid-stream error emits response.failed", func(t *testing.T) {
		c, rec := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5")
		info := newClaudeResponsesTestInfo("claude-sonnet-4-5", &req)
		info.IsStream = true

		resp := claudeHTTPSSEResponse(
			`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"output_tokens":1}}}`,
			`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		)
		_, apiErr := ClaudeStreamHandler(c, resp, info)
		require.NotNil(t, apiErr)

		events := parseResponsesSSE(t, rec.Body.String())
		last := events[len(events)-1]
		require.Equal(t, "response.failed", last.name)
		respObj := last.data["response"].(map[string]any)
		require.Equal(t, "failed", respObj["status"])
		require.Equal(t, "overloaded_error", respObj["error"].(map[string]any)["code"])
	})
}

func TestClaudeResponses_RoundTripThinkingAndModelMismatchFallback(t *testing.T) {
	// Step 1: Upstream Claude responds with thinking + redacted_thinking + tool_use (even without include).
	turn1Req := parseResponsesRequestForTest(t, `{
		"model": "claude-sonnet-4-5-20250929",
		"reasoning": {"effort": "high"},
		"input": "check status",
		"tools": [{"type": "function", "name": "check_status", "parameters": {"type": "object"}}]
	}`)
	c1, rec1 := newClaudeResponsesTestCtx(t, "claude-sonnet-4-5-20250929")
	info1 := newClaudeResponsesTestInfo("claude-sonnet-4-5-20250929", &turn1Req)

	turn1Upstream := `{
		"id": "msg_rt_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-5",
		"stop_reason": "tool_use",
		"content": [
			{"type": "thinking", "thinking": "exact thinking text\nline 2", "signature": "sig_exact_999"},
			{"type": "redacted_thinking", "data": "RedactedBlob==123"},
			{"type": "tool_use", "id": "toolu_rt_1", "name": "check_status", "input": {"env": "prod"}}
		],
		"usage": {"input_tokens": 20, "output_tokens": 15}
	}`
	_, apiErr := ClaudeHandler(c1, claudeHTTPJSONResponse(turn1Upstream), info1)
	require.Nil(t, apiErr)

	var turn1Resp map[string]any
	require.NoError(t, common.Unmarshal(rec1.Body.Bytes(), &turn1Resp))
	outputItems := turn1Resp["output"].([]any)
	require.Len(t, outputItems, 3)

	// Step 2: Feed turn 1 output + function_call_output into turn 2 request.
	nextInput := append([]any{
		map[string]any{"type": "message", "role": "user", "content": "check status"},
	}, outputItems...)
	nextInput = append(nextInput, map[string]any{
		"type":    "function_call_output",
		"call_id": "toolu_rt_1",
		"output":  "all green",
	})
	inputRaw, err := common.Marshal(nextInput)
	require.NoError(t, err)

	// Cross-channel equivalent model (Bedrock): byte-identical thinking + redacted_thinking replayed before tool_use.
	turn2Req := dto.OpenAIResponsesRequest{
		Model:     "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
		Input:     inputRaw,
		Reasoning: &dto.Reasoning{Effort: "high"},
		Tools:     turn1Req.Tools,
	}
	c2, _ := newClaudeResponsesTestCtx(t, "")
	claudeReqSame, err := RequestResponses2ClaudeMessage(c2, newClaudeResponsesTestInfo(turn2Req.Model, &turn2Req), turn2Req)
	require.NoError(t, err)
	require.NotNil(t, claudeReqSame.Thinking)
	require.Len(t, claudeReqSame.Messages, 3)

	assistantBlocks, ok := claudeReqSame.Messages[1].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, assistantBlocks, 3)
	require.Equal(t, "thinking", assistantBlocks[0].Type)
	require.NotNil(t, assistantBlocks[0].Thinking)
	require.Equal(t, "exact thinking text\nline 2", *assistantBlocks[0].Thinking)
	require.Equal(t, "sig_exact_999", assistantBlocks[0].Signature)
	require.Equal(t, "redacted_thinking", assistantBlocks[1].Type)
	require.Equal(t, "RedactedBlob==123", assistantBlocks[1].Data)
	require.Equal(t, "tool_use", assistantBlocks[2].Type)
	require.Equal(t, "toolu_rt_1", assistantBlocks[2].Id)

	// Different model: blocks dropped, fallback disables Thinking to avoid 400.
	turn2OtherModel := turn2Req
	turn2OtherModel.Model = "claude-opus-4-5"
	c3, _ := newClaudeResponsesTestCtx(t, "")
	claudeReqOther, err := RequestResponses2ClaudeMessage(c3, newClaudeResponsesTestInfo("claude-opus-4-5", &turn2OtherModel), turn2OtherModel)
	require.NoError(t, err)
	require.Nil(t, claudeReqOther.Thinking)
	otherAssistantBlocks, ok := claudeReqOther.Messages[1].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	var hasToolUse bool
	for _, b := range otherAssistantBlocks {
		require.NotEqual(t, "thinking", b.Type)
		require.NotEqual(t, "redacted_thinking", b.Type)
		if b.Type == "tool_use" && b.Id == "toolu_rt_1" {
			hasToolUse = true
		}
	}
	require.True(t, hasToolUse)
}
