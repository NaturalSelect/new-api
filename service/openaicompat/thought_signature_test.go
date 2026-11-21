package openaicompat

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestThoughtSignatureExtraContentRoundTrip(t *testing.T) {
	raw := ExtraContentFromThoughtSignature("AY89a1+/=")
	require.JSONEq(t, `{"google":{"thought_signature":"AY89a1+/="}}`, string(raw))
	require.Equal(t, "AY89a1+/=", ThoughtSignatureFromExtraContent(raw))
}

func testSignatureDecoder(encrypted string) (string, bool, bool) {
	if signature, ok := strings.CutPrefix(encrypted, "call:"); ok {
		return signature, false, true
	}
	signature, ok := strings.CutPrefix(encrypted, "msg:")
	return signature, true, ok
}

func TestResponsesInputBindsReasoningSignatures(t *testing.T) {
	req := parseResponsesRequest(t, `{"model":"m","input":[
		{"type":"message","role":"user","content":"hi"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"checking"}]},
		{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"msg:M"},
		{"type":"reasoning","id":"rs_2","summary":[],"encrypted_content":"call:A"},
		{"type":"function_call","call_id":"call_a","name":"f","arguments":"{}"},
		{"type":"function_call","call_id":"call_b","name":"f","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_a","output":"ok"},
		{"type":"function_call_output","call_id":"call_b","output":"ok"},
		{"type":"reasoning","id":"rs_3","summary":[],"encrypted_content":"foreign-ciphertext"},
		{"type":"custom_tool_call","call_id":"call_c","name":"p","input":"x"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]},
		{"type":"reasoning","id":"rs_4","summary":[],"encrypted_content":"msg:D"},
		{"type":"reasoning","id":"rs_5","summary":[],"encrypted_content":"msg:E"},
		{"type":"reasoning","id":"rs_6","summary":[],"encrypted_content":"call:Z"},
		{"type":"message","role":"user","content":"again"},
		{"type":"reasoning","id":"rs_7","summary":[],"encrypted_content":"msg:X"}
	]}`)

	out, err := ResponsesRequestToChatCompletionsRequestWithSignatures(req, testSignatureDecoder)
	require.NoError(t, err)
	require.Len(t, out.Messages, 7)

	require.Equal(t, "M", ThoughtSignatureFromExtraContent(out.Messages[1].ExtraContent))
	calls := out.Messages[1].ParseToolCalls()
	require.Len(t, calls, 2)
	require.Equal(t, "A", ThoughtSignatureFromExtraContent(calls[0].ExtraContent))
	require.Empty(t, calls[1].ExtraContent)

	require.Empty(t, out.Messages[4].ExtraContent)
	require.Empty(t, out.Messages[4].ParseToolCalls()[0].ExtraContent)
	require.Equal(t, "D", ThoughtSignatureFromExtraContent(out.Messages[5].ExtraContent))
	require.Empty(t, out.Messages[6].ExtraContent)

	plain, err := ResponsesRequestToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, plain.Messages, 7)
	for _, msg := range plain.Messages {
		require.Empty(t, msg.ExtraContent)
		for _, call := range msg.ParseToolCalls() {
			require.Empty(t, call.ExtraContent)
		}
	}
}

func TestThoughtSignatureHelpersIgnoreMissingOrMalformedInput(t *testing.T) {
	require.Nil(t, ExtraContentFromThoughtSignature(""))
	require.Empty(t, ThoughtSignatureFromExtraContent(nil))
	require.Empty(t, ThoughtSignatureFromExtraContent([]byte(`not json`)))
	require.Empty(t, ThoughtSignatureFromExtraContent([]byte(`{"google":{}}`)))
	require.Empty(t, ThoughtSignatureFromExtraContent([]byte(`{"google":{"thought_signature":5}}`)))
	require.Empty(t, ThoughtSignatureFromExtraContent([]byte(`{"other":{"thought_signature":"x"}}`)))
}

func TestToolCallDTOsKeepExtraContent(t *testing.T) {
	var delta dto.ToolCallResponse
	require.NoError(t, common.UnmarshalJsonStr(
		`{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":""},"extra_content":{"google":{"thought_signature":"S"}}}`, &delta))
	require.Equal(t, "S", ThoughtSignatureFromExtraContent(delta.ExtraContent))

	msg := dto.Message{Role: "assistant"}
	msg.SetToolCalls([]dto.ToolCallRequest{{
		ID: "c1", Type: "function", Function: dto.FunctionRequest{Name: "f"},
		ExtraContent: ExtraContentFromThoughtSignature("S"),
	}})
	calls := msg.ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "S", ThoughtSignatureFromExtraContent(calls[0].ExtraContent))

	plain := dto.Message{Role: "assistant"}
	plain.SetToolCalls([]dto.ToolCallRequest{{ID: "c2", Type: "function", Function: dto.FunctionRequest{Name: "f"}}})
	require.NotContains(t, string(plain.ToolCalls), "extra_content")
}

func TestChatToResponsesStreamCollectsThoughtSignatureByCallID(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)
	feedChunks(t, tr,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f","arguments":""},"extra_content":{"google":{"thought_signature":"SIG-A"}}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)

	require.Equal(t, map[string]string{"call_a": "SIG-A"}, tr.ThoughtSignatures())
}

func TestChatToResponsesStreamAdoptsThoughtSignatureArrivingInALaterFragment(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)
	feedChunks(t, tr,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f","arguments":"{"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"},"extra_content":{"google":{"thought_signature":"SIG-LATE"}}}]}}]}`,
	)

	require.Equal(t, map[string]string{"call_a": "SIG-LATE"}, tr.ThoughtSignatures())
}

func TestChatToResponsesStreamKeepsFirstThoughtSignatureOfACall(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)
	feedChunks(t, tr,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f","arguments":""},"extra_content":{"google":{"thought_signature":"FIRST"}}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"},"extra_content":{"google":{"thought_signature":"SECOND"}}}]}}]}`,
	)

	require.Equal(t, map[string]string{"call_a": "FIRST"}, tr.ThoughtSignatures())
}

func TestChatToResponsesNonStreamCollectsThoughtSignature(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, false)
	msg := &dto.Message{Role: "assistant"}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: "one", Arguments: `{}`}, ExtraContent: ExtraContentFromThoughtSignature("SIG-1")},
		{ID: "call_2", Type: "function", Function: dto.FunctionRequest{Name: "two", Arguments: `{}`}},
	})
	tr.ConsumeMessage(msg, "tool_calls")

	require.Equal(t, map[string]string{"call_1": "SIG-1"}, tr.ThoughtSignatures())
}

func TestChatToResponsesThoughtSignatureNeverReachesTheResponse(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)
	events := feedChunks(t, tr,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"f","arguments":"{}"},"extra_content":{"google":{"thought_signature":"SECRET-SIG"}}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	final, finalEvents := tr.Complete(&dto.Usage{})

	body, err := common.Marshal(map[string]any{"final": final, "events": append(events, finalEvents...)})
	require.NoError(t, err)
	require.NotContains(t, string(body), "SECRET-SIG")
}

func TestChatToResponsesWithoutSignaturesReportsNone(t *testing.T) {
	tr := NewChatToResponsesTranscoder("resp_1", "m", 1, nil, true)
	feedChunks(t, tr, `{"choices":[{"index":0,"delta":{"content":"hello"}}]}`)

	require.Empty(t, tr.ThoughtSignatures())
}
