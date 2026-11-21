package gemini

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

const includeEncryptedBody = `{"model":"gemini-3-flash-preview","input":"hi","include":["reasoning.encrypted_content"],
	"tools":[{"type":"function","name":"get_weather"},{"type":"function","name":"get_time"}]}`

func signedCall(name, signature string) dto.GeminiPart {
	part := dto.GeminiPart{FunctionCall: &dto.FunctionCall{FunctionName: name, Arguments: map[string]any{"city": "Paris"}}}
	if signature != "" {
		part.ThoughtSignature = json.RawMessage(strconv.Quote(signature))
	}
	return part
}

func runResponsesHandler(t *testing.T, body string, parts ...dto.GeminiPart) []map[string]any {
	t.Helper()
	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(responsesTestRequest(t, body))
	_, apiErr := GeminiResponsesHandler(c, info, geminiResponsesHTTPResponse(t, dto.GeminiChatResponse{
		Candidates:    textCandidate("STOP", parts...),
		UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 5, TotalTokenCount: 10},
	}))
	require.Nil(t, apiErr)

	var got struct {
		Output []map[string]any `json:"output"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &got))
	return got.Output
}

func itemTypes(output []map[string]any) []string {
	types := make([]string, 0, len(output))
	for _, item := range output {
		types = append(types, item["type"].(string))
	}
	return types
}

func TestGeminiResponsesHandlerCarriesSignatureOnReasoningItem(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "thinking it over", Thought: true},
		signedCall("get_weather", "SIG-1"),
		signedCall("get_time", ""),
	)

	require.Equal(t, []string{"reasoning", "function_call", "function_call"}, itemTypes(output))
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG-1", output[0]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "thinking it over"}}, output[0]["summary"])
	require.NotContains(t, output[1], "encrypted_content")
}

func TestGeminiResponsesHandlerAddsSignatureOnlyReasoningItem(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "checking"},
		signedCall("get_weather", "SIG-1"),
		signedCall("get_time", "SIG-2"),
	)

	require.Equal(t, []string{"message", "reasoning", "function_call", "reasoning", "function_call"}, itemTypes(output))
	require.Equal(t, []any{}, output[1]["summary"])
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG-1", output[1]["encrypted_content"])
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG-2", output[3]["encrypted_content"])
}

func TestGeminiResponsesHandlerOmitsSignatureUnlessRequested(t *testing.T) {
	output := runResponsesHandler(t, `{"model":"gemini-3-flash-preview","input":"hi","tools":[{"type":"function","name":"get_weather"}]}`,
		dto.GeminiPart{Text: "thinking it over", Thought: true},
		signedCall("get_weather", "SIG-1"),
	)

	require.Equal(t, []string{"reasoning", "function_call"}, itemTypes(output))
	require.NotContains(t, output[0], "encrypted_content")
}

func TestGeminiResponsesStreamHandlerCarriesSignature(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(responsesTestRequest(t, includeEncryptedBody))
	info.IsStream = true

	_, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t,
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "Let me check."})},
		dto.GeminiChatResponse{
			Candidates:    textCandidate("STOP", signedCall("get_weather", "SIG-1")),
			UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 5, TotalTokenCount: 10},
		},
	))
	require.Nil(t, apiErr)

	var names []string
	var doneReasoning map[string]any
	for _, ev := range parseResponsesSSE(t, rec.Body.String()) {
		names = append(names, ev.name)
		if ev.name == "response.output_item.done" {
			if item := ev.data["item"].(map[string]any); item["type"] == "reasoning" {
				doneReasoning = item
			}
		}
	}

	require.NotNil(t, doneReasoning)
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG-1", doneReasoning["encrypted_content"])
	require.Equal(t, []any{}, doneReasoning["summary"])
	require.NotContains(t, names, "response.reasoning_summary_part.added")
	require.NotContains(t, names, "response.reasoning_summary_text.done")

	events := parseResponsesSSE(t, rec.Body.String())
	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.name)
	output := final.data["response"].(map[string]any)["output"].([]any)
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG-1", output[1].(map[string]any)["encrypted_content"])
}

func replayRequest(t *testing.T, output []map[string]any, callID string) *dto.OpenAIResponsesRequest {
	t.Helper()
	input := []any{map[string]any{"type": "message", "role": "user", "content": "weather in Paris?"}}
	for _, item := range output {
		input = append(input, item)
	}
	input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": "sunny"})
	body, err := common.Marshal(map[string]any{
		"model": "gemini-3-flash-preview",
		"input": input,
		"tools": []any{map[string]any{"type": "function", "name": "get_weather"}},
	})
	require.NoError(t, err)
	return responsesTestRequest(t, string(body))
}

func TestGeminiResponsesSignatureSurvivesReplay(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "thinking it over", Thought: true},
		signedCall("get_weather", "SIG-1"),
		signedCall("get_time", ""),
	)

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayRequest(t, output, output[1]["call_id"].(string)))
	require.NoError(t, err)
	geminiRequest := got.(*dto.GeminiChatRequest)

	require.Len(t, geminiRequest.Contents, 3)
	parts := geminiRequest.Contents[1].Parts
	require.Len(t, parts, 2)
	require.Equal(t, `"SIG-1"`, string(parts[0].ThoughtSignature))
	require.Empty(t, parts[1].ThoughtSignature)
}

func TestGeminiResponsesIgnoresSignatureFromAnotherModel(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedCall("get_weather", "SIG-1"))

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-3-pro-preview"
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayRequest(t, output, output[1]["call_id"].(string)))
	require.NoError(t, err)

	signature := string(got.(*dto.GeminiChatRequest).Contents[1].Parts[0].ThoughtSignature)
	require.NotEqual(t, `"SIG-1"`, signature)
	require.Equal(t, strconv.Quote(thoughtSignatureBypassValue), signature)
}

func TestDecodeEncryptedReasoning(t *testing.T) {
	signature, forMessage, ok := decodeEncryptedReasoning("Gemini-3-Flash", encodeEncryptedReasoning(callSignaturePrefix, "gemini-3-flash", "AY89a1+/="))
	require.True(t, ok)
	require.False(t, forMessage)
	require.Equal(t, "AY89a1+/=", signature)

	signature, forMessage, ok = decodeEncryptedReasoning("gemini-3-flash", encodeEncryptedReasoning(messageSignaturePrefix, "gemini-3-flash", "MSG"))
	require.True(t, ok)
	require.True(t, forMessage)
	require.Equal(t, "MSG", signature)

	for _, encrypted := range []string{
		"",
		"gAAAAABn-openai-ciphertext",
		"gemini-sig-v1:gemini-3-flash",
		"gemini-sig-v1:gemini-3-flash:",
		"gemini-msg-sig-v1:gemini-3-flash:",
		encodeEncryptedReasoning(callSignaturePrefix, "gemini-3-pro", "SIG"),
		encodeEncryptedReasoning(messageSignaturePrefix, "gemini-3-pro", "SIG"),
	} {
		_, _, ok := decodeEncryptedReasoning("gemini-3-flash", encrypted)
		require.False(t, ok, encrypted)
	}
}

func signedText(text, signature string) dto.GeminiPart {
	return dto.GeminiPart{Text: text, ThoughtSignature: json.RawMessage(strconv.Quote(signature))}
}

func signedImage(signature string) dto.GeminiPart {
	return dto.GeminiPart{
		InlineData:       &dto.GeminiInlineData{MimeType: "image/png", Data: "AAAA"},
		ThoughtSignature: json.RawMessage(strconv.Quote(signature)),
	}
}

func messageText(item map[string]any) string {
	return item["content"].([]any)[0].(map[string]any)["text"].(string)
}

func TestGeminiResponsesHandlerCarriesMessageSignatureBehindMessage(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Hello", "SIG-M"))

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "Hello", messageText(output[0]))
	require.Equal(t, []any{}, output[1]["summary"])
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-M", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerCarriesSignatureOfTrailingEmptyPart(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "Hel"},
		dto.GeminiPart{Text: "lo"},
		signedText("", "SIG-M"),
	)

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "Hello", messageText(output[0]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-M", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerSplitsMessageAtSignedPart(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		signedText("first", "SIG-1"),
		dto.GeminiPart{Text: "second"},
		signedText("", "SIG-2"),
	)

	require.Equal(t, []string{"message", "reasoning", "message", "reasoning"}, itemTypes(output))
	require.Equal(t, "first", messageText(output[0]))
	require.Equal(t, "second", messageText(output[2]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-1", output[1]["encrypted_content"])
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-2", output[3]["encrypted_content"])
}

func TestGeminiResponsesHandlerCarriesImageSignature(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, dto.GeminiPart{Text: "here"}, signedImage("SIG-I"))

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "here\n![image](data:image/png;base64,AAAA)", messageText(output[0]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-I", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerOmitsMessageSignatureUnlessRequested(t *testing.T) {
	output := runResponsesHandler(t, `{"model":"gemini-3-flash-preview","input":"hi"}`, signedText("Hello", "SIG-M"))

	require.Equal(t, []string{"message"}, itemTypes(output))
}

func TestGeminiResponsesHandlerIgnoresMessageSignatureWithoutMessage(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedCall("get_weather", ""), signedText("", "SIG-M"))

	require.Equal(t, []string{"function_call"}, itemTypes(output))
}

func TestGeminiResponsesStreamHandlerCarriesMessageSignature(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(responsesTestRequest(t, includeEncryptedBody))
	info.IsStream = true

	_, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t,
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "Hel"})},
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "lo"})},
		dto.GeminiChatResponse{
			Candidates:    textCandidate("STOP", signedText("", "SIG-M")),
			UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 5, TotalTokenCount: 10},
		},
	))
	require.Nil(t, apiErr)

	events := parseResponsesSSE(t, rec.Body.String())
	var doneItems []map[string]any
	for _, ev := range events {
		switch ev.name {
		case "response.output_item.done":
			doneItems = append(doneItems, ev.data["item"].(map[string]any))
		case "response.reasoning_summary_part.added", "response.reasoning_summary_text.done":
			t.Fatalf("a signature-only reasoning item must not open a summary part: %s", ev.name)
		}
	}
	require.Equal(t, []string{"message", "reasoning"}, itemTypes(doneItems))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-M", doneItems[1]["encrypted_content"])

	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.name)
	output := final.data["response"].(map[string]any)["output"].([]any)
	require.Len(t, output, 2)
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG-M", output[1].(map[string]any)["encrypted_content"])
}

func replayInput(t *testing.T, output []map[string]any, tail ...map[string]any) *dto.OpenAIResponsesRequest {
	t.Helper()
	input := []any{map[string]any{"type": "message", "role": "user", "content": "draw a cat"}}
	for _, item := range output {
		input = append(input, item)
	}
	for _, item := range tail {
		input = append(input, item)
	}
	body, err := common.Marshal(map[string]any{
		"model": "gemini-3-flash-preview",
		"input": input,
		"tools": []any{map[string]any{"type": "function", "name": "get_weather"}},
	})
	require.NoError(t, err)
	return responsesTestRequest(t, string(body))
}

func userTurn(text string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": text}
}

func TestGeminiResponsesMessageSignatureSurvivesReplay(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, dto.GeminiPart{Text: "Hello"}, signedText("", "SIG-M"))

	c, _ := newResponsesTestContext()
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	contents := got.(*dto.GeminiChatRequest).Contents
	require.Len(t, contents, 3)
	require.Equal(t, "model", contents[1].Role)
	require.Equal(t, "Hello", contents[1].Parts[0].Text)
	require.Equal(t, `"SIG-M"`, string(contents[1].Parts[0].ThoughtSignature))
}

func TestGeminiResponsesImageSignatureSurvivesReplay(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedImage("SIG-I"))

	c, _ := newResponsesTestContext()
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, userTurn("make it blue")))
	require.NoError(t, err)

	part := got.(*dto.GeminiChatRequest).Contents[1].Parts[0]
	require.NotNil(t, part.InlineData)
	require.Equal(t, `"SIG-I"`, string(part.ThoughtSignature))
}

func TestGeminiResponsesMessageAndCallSignaturesSurviveReplayTogether(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Let me check", "SIG-M"), signedCall("get_weather", "SIG-C"))
	require.Equal(t, []string{"message", "reasoning", "reasoning", "function_call"}, itemTypes(output))

	c, _ := newResponsesTestContext()
	callOutput := map[string]any{"type": "function_call_output", "call_id": output[3]["call_id"], "output": "sunny"}
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, callOutput))
	require.NoError(t, err)

	contents := got.(*dto.GeminiChatRequest).Contents
	require.Len(t, contents, 3)
	parts := contents[1].Parts
	require.Len(t, parts, 2)
	require.NotNil(t, parts[0].FunctionCall)
	require.Equal(t, `"SIG-C"`, string(parts[0].ThoughtSignature))
	require.Equal(t, "Let me check", parts[1].Text)
	require.Equal(t, `"SIG-M"`, string(parts[1].ThoughtSignature))
}

func TestGeminiResponsesIgnoresMessageSignatureFromAnotherModel(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Hello", "SIG-M"))

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-3-pro-preview"
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	require.Equal(t, strconv.Quote(thoughtSignatureBypassValue), string(got.(*dto.GeminiChatRequest).Contents[1].Parts[0].ThoughtSignature))
}
