package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func chatChunk(t *testing.T, body string) *dto.ChatCompletionsStreamResponse {
	t.Helper()
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(body, &chunk))
	return &chunk
}

func feedChunks(t *testing.T, tr *ChatToResponsesTranscoder, bodies ...string) []ResponsesStreamEvent {
	t.Helper()
	var events []ResponsesStreamEvent
	for _, body := range bodies {
		events = append(events, tr.ConsumeChunk(chatChunk(t, body))...)
	}
	return events
}

func eventTypes(events []ResponsesStreamEvent) []string {
	names := make([]string, 0, len(events))
	for _, ev := range events {
		names = append(names, ev.Type)
	}
	return names
}

func newTestResponsesRequest(t *testing.T, body string) *dto.OpenAIResponsesRequest {
	t.Helper()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return &req
}

func outputItems(t *testing.T, resp map[string]any) []map[string]any {
	t.Helper()
	raw, ok := resp["output"].([]any)
	require.True(t, ok, "output must be a list")
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		items = append(items, item.(map[string]any))
	}
	return items
}

func TestChatToResponsesStreamReasoningTextAndToolCall(t *testing.T) {
	request := newTestResponsesRequest(t, `{"model":"gemini-4-hydrogen","input":"hi","instructions":"be brief","reasoning":{"effort":"low"}}`)
	tr := NewChatToResponsesTranscoder("resp_1", "gemini-4-hydrogen", 100, request, true)

	events := feedChunks(t, tr,
		`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think"}}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning_content":" more"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_weather","arguments":"{\"city\""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	usage := &dto.Usage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}
	usage.CompletionTokenDetails.ReasoningTokens = 2
	final, tail := tr.Complete(usage)
	events = append(events, tail...)

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
		"response.content_part.added",
		"response.output_text.delta",
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
	}, eventTypes(events))

	for i, ev := range events {
		require.Equal(t, ev.Type, ev.Data["type"])
		require.EqualValues(t, i, ev.Data["sequence_number"])
	}

	require.Equal(t, "think more", events[6].Data["text"])
	require.Equal(t, "Hello", events[13].Data["text"])
	require.EqualValues(t, 2, events[16].Data["output_index"])
	require.Equal(t, `{"city":"Paris"}`, events[19].Data["arguments"])
	require.Equal(t, "get_weather", events[19].Data["name"])

	require.Equal(t, "completed", final["status"])
	require.Equal(t, "resp_1", final["id"])
	require.Equal(t, "gemini-4-hydrogen", final["model"])
	require.Equal(t, final, events[len(events)-1].Data["response"])
	require.Equal(t, map[string]any{
		"input_tokens":          12,
		"input_tokens_details":  map[string]any{"cached_tokens": 0},
		"output_tokens":         8,
		"output_tokens_details": map[string]any{"reasoning_tokens": 2},
		"total_tokens":          20,
	}, final["usage"])

	items := outputItems(t, final)
	require.Len(t, items, 3)
	require.Equal(t, "reasoning", items[0]["type"])
	require.Equal(t, "message", items[1]["type"])
	require.Equal(t, "function_call", items[2]["type"])
	require.Equal(t, "call_a", items[2]["call_id"])
	require.Equal(t, `{"city":"Paris"}`, items[2]["arguments"])
	require.Equal(t, events[2].Data["item"].(map[string]any)["id"], items[0]["id"])

	created := events[0].Data["response"].(map[string]any)
	require.Equal(t, "in_progress", created["status"])
	require.Equal(t, []any{}, created["output"])
	require.Nil(t, created["usage"])
	require.JSONEq(t, `"be brief"`, string(created["instructions"].(json.RawMessage)))
}

func TestChatToResponsesStreamParallelToolCallsReusingIndexZero(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	feedChunks(t, tr,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"one","arguments":"{\"x\":1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_b","function":{"name":"two","arguments":"{\"y\":2}"}},{"index":0,"id":"call_c","function":{"name":"three","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	final, _ := tr.Complete(&dto.Usage{PromptTokens: 1, CompletionTokens: 1})

	items := outputItems(t, final)
	require.Len(t, items, 3)
	require.Equal(t, []any{"call_a", "call_b", "call_c"}, []any{items[0]["call_id"], items[1]["call_id"], items[2]["call_id"]})
	require.Equal(t, []any{"one", "two", "three"}, []any{items[0]["name"], items[1]["name"], items[2]["name"]})
	require.Equal(t, []any{`{"x":1}`, `{"y":2}`, `{}`}, []any{items[0]["arguments"], items[1]["arguments"], items[2]["arguments"]})
}

func TestChatToResponsesStreamRepeatedIDAndLateIDStayOneCall(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	feedChunks(t, tr,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"one","arguments":"{\"a\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"1"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"}"}}]}}]}`,
	)
	final, _ := tr.Complete(nil)

	items := outputItems(t, final)
	require.Len(t, items, 1)
	require.Equal(t, `{"a":1}`, items[0]["arguments"])
	require.NotEmpty(t, items[0]["call_id"])
}

