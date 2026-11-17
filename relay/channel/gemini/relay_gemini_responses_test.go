package gemini

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

func responsesTestRequest(t *testing.T, body string) *dto.OpenAIResponsesRequest {
	t.Helper()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return &req
}

func newResponsesTestInfo(request *dto.OpenAIResponsesRequest) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "gemini-3-flash-preview",
		Request:         request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-3-flash-preview",
		},
	}
}

func newResponsesTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func geminiResponsesHTTPResponse(t *testing.T, payload dto.GeminiChatResponse) *http.Response {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
}

func geminiResponsesStreamHTTPResponse(t *testing.T, chunks ...dto.GeminiChatResponse) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	for _, chunk := range chunks {
		data, err := common.Marshal(chunk)
		require.NoError(t, err)
		buf.WriteString("data: " + string(data) + "\n")
	}
	buf.WriteString("data: [DONE]\n")
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(&buf)}
}

func textCandidate(finishReason string, parts ...dto.GeminiPart) []dto.GeminiChatCandidate {
	candidate := dto.GeminiChatCandidate{Content: dto.GeminiChatContent{Role: "model", Parts: parts}}
	if finishReason != "" {
		candidate.FinishReason = common.GetPointer(finishReason)
	}
	return []dto.GeminiChatCandidate{candidate}
}

func TestGeminiResponsesHandlerReasoningMessageAndUsage(t *testing.T) {
	c, rec := newResponsesTestContext()
	request := responsesTestRequest(t, `{
		"model":"gemini-3-flash-preview","input":"hi","instructions":"be brief","max_output_tokens":128,
		"temperature":0.2,"reasoning":{"effort":"low","summary":"auto"},"metadata":{"k":"v"},
		"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]
	}`)
	info := newResponsesTestInfo(request)

	usage, apiErr := GeminiResponsesHandler(c, info, geminiResponsesHTTPResponse(t, dto.GeminiChatResponse{
		Candidates: textCandidate("STOP",
			dto.GeminiPart{Text: "thinking it over", Thought: true},
			dto.GeminiPart{Text: "Hello"},
		),
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:        10,
			CandidatesTokenCount:    5,
			ThoughtsTokenCount:      3,
			CachedContentTokenCount: 4,
			TotalTokenCount:         18,
		},
	}))
	require.Nil(t, apiErr)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 8, usage.CompletionTokens)

	var got map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "response", got["object"])
	require.True(t, strings.HasPrefix(got["id"].(string), "resp_"))
	require.Equal(t, "completed", got["status"])
	require.Nil(t, got["incomplete_details"])
	require.Equal(t, "gemini-3-flash-preview", got["model"])
	require.Equal(t, "be brief", got["instructions"])
	require.EqualValues(t, 128, got["max_output_tokens"])
	require.EqualValues(t, 0.2, got["temperature"])
	require.Equal(t, map[string]any{"k": "v"}, got["metadata"])
	require.Equal(t, map[string]any{"effort": "low", "summary": "auto"}, got["reasoning"])
	require.Len(t, got["tools"], 1)

	output := got["output"].([]any)
	require.Len(t, output, 2)
	reasoning := output[0].(map[string]any)
	require.Equal(t, "reasoning", reasoning["type"])
	require.True(t, strings.HasPrefix(reasoning["id"].(string), "rs_"))
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "thinking it over"}}, reasoning["summary"])
	message := output[1].(map[string]any)
	require.Equal(t, "message", message["type"])
	require.Equal(t, "completed", message["status"])
	require.Equal(t, "assistant", message["role"])
	require.Equal(t, []any{map[string]any{"type": "output_text", "text": "Hello", "annotations": []any{}}}, message["content"])

	require.Equal(t, map[string]any{
		"input_tokens":          float64(10),
		"input_tokens_details":  map[string]any{"cached_tokens": float64(4)},
		"output_tokens":         float64(8),
		"output_tokens_details": map[string]any{"reasoning_tokens": float64(3)},
		"total_tokens":          float64(18),
	}, got["usage"])
}

