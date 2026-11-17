package openai

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
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type viaChatSSEEvent struct {
	name string
	data map[string]any
}

func parseViaChatSSE(t *testing.T, raw string) []viaChatSSEEvent {
	t.Helper()
	var events []viaChatSSEEvent
	for _, block := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		var ev viaChatSSEEvent
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

func viaChatEventNames(events []viaChatSSEEvent) []string {
	names := make([]string, 0, len(events))
	for _, ev := range events {
		names = append(names, ev.name)
	}
	return names
}

func newViaChatTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func newPoeResponsesInfo(model string, stream bool, body string, t *testing.T) *relaycommon.RelayInfo {
	t.Helper()
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &request))
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		IsStream:        stream,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		Request:         &request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypePoeOpenAI,
			ChannelBaseUrl:    "https://api.poe.com",
			UpstreamModelName: model,
		},
	}
}

func chatOnlyGeminiCatalog() map[string]service.PoeModelInfo {
	return map[string]service.PoeModelInfo{
		"gemini-4-hydrogen": {Params: map[string][]string{"thinking_level": {"low", "medium", "high", "xhigh"}}},
		"gemini-3.1-pro":    {Endpoints: []string{"/v1/responses", "/v1/chat/completions"}},
	}
}

func TestConvertOpenAIResponsesRequestPoeChatOnlyModelUsesChat(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)
	adaptor := &Adaptor{}
	adaptor.Init(info)

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:        "gemini-4-hydrogen",
		Input:        []byte(`"hi"`),
		Instructions: []byte(`"be brief"`),
		Reasoning:    &dto.Reasoning{Effort: "medium"},
		Tools:        []byte(`[{"type":"function","name":"get_weather","parameters":{"type":"object"}}]`),
	})
	require.NoError(t, err)

	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected a chat request, got %T", converted)
	require.Equal(t, "gemini-4-hydrogen", chatRequest.Model)
	require.Len(t, chatRequest.Messages, 2)
	require.Equal(t, "system", chatRequest.Messages[0].Role)
	require.Equal(t, "user", chatRequest.Messages[1].Role)
	require.Len(t, chatRequest.Tools, 1)
	require.Equal(t, "get_weather", chatRequest.Tools[0].Function.Name)
	require.Empty(t, chatRequest.ReasoningEffort)
	require.Nil(t, chatRequest.StreamOptions)

	extraBody := unmarshalRawObject(t, chatRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["thinking_level"], `"medium"`)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.poe.com/v1/chat/completions", url)
}

func TestConvertOpenAIResponsesRequestPoeChatFallbackAsksForStreamUsage(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)
	stream := true

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:  "gemini-4-hydrogen",
		Input:  []byte(`"hi"`),
		Stream: &stream,
	})
	require.NoError(t, err)

	chatRequest := converted.(*dto.GeneralOpenAIRequest)
	require.NotNil(t, chatRequest.StreamOptions)
	require.True(t, chatRequest.StreamOptions.IncludeUsage)
}

func TestConvertOpenAIResponsesRequestPoeRejectsStoredStateOnChatFallback(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)

	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model:              "gemini-4-hydrogen",
		Input:              []byte(`"hi"`),
		PreviousResponseID: "resp_old",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "previous_response_id")
}

func TestConvertOpenAIResponsesRequestPoePassesThroughWhenResponsesIsListed(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	info := newPoeResponsesInfo("gemini-3.1-pro", false, `{"model":"gemini-3.1-pro","input":"hi"}`, t)
	adaptor := &Adaptor{}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{Model: "gemini-3.1-pro", Input: []byte(`"hi"`)})
	require.NoError(t, err)
	require.IsType(t, dto.OpenAIResponsesRequest{}, converted)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.poe.com/v1/responses", url)
}

func TestShouldServeResponsesViaChatOnlyForPoeResponsesRequests(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())

	poe := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"x","input":"hi"}`, t)
	require.True(t, ShouldServeResponsesViaChat(poe))

	unknown := newPoeResponsesInfo("some-community-bot", false, `{"model":"x","input":"hi"}`, t)
	require.False(t, ShouldServeResponsesViaChat(unknown))

	compact := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"x","input":"hi"}`, t)
	compact.RelayMode = relayconstant.RelayModeResponsesCompact
	require.False(t, ShouldServeResponsesViaChat(compact))

	plainOpenAI := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"x","input":"hi"}`, t)
	plainOpenAI.ChannelType = constant.ChannelTypeOpenAI
	require.False(t, ShouldServeResponsesViaChat(plainOpenAI))

	require.False(t, ShouldServeResponsesViaChat(nil))
	require.False(t, ShouldServeResponsesViaChat(&relaycommon.RelayInfo{}))
}

func chatJSONResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func chatSSEResponse(lines ...string) *http.Response {
	var buf bytes.Buffer
	for _, line := range lines {
		buf.WriteString("data: " + line + "\n")
	}
	buf.WriteString("data: [DONE]\n")
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(&buf)}
}

func withStreamingTimeout(t *testing.T) {
	t.Helper()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = old })
}

func TestAdaptorDoResponseServesChatBodyAsResponses(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi","instructions":"be brief"}`, t)
	adaptor := &Adaptor{}

	_, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *info.Request.(*dto.OpenAIResponsesRequest))
	require.NoError(t, err)

	usage, apiErr := adaptor.DoResponse(c, chatJSONResponse(`{
		"id":"chatcmpl-1","object":"chat.completion","model":"gemini-4-hydrogen",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"Hello",
			"reasoning_content":"thinking",
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}],
		"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4}}
	}`), info)
	require.Nil(t, apiErr)

	require.Equal(t, 10, usage.(*dto.Usage).PromptTokens)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "response", resp["object"])
	require.Equal(t, "completed", resp["status"])
	require.Equal(t, "gemini-4-hydrogen", resp["model"])
	require.Equal(t, "be brief", resp["instructions"])

	output := resp["output"].([]any)
	require.Len(t, output, 3)
	require.Equal(t, "reasoning", output[0].(map[string]any)["type"])
	require.Equal(t, "message", output[1].(map[string]any)["type"])
	call := output[2].(map[string]any)
	require.Equal(t, "function_call", call["type"])
	require.Equal(t, "call_1", call["call_id"])
	require.Equal(t, `{"city":"Paris"}`, call["arguments"])

	respUsage := resp["usage"].(map[string]any)
	require.EqualValues(t, 10, respUsage["input_tokens"])
	require.EqualValues(t, 6, respUsage["output_tokens"])
	require.EqualValues(t, 4, respUsage["input_tokens_details"].(map[string]any)["cached_tokens"])
}