func TestChatToResponsesStreamToolCallWithoutArgumentsGetsEmptyObject(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	events := feedChunks(t, tr,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"ping"}}]},"finish_reason":"tool_calls"}]}`,
	)
	final, tail := tr.Complete(nil)
	events = append(events, tail...)

	require.Equal(t, []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	}, eventTypes(events))
	require.Equal(t, "{}", events[3].Data["delta"])
	require.Equal(t, "{}", outputItems(t, final)[0]["arguments"])
}

func TestChatToResponsesStreamTextAfterToolCallClosesTheCallFirst(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	events := feedChunks(t, tr,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"one","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"content":"done"}}]}`,
	)
	names := eventTypes(events)
	toolDone := indexOf(names, "response.output_item.done")
	messageAdded := lastIndexOf(names, "response.output_item.added")
	require.True(t, toolDone >= 0 && toolDone < messageAdded, "tool item must be closed before the message opens: %v", names)

	final, _ := tr.Complete(nil)
	items := outputItems(t, final)
	require.Len(t, items, 2)
	require.Equal(t, "function_call", items[0]["type"])
	require.Equal(t, "message", items[1]["type"])
}

func indexOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}

func lastIndexOf(names []string, want string) int {
	for i := len(names) - 1; i >= 0; i-- {
		if names[i] == want {
			return i
		}
	}
	return -1
}

func TestChatToResponsesStreamCustomToolInputIsUnwrapped(t *testing.T) {
	request := newTestResponsesRequest(t, `{"model":"m","input":"hi","tools":[{"type":"custom","name":"apply_patch"}]}`)
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, request, true)

	events := feedChunks(t, tr,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_p","function":{"name":"apply_patch","arguments":"{\"input\":\"*** Begin"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":" Patch\"}"}}]},"finish_reason":"tool_calls"}]}`,
	)
	final, tail := tr.Complete(nil)
	events = append(events, tail...)

	require.Equal(t, []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		"response.output_item.done",
		"response.completed",
	}, eventTypes(events))
	require.Equal(t, "*** Begin Patch", events[3].Data["delta"])
	require.Equal(t, "*** Begin Patch", events[4].Data["input"])

	items := outputItems(t, final)
	require.Equal(t, "custom_tool_call", items[0]["type"])
	require.Equal(t, "apply_patch", items[0]["name"])
	require.Equal(t, "*** Begin Patch", items[0]["input"])
}

func TestChatToResponsesStreamLengthFinishIsIncomplete(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	feedChunks(t, tr, `{"choices":[{"delta":{"content":"cut off"},"finish_reason":"length"}]}`)
	final, tail := tr.Complete(&dto.Usage{PromptTokens: 3, CompletionTokens: 4})

	require.Equal(t, "incomplete", final["status"])
	require.Equal(t, map[string]any{"reason": "max_output_tokens"}, final["incomplete_details"])
	require.Equal(t, "response.incomplete", tail[len(tail)-1].Type)
	require.EqualValues(t, 7, final["usage"].(map[string]any)["total_tokens"])
}

func TestChatToResponsesStreamContentFilterIsIncomplete(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	feedChunks(t, tr, `{"choices":[{"delta":{},"finish_reason":"content_filter"}]}`)
	final, _ := tr.Complete(nil)

	require.Equal(t, "incomplete", final["status"])
	require.Equal(t, map[string]any{"reason": "content_filter"}, final["incomplete_details"])
}

func TestChatToResponsesStreamIgnoresUsageOnlyChunkAndEmptyDeltas(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	events := feedChunks(t, tr,
		`{"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
	)
	require.Equal(t, []string{"response.created", "response.in_progress"}, eventTypes(events))

	final, _ := tr.Complete(nil)
	require.Empty(t, outputItems(t, final))
}