func TestGeminiResponsesHandlerFunctionAndCustomToolCalls(t *testing.T) {
	c, rec := newResponsesTestContext()
	request := responsesTestRequest(t, `{
		"model":"gemini-3-flash-preview","input":"hi",
		"tools":[{"type":"function","name":"get_weather"},{"type":"custom","name":"apply_patch"}]
	}`)
	info := newResponsesTestInfo(request)

	_, apiErr := GeminiResponsesHandler(c, info, geminiResponsesHTTPResponse(t, dto.GeminiChatResponse{
		Candidates: textCandidate("STOP",
			dto.GeminiPart{Text: "checking"},
			dto.GeminiPart{FunctionCall: &dto.FunctionCall{FunctionName: "get_weather", Arguments: map[string]any{"city": "Paris"}}},
			dto.GeminiPart{FunctionCall: &dto.FunctionCall{FunctionName: "apply_patch", Arguments: map[string]any{"input": "*** Begin Patch"}}},
		),
		UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 5, TotalTokenCount: 10},
	}))
	require.Nil(t, apiErr)

	var got map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
	output := got["output"].([]any)
	require.Len(t, output, 3)
	require.Equal(t, "message", output[0].(map[string]any)["type"])

	function := output[1].(map[string]any)
	require.Equal(t, "function_call", function["type"])
	require.Equal(t, "get_weather", function["name"])
	require.Equal(t, `{"city":"Paris"}`, function["arguments"])
	require.True(t, strings.HasPrefix(function["call_id"].(string), "call_"))
	require.Equal(t, "completed", function["status"])

	custom := output[2].(map[string]any)
	require.Equal(t, "custom_tool_call", custom["type"])
	require.Equal(t, "apply_patch", custom["name"])
	require.Equal(t, "*** Begin Patch", custom["input"])
}

func TestGeminiResponsesHandlerTerminalStates(t *testing.T) {
	tests := []struct {
		finishReason string
		status       string
		reason       any
	}{
		{"STOP", "completed", nil},
		{"MAX_TOKENS", "incomplete", "max_output_tokens"},
		{"SAFETY", "incomplete", "content_filter"},
		{"MALFORMED_FUNCTION_CALL", "incomplete", "content_filter"},
	}
	for _, tt := range tests {
		t.Run(tt.finishReason, func(t *testing.T) {
			c, rec := newResponsesTestContext()
			info := newResponsesTestInfo(nil)
			_, apiErr := GeminiResponsesHandler(c, info, geminiResponsesHTTPResponse(t, dto.GeminiChatResponse{
				Candidates:    textCandidate(tt.finishReason, dto.GeminiPart{Text: "partial"}),
				UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 1, CandidatesTokenCount: 1, TotalTokenCount: 2},
			}))
			require.Nil(t, apiErr)

			var got map[string]any
			require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, tt.status, got["status"])
			if tt.reason == nil {
				require.Nil(t, got["incomplete_details"])
			} else {
				require.Equal(t, map[string]any{"reason": tt.reason}, got["incomplete_details"])
			}
		})
	}
}

func TestGeminiResponsesHandlerBlockedPrompt(t *testing.T) {
	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(nil)

	usage, apiErr := GeminiResponsesHandler(c, info, geminiResponsesHTTPResponse(t, dto.GeminiChatResponse{
		PromptFeedback: &dto.GeminiChatPromptFeedback{BlockReason: common.GetPointer("SAFETY")},
		UsageMetadata:  dto.GeminiUsageMetadata{PromptTokenCount: 7, TotalTokenCount: 7},
	}))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "request blocked by Gemini API: SAFETY")
}