func TestOaiChatToResponsesHandlerEstimatesUsageWhenUpstreamReportsNone(t *testing.T) {
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)

	usage, apiErr := OaiChatToResponsesHandler(c, info, chatJSONResponse(`{
		"id":"chatcmpl-1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"A reasonably long answer for estimation."}}]
	}`))
	require.Nil(t, apiErr)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
	require.Contains(t, rec.Body.String(), `"status":"completed"`)
}

func TestOaiChatToResponsesHandlerPassesUpstreamErrorBody(t *testing.T) {
	c, _ := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)

	_, apiErr := OaiChatToResponsesHandler(c, info, chatJSONResponse(`{"error":{"message":"bad things","type":"invalid_request_error"}}`))
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "bad things")
}

func TestOaiChatToResponsesHandlerRejectsEmptyChoices(t *testing.T) {
	c, _ := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)

	_, apiErr := OaiChatToResponsesHandler(c, info, chatJSONResponse(`{"id":"x","choices":[]}`))
	require.NotNil(t, apiErr)
}

func TestOaiChatToResponsesStreamHandlerEventSequenceAndUsage(t *testing.T) {
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true,"instructions":"be brief"}`, t)

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`,
	))
	require.Nil(t, apiErr)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 8, usage.CompletionTokens)

	events := parseViaChatSSE(t, rec.Body.String())
	require.Equal(t, []string{
		"response.created",
		"response.in_progress",
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
	}, viaChatEventNames(events))
	for i, ev := range events {
		require.Equal(t, ev.name, ev.data["type"])
		require.EqualValues(t, i, ev.data["sequence_number"])
	}

	final := events[len(events)-1].data["response"].(map[string]any)
	require.Equal(t, "completed", final["status"])
	require.Equal(t, "gemini-4-hydrogen", final["model"])
	require.Equal(t, "be brief", final["instructions"])
	require.EqualValues(t, 20, final["usage"].(map[string]any)["total_tokens"])
	output := final["output"].([]any)
	require.Len(t, output, 2)
	require.Equal(t, "Hello", output[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
	require.Equal(t, `{"city":"Paris"}`, output[1].(map[string]any)["arguments"])
}

func TestOaiChatToResponsesStreamHandlerEstimatesUsageWhenMissing(t *testing.T) {
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"choices":[{"index":0,"delta":{"content":"An answer long enough to be counted."},"finish_reason":"stop"}]}`,
	))
	require.Nil(t, apiErr)
	require.Greater(t, usage.CompletionTokens, 0)

	events := parseViaChatSSE(t, rec.Body.String())
	final := events[len(events)-1].data["response"].(map[string]any)
	require.Equal(t, "response.completed", events[len(events)-1].name)
	require.EqualValues(t, usage.CompletionTokens, final["usage"].(map[string]any)["output_tokens"])
}

func TestOaiChatToResponsesStreamHandlerLengthFinishIsIncomplete(t *testing.T) {
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"choices":[{"index":0,"delta":{"content":"cut"},"finish_reason":"length"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
	))
	require.Nil(t, apiErr)

	events := parseViaChatSSE(t, rec.Body.String())
	require.Equal(t, "response.incomplete", events[len(events)-1].name)
	final := events[len(events)-1].data["response"].(map[string]any)
	require.Equal(t, "incomplete", final["status"])
}

func TestOaiChatToResponsesStreamHandlerMidStreamErrorFailsTheResponse(t *testing.T) {
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)

	usage, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		`{"error":{"message":"upstream exploded","type":"provider_error"}}`,
	))
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	events := parseViaChatSSE(t, rec.Body.String())
	last := events[len(events)-1]
	require.Equal(t, "response.failed", last.name)
	resp := last.data["response"].(map[string]any)
	require.Equal(t, "failed", resp["status"])
	require.Equal(t, map[string]any{"code": "provider_error", "message": "upstream exploded"}, resp["error"])
}

func TestOaiChatToResponsesStreamHandlerErrorBeforeAnyOutputIsReturned(t *testing.T) {
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"error":{"message":"no capacity","type":"overloaded_error"}}`,
	))
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "no capacity")
	require.NotContains(t, rec.Body.String(), "response.created")
}

func TestOaiChatToResponsesStreamHandlerEmptyStreamIsAnError(t *testing.T) {
	withStreamingTimeout(t)
	c, _ := newViaChatTestContext()
	info := newPoeResponsesInfo("gemini-4-hydrogen", true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse())
	require.NotNil(t, apiErr)
}