func TestChatToResponsesFailKeepsWhatWasProduced(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)

	feedChunks(t, tr, `{"choices":[{"delta":{"content":"partial"}}]}`)
	events := tr.Fail("upstream_error", "boom")

	require.Len(t, events, 1)
	require.Equal(t, "response.failed", events[0].Type)
	resp := events[0].Data["response"].(map[string]any)
	require.Equal(t, "failed", resp["status"])
	require.Equal(t, map[string]any{"code": "upstream_error", "message": "boom"}, resp["error"])
	items := outputItems(t, resp)
	require.Len(t, items, 1)
	require.Equal(t, "in_progress", items[0]["status"])
}

func TestChatToResponsesNonStreamMessage(t *testing.T) {
	request := newTestResponsesRequest(t, `{"model":"m","input":"hi","instructions":"be brief","max_output_tokens":128,"temperature":0.2,"reasoning":{"effort":"low","summary":"auto"}}`)
	tr := NewChatToResponsesTranscoder("resp_9", "gemini-4-hydrogen", 55, request, false)

	reasoning := "thinking it over"
	msg := &dto.Message{Role: "assistant", Content: "Hello", ReasoningContent: &reasoning}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: "one", Arguments: `{"a":1}`}},
		{ID: "call_2", Type: "function", Function: dto.FunctionRequest{Name: "two", Arguments: `{"b":2}`}},
	})
	tr.ConsumeMessage(msg, "tool_calls")
	final, events := tr.Complete(&dto.Usage{PromptTokens: 10, CompletionTokens: 6, TotalTokens: 16})

	require.Nil(t, events)
	require.Equal(t, "completed", final["status"])
	require.Equal(t, "response", final["object"])
	require.EqualValues(t, 55, final["created_at"])
	require.EqualValues(t, 128, final["max_output_tokens"])
	require.Equal(t, 0.2, final["temperature"])
	require.Equal(t, map[string]any{"effort": "low", "summary": "auto"}, final["reasoning"])

	items := outputItems(t, final)
	require.Len(t, items, 4)
	require.Equal(t, "reasoning", items[0]["type"])
	require.Equal(t, "message", items[1]["type"])
	content := items[1]["content"].([]any)[0].(map[string]any)
	require.Equal(t, "Hello", content["text"])
	require.Equal(t, "call_1", items[2]["call_id"])
	require.Equal(t, `{"a":1}`, items[2]["arguments"])
	require.Equal(t, "call_2", items[3]["call_id"])
	require.Equal(t, `{"b":2}`, items[3]["arguments"])
	require.Equal(t, "completed", items[3]["status"])
	require.Contains(t, tr.OutputText(), "Hello")
}

func TestChatToResponsesNonStreamEmptyContentWithNullToolCalls(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_9", "m", 1, nil, false)

	tr.ConsumeMessage(&dto.Message{Role: "assistant", Content: nil}, "stop")
	final, _ := tr.Complete(nil)

	require.Equal(t, "completed", final["status"])
	require.Empty(t, outputItems(t, final))
}