func TestGeminiResponsesStreamHandlerEventSequence(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	request := responsesTestRequest(t, `{"model":"gemini-3-flash-preview","input":"hi","stream":true,"instructions":"be brief"}`)
	info := newResponsesTestInfo(request)
	info.IsStream = true

	usage, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t,
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "think", Thought: true})},
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: " more", Thought: true})},
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "Hel"})},
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "lo"})},
		dto.GeminiChatResponse{
			Candidates: textCandidate("STOP", dto.GeminiPart{FunctionCall: &dto.FunctionCall{FunctionName: "get_weather", Arguments: map[string]any{"city": "Paris"}}}),
			UsageMetadata: dto.GeminiUsageMetadata{
				PromptTokenCount: 12, CandidatesTokenCount: 6, ThoughtsTokenCount: 2, TotalTokenCount: 20,
			},
		},
	))
	require.Nil(t, apiErr)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 8, usage.CompletionTokens)

	events := parseResponsesSSE(t, rec.Body.String())
	names := make([]string, 0, len(events))
	for i, ev := range events {
		names = append(names, ev.name)
		require.Equal(t, ev.name, ev.data["type"])
		require.EqualValues(t, i, ev.data["sequence_number"])
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
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	}, names)

	created := events[0].data["response"].(map[string]any)
	require.Equal(t, "in_progress", created["status"])
	require.Equal(t, []any{}, created["output"])
	require.Nil(t, created["usage"])
	require.Equal(t, "be brief", created["instructions"])

	require.Equal(t, "think", events[4].data["delta"])
	require.Equal(t, " more", events[5].data["delta"])
	require.Equal(t, "think more", events[6].data["text"])
	require.EqualValues(t, 1, events[9].data["output_index"])
	require.Equal(t, "Hello", events[13].data["text"])
	require.Equal(t, `{"city":"Paris"}`, events[18].data["arguments"])
	require.Equal(t, "get_weather", events[18].data["name"])

	final := events[len(events)-1].data["response"].(map[string]any)
	require.Equal(t, "completed", final["status"])
	output := final["output"].([]any)
	require.Len(t, output, 3)
	require.Equal(t, "reasoning", output[0].(map[string]any)["type"])
	require.Equal(t, "message", output[1].(map[string]any)["type"])
	require.Equal(t, "function_call", output[2].(map[string]any)["type"])
	require.Equal(t, events[2].data["item"].(map[string]any)["id"], output[0].(map[string]any)["id"])
	require.Equal(t, map[string]any{
		"input_tokens":          float64(12),
		"input_tokens_details":  map[string]any{"cached_tokens": float64(0)},
		"output_tokens":         float64(8),
		"output_tokens_details": map[string]any{"reasoning_tokens": float64(2)},
		"total_tokens":          float64(20),
	}, final["usage"])
}

func TestGeminiResponsesStreamHandlerIncomplete(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.IsStream = true

	_, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t,
		dto.GeminiChatResponse{
			Candidates:    textCandidate("MAX_TOKENS", dto.GeminiPart{Text: "cut off"}),
			UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 4, TotalTokenCount: 7},
		},
	))
	require.Nil(t, apiErr)

	events := parseResponsesSSE(t, rec.Body.String())
	last := events[len(events)-1]
	require.Equal(t, "response.incomplete", last.name)
	response := last.data["response"].(map[string]any)
	require.Equal(t, "incomplete", response["status"])
	require.Equal(t, map[string]any{"reason": "max_output_tokens"}, response["incomplete_details"])
}

func TestGeminiResponsesStreamHandlerEmptyStream(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.IsStream = true

	_, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	require.Empty(t, parseResponsesSSE(t, rec.Body.String()))
}

func TestGeminiAdaptorConvertOpenAIResponsesRequest(t *testing.T) {
	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-2.5-flash"

	request := responsesTestRequest(t, `{
		"model":"gemini-2.5-flash","instructions":"be brief","max_output_tokens":64,
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"weather in Paris?"}]},
			{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}
		],
		"tools":[{"type":"function","name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]
	}`)

	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *request)
	require.NoError(t, err)
	geminiRequest, ok := got.(*dto.GeminiChatRequest)
	require.True(t, ok)

	require.NotNil(t, geminiRequest.SystemInstructions)
	require.Equal(t, "be brief", geminiRequest.SystemInstructions.Parts[0].Text)
	require.NotNil(t, geminiRequest.GenerationConfig.MaxOutputTokens)
	require.Equal(t, uint(64), *geminiRequest.GenerationConfig.MaxOutputTokens)
	require.NotEmpty(t, geminiRequest.Tools)

	require.Len(t, geminiRequest.Contents, 3)
	require.Equal(t, "weather in Paris?", geminiRequest.Contents[0].Parts[0].Text)
	call := geminiRequest.Contents[1].Parts[0].FunctionCall
	require.NotNil(t, call)
	require.Equal(t, "get_weather", call.FunctionName)
	response := geminiRequest.Contents[2].Parts[0].FunctionResponse
	require.NotNil(t, response)
	require.Equal(t, "get_weather", response.Name)
}

func TestGeminiAdaptorConvertOpenAIResponsesRequestRejectsStoredState(t *testing.T) {
	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)

	request := responsesTestRequest(t, `{"model":"gemini-2.5-flash","input":"hi","previous_response_id":"resp_123"}`)
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *request)
	require.ErrorContains(t, err, "previous_response_id")
}
