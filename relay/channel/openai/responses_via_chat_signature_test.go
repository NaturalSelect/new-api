package openai

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/stretchr/testify/require"
)

const viaChatSignatureModel = "gemini-4-hydrogen"

// replayToolCallTurn converts the next Responses request of a tool loop, in which the client
// sends back the function_call it received together with its output.
func replayToolCallTurn(t *testing.T, userID int, model, callID string) []dto.Message {
	t.Helper()
	info := newPoeResponsesInfo(model, false, `{"model":"`+model+`","input":"x"}`, t)
	info.UserId = userID
	input := fmt.Sprintf(`[
		{"role":"user","content":"weather in Paris?"},
		{"type":"function_call","call_id":%q,"name":"get_weather","arguments":"{\"city\":\"Paris\"}"},
		{"type":"function_call_output","call_id":%q,"output":"18C"}
	]`, callID, callID)

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: model,
		Input: []byte(input),
	})
	require.NoError(t, err)
	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected a chat request, got %T", converted)
	return chatRequest.Messages
}

func replayedSignature(t *testing.T, messages []dto.Message) string {
	t.Helper()
	for i := range messages {
		if calls := messages[i].ParseToolCalls(); len(calls) > 0 {
			return openaicompat.ThoughtSignatureFromExtraContent(calls[0].ExtraContent)
		}
	}
	t.Fatal("no assistant tool call in the converted request")
	return ""
}

func TestResponsesViaChatRestoresThoughtSignatureOnNextTurn(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo(viaChatSignatureModel, false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)
	info.UserId = 71

	_, apiErr := OaiChatToResponsesHandler(c, info, chatJSONResponse(`{
		"id":"chatcmpl-1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"",
			"tool_calls":[{"id":"call_sig_nonstream","type":"function",
				"extra_content":{"google":{"thought_signature":"SIG-NONSTREAM"}},
				"function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}]}}],
		"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16}
	}`))
	require.Nil(t, apiErr)
	require.NotContains(t, rec.Body.String(), "SIG-NONSTREAM", "the signature must not leak to the Responses client")

	messages := replayToolCallTurn(t, 71, viaChatSignatureModel, "call_sig_nonstream")
	require.Equal(t, "SIG-NONSTREAM", replayedSignature(t, messages))

	wire, err := common.Marshal(messages)
	require.NoError(t, err)
	require.Contains(t, string(wire), `"extra_content":{"google":{"thought_signature":"SIG-NONSTREAM"}}`)
}

func TestResponsesViaChatStreamStoresThoughtSignature(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())
	withStreamingTimeout(t)
	c, rec := newViaChatTestContext()
	info := newPoeResponsesInfo(viaChatSignatureModel, true, `{"model":"gemini-4-hydrogen","input":"hi","stream":true}`, t)
	info.UserId = 72

	_, apiErr := OaiChatToResponsesStreamHandler(c, info, chatSSEResponse(
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_sig_stream_1","type":"function","function":{"name":"get_weather","arguments":""},"extra_content":{"google":{"thought_signature":"SIG-STREAM-1"}}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"Paris\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_sig_stream_2","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14}}`,
	))
	require.Nil(t, apiErr)
	require.NotContains(t, rec.Body.String(), "SIG-STREAM-1", "the signature must not leak to the Responses client")

	require.Equal(t, "SIG-STREAM-1", replayedSignature(t, replayToolCallTurn(t, 72, viaChatSignatureModel, "call_sig_stream_1")))
	require.Empty(t, replayedSignature(t, replayToolCallTurn(t, 72, viaChatSignatureModel, "call_sig_stream_2")),
		"an unsigned parallel call must not borrow another call's signature")
}

func TestResponsesViaChatThoughtSignatureIsScopedToUserAndModel(t *testing.T) {
	setPoeCatalogForTest(t, map[string]service.PoeModelInfo{
		"gemini-4-hydrogen": {},
		"gemini-3.8-flash":  {},
	})
	c, _ := newViaChatTestContext()
	info := newPoeResponsesInfo(viaChatSignatureModel, false, `{"model":"gemini-4-hydrogen","input":"hi"}`, t)
	info.UserId = 73

	_, apiErr := OaiChatToResponsesHandler(c, info, chatJSONResponse(`{
		"id":"chatcmpl-1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"",
			"tool_calls":[{"id":"call_sig_scope","type":"function",
				"extra_content":{"google":{"thought_signature":"SIG-SCOPE"}},
				"function":{"name":"get_weather","arguments":"{}"}}]}}],
		"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
	}`))
	require.Nil(t, apiErr)

	require.Equal(t, "SIG-SCOPE", replayedSignature(t, replayToolCallTurn(t, 73, viaChatSignatureModel, "call_sig_scope")))
	require.Empty(t, replayedSignature(t, replayToolCallTurn(t, 74, viaChatSignatureModel, "call_sig_scope")), "another user must not read it")
	require.Empty(t, replayedSignature(t, replayToolCallTurn(t, 73, "gemini-3.8-flash", "call_sig_scope")), "another model must not read it")
}

func TestResponsesViaChatReplayWithoutStoredSignatureLeavesToolCallUntouched(t *testing.T) {
	setPoeCatalogForTest(t, chatOnlyGeminiCatalog())

	messages := replayToolCallTurn(t, 75, viaChatSignatureModel, "call_sig_never_seen")
	require.Empty(t, replayedSignature(t, messages))
	for i := range messages {
		for _, call := range messages[i].ParseToolCalls() {
			require.Empty(t, call.ExtraContent)
		}
	}
}
