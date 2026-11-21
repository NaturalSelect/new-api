package gemini

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
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
		signedCall("get_weather", "SIG1"),
		signedCall("get_time", ""),
	)

	require.Equal(t, []string{"reasoning", "function_call", "function_call"}, itemTypes(output))
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG1", output[0]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "thinking it over"}}, output[0]["summary"])
	require.NotContains(t, output[1], "encrypted_content")
}

func TestGeminiResponsesHandlerAddsSignatureOnlyReasoningItem(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "checking"},
		signedCall("get_weather", "SIG1"),
		signedCall("get_time", "SIG2"),
	)

	require.Equal(t, []string{"message", "reasoning", "function_call", "reasoning", "function_call"}, itemTypes(output))
	require.Equal(t, []any{}, output[1]["summary"])
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG1", output[1]["encrypted_content"])
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG2", output[3]["encrypted_content"])
}

func TestGeminiResponsesHandlerOmitsSignatureUnlessRequested(t *testing.T) {
	output := runResponsesHandler(t, `{"model":"gemini-3-flash-preview","input":"hi","tools":[{"type":"function","name":"get_weather"}]}`,
		dto.GeminiPart{Text: "thinking it over", Thought: true},
		signedCall("get_weather", "SIG1"),
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
			Candidates:    textCandidate("STOP", signedCall("get_weather", "SIG1")),
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
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG1", doneReasoning["encrypted_content"])
	require.Equal(t, []any{}, doneReasoning["summary"])
	require.NotContains(t, names, "response.reasoning_summary_part.added")
	require.NotContains(t, names, "response.reasoning_summary_text.done")

	events := parseResponsesSSE(t, rec.Body.String())
	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.name)
	output := final.data["response"].(map[string]any)["output"].([]any)
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIG1", output[1].(map[string]any)["encrypted_content"])
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
		signedCall("get_weather", "SIG1"),
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
	require.Equal(t, `"SIG1"`, string(parts[0].ThoughtSignature))
	require.Empty(t, parts[1].ThoughtSignature)
}

func TestGeminiResponsesIgnoresSignatureFromAnotherModel(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedCall("get_weather", "SIG1"))

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-3-pro-preview"
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayRequest(t, output, output[1]["call_id"].(string)))
	require.NoError(t, err)

	signature := string(got.(*dto.GeminiChatRequest).Contents[1].Parts[0].ThoughtSignature)
	require.NotEqual(t, `"SIG1"`, signature)
	require.Equal(t, strconv.Quote(thoughtSignatureBypassValue), signature)
}

func TestDecodeEncryptedReasoning(t *testing.T) {
	kinds := []openaicompat.ReasoningSignatureKind{
		openaicompat.ReasoningSignatureForCall,
		openaicompat.ReasoningSignatureForMessage,
		openaicompat.ReasoningSignatureForThought,
	}
	for _, kind := range kinds {
		signature, got := decodeEncryptedReasoning("Gemini-3-Flash", encodeEncryptedReasoning(kind, "gemini-3-flash", "AY89a1+/"))
		require.Equal(t, kind, got)
		require.Equal(t, "AY89a1+/", signature)
	}

	for _, signature := range []string{"AY89a1+/", "AY89a1-_", "U0lHLTE=", "U0lHLTE"} {
		_, kind := decodeEncryptedReasoning("gemini-3-flash", encodeEncryptedReasoning(openaicompat.ReasoningSignatureForCall, "gemini-3-flash", signature))
		require.Equal(t, openaicompat.ReasoningSignatureForCall, kind, signature)
	}

	rejected := []string{
		"",
		"gAAAAABn-openai-ciphertext",
		"gemini-sig-v1:gemini-3-flash",
		"gemini-sig-v1:gemini-3-flash:",
		"gemini-msg-sig-v1:gemini-3-flash:",
		"gemini-thought-sig-v1:gemini-3-flash:",
		"gemini-sig-v1:gemini-3-flash:not*base64",
		"gemini-sig-v1:gemini-3-flash:SIG-1",
		"gemini-msg-sig-v1:gemini-3-flash:has space",
	}
	for _, kind := range kinds {
		rejected = append(rejected, encodeEncryptedReasoning(kind, "gemini-3-pro", "AY89a1+/"))
	}
	for _, encrypted := range rejected {
		signature, kind := decodeEncryptedReasoning("gemini-3-flash", encrypted)
		require.Equal(t, openaicompat.ReasoningSignatureNone, kind, encrypted)
		require.Empty(t, signature, encrypted)
	}
}

func signedThought(text, signature string) dto.GeminiPart {
	return dto.GeminiPart{Text: text, Thought: true, ThoughtSignature: json.RawMessage(strconv.Quote(signature))}
}

func thoughtSummaries(item map[string]any) []any {
	return item["summary"].([]any)
}

func TestGeminiResponsesHandlerCarriesThoughtSignature(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		signedThought("thinking it over", "SIGT"),
		dto.GeminiPart{Text: "Hello"},
	)

	require.Equal(t, []string{"reasoning", "message"}, itemTypes(output))
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIGT", output[0]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "thinking it over"}}, thoughtSummaries(output[0]))
}

func TestGeminiResponsesHandlerEndsReasoningItemAtSignedThought(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "a", Thought: true},
		signedThought("b", "SIG1"),
		signedThought("c", "SIG2"),
		dto.GeminiPart{Text: "d", Thought: true},
	)

	require.Equal(t, []string{"reasoning", "reasoning", "reasoning"}, itemTypes(output))
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIG1", output[0]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "ab"}}, thoughtSummaries(output[0]))
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIG2", output[1]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "c"}}, thoughtSummaries(output[1]))
	require.NotContains(t, output[2], "encrypted_content")
}

func TestGeminiResponsesHandlerKeepsThoughtAndCallSignaturesApart(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedThought("plan", "SIGT"), signedCall("get_weather", "SIGC"))

	require.Equal(t, []string{"reasoning", "reasoning", "function_call"}, itemTypes(output))
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIGT", output[0]["encrypted_content"])
	require.Equal(t, "gemini-sig-v1:gemini-3-flash-preview:SIGC", output[1]["encrypted_content"])
	require.Equal(t, []any{}, thoughtSummaries(output[1]))
}

func TestGeminiResponsesHandlerIgnoresSignatureOfEmptyThoughtWithoutReasoningItem(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, dto.GeminiPart{Text: "Hello"}, signedThought("", "SIGT"))

	require.Equal(t, []string{"message"}, itemTypes(output))
}

func TestGeminiResponsesHandlerOmitsThoughtSignatureUnlessRequested(t *testing.T) {
	output := runResponsesHandler(t, `{"model":"gemini-3-flash-preview","input":"hi"}`, signedThought("plan", "SIGT"))

	require.Equal(t, []string{"reasoning"}, itemTypes(output))
	require.NotContains(t, output[0], "encrypted_content")
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "plan"}}, thoughtSummaries(output[0]))
}

func TestGeminiResponsesStreamHandlerCarriesThoughtSignature(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	c, rec := newResponsesTestContext()
	info := newResponsesTestInfo(responsesTestRequest(t, includeEncryptedBody))
	info.IsStream = true

	_, apiErr := GeminiResponsesStreamHandler(c, info, geminiResponsesStreamHTTPResponse(t,
		dto.GeminiChatResponse{Candidates: textCandidate("", dto.GeminiPart{Text: "think", Thought: true})},
		dto.GeminiChatResponse{Candidates: textCandidate("", signedThought("ing", "SIGT"))},
		dto.GeminiChatResponse{
			Candidates:    textCandidate("STOP", dto.GeminiPart{Text: "Hello"}),
			UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 5, CandidatesTokenCount: 5, TotalTokenCount: 10},
		},
	))
	require.Nil(t, apiErr)

	events := parseResponsesSSE(t, rec.Body.String())
	var names []string
	var doneItems []map[string]any
	for _, ev := range events {
		names = append(names, ev.name)
		if ev.name == "response.output_item.done" {
			doneItems = append(doneItems, ev.data["item"].(map[string]any))
		}
	}
	require.Contains(t, names, "response.reasoning_summary_text.done")
	require.Equal(t, []string{"reasoning", "message"}, itemTypes(doneItems))
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIGT", doneItems[0]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "thinking"}}, thoughtSummaries(doneItems[0]))

	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.name)
	output := final.data["response"].(map[string]any)["output"].([]any)
	require.Equal(t, "gemini-thought-sig-v1:gemini-3-flash-preview:SIGT", output[0].(map[string]any)["encrypted_content"])
}

func TestGeminiResponsesThoughtSignatureSurvivesReplay(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedThought("thinking it over", "SIGT"), dto.GeminiPart{Text: "Hello"})

	c, _ := newResponsesTestContext()
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	contents := got.(*dto.GeminiChatRequest).Contents
	require.Len(t, contents, 3)
	parts := contents[1].Parts
	require.Len(t, parts, 2)
	require.True(t, parts[0].Thought)
	require.Equal(t, "thinking it over", parts[0].Text)
	require.Equal(t, `"SIGT"`, string(parts[0].ThoughtSignature))
	require.False(t, parts[1].Thought)
	require.Equal(t, "Hello", parts[1].Text)
	require.Equal(t, strconv.Quote(thoughtSignatureBypassValue), string(parts[1].ThoughtSignature), "the placeholder must go on the answer, not over the thought signature")
}

func TestGeminiResponsesThoughtAndCallSignaturesSurviveReplayTogether(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedThought("plan", "SIGT"), signedCall("get_weather", "SIGC"))
	require.Equal(t, []string{"reasoning", "reasoning", "function_call"}, itemTypes(output))

	c, _ := newResponsesTestContext()
	callOutput := map[string]any{"type": "function_call_output", "call_id": output[2]["call_id"], "output": "sunny"}
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, callOutput))
	require.NoError(t, err)

	contents := got.(*dto.GeminiChatRequest).Contents
	require.Len(t, contents, 3)
	parts := contents[1].Parts
	require.Len(t, parts, 2)
	require.True(t, parts[0].Thought)
	require.Equal(t, `"SIGT"`, string(parts[0].ThoughtSignature))
	require.NotNil(t, parts[1].FunctionCall)
	require.Equal(t, `"SIGC"`, string(parts[1].ThoughtSignature))
}

func TestGeminiResponsesIgnoresThoughtSignatureFromAnotherModel(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedThought("plan", "SIGT"), dto.GeminiPart{Text: "Hello"})

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-3-pro-preview"
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	parts := got.(*dto.GeminiChatRequest).Contents[1].Parts
	require.Len(t, parts, 1)
	require.False(t, parts[0].Thought)
	require.Equal(t, "Hello", parts[0].Text)
}

func TestGeminiResponsesFallsBackToPlaceholderForMalformedSignature(t *testing.T) {
	const model = "gemini-3-flash-preview"
	placeholder := strconv.Quote(thoughtSignatureBypassValue)

	t.Run("tool call", func(t *testing.T) {
		request := responsesTestRequest(t, `{"model":"`+model+`","input":[
			{"type":"message","role":"user","content":"weather?"},
			{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gemini-sig-v1:`+model+`:not*base64"},
			{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}
		]}`)
		c, _ := newResponsesTestContext()
		got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *request)
		require.NoError(t, err)

		part := got.(*dto.GeminiChatRequest).Contents[1].Parts[0]
		require.NotNil(t, part.FunctionCall)
		require.Equal(t, placeholder, string(part.ThoughtSignature))
	})

	t.Run("message", func(t *testing.T) {
		request := responsesTestRequest(t, `{"model":"`+model+`","input":[
			{"type":"message","role":"user","content":"hi"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]},
			{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gemini-msg-sig-v1:`+model+`:not*base64"},
			{"type":"message","role":"user","content":"thanks"}
		]}`)
		c, _ := newResponsesTestContext()
		got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *request)
		require.NoError(t, err)

		part := got.(*dto.GeminiChatRequest).Contents[1].Parts[0]
		require.Equal(t, "Hello", part.Text)
		require.Equal(t, placeholder, string(part.ThoughtSignature))
	})

	t.Run("thought", func(t *testing.T) {
		request := responsesTestRequest(t, `{"model":"`+model+`","input":[
			{"type":"message","role":"user","content":"hi"},
			{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"gemini-thought-sig-v1:`+model+`:not*base64"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]},
			{"type":"message","role":"user","content":"thanks"}
		]}`)
		c, _ := newResponsesTestContext()
		got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *request)
		require.NoError(t, err)

		parts := got.(*dto.GeminiChatRequest).Contents[1].Parts
		require.Len(t, parts, 1)
		require.False(t, parts[0].Thought)
	})
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
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Hello", "SIGM"))

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "Hello", messageText(output[0]))
	require.Equal(t, []any{}, output[1]["summary"])
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIGM", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerCarriesSignatureOfTrailingEmptyPart(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		dto.GeminiPart{Text: "Hel"},
		dto.GeminiPart{Text: "lo"},
		signedText("", "SIGM"),
	)

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "Hello", messageText(output[0]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIGM", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerSplitsMessageAtSignedPart(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody,
		signedText("first", "SIG1"),
		dto.GeminiPart{Text: "second"},
		signedText("", "SIG2"),
	)

	require.Equal(t, []string{"message", "reasoning", "message", "reasoning"}, itemTypes(output))
	require.Equal(t, "first", messageText(output[0]))
	require.Equal(t, "second", messageText(output[2]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG1", output[1]["encrypted_content"])
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIG2", output[3]["encrypted_content"])
}

func TestGeminiResponsesHandlerCarriesImageSignature(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, dto.GeminiPart{Text: "here"}, signedImage("SIGI"))

	require.Equal(t, []string{"message", "reasoning"}, itemTypes(output))
	require.Equal(t, "here\n![image](data:image/png;base64,AAAA)", messageText(output[0]))
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIGI", output[1]["encrypted_content"])
}

func TestGeminiResponsesHandlerOmitsMessageSignatureUnlessRequested(t *testing.T) {
	output := runResponsesHandler(t, `{"model":"gemini-3-flash-preview","input":"hi"}`, signedText("Hello", "SIGM"))

	require.Equal(t, []string{"message"}, itemTypes(output))
}

func TestGeminiResponsesHandlerIgnoresMessageSignatureWithoutMessage(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedCall("get_weather", ""), signedText("", "SIGM"))

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
			Candidates:    textCandidate("STOP", signedText("", "SIGM")),
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
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIGM", doneItems[1]["encrypted_content"])

	final := events[len(events)-1]
	require.Equal(t, "response.completed", final.name)
	output := final.data["response"].(map[string]any)["output"].([]any)
	require.Len(t, output, 2)
	require.Equal(t, "gemini-msg-sig-v1:gemini-3-flash-preview:SIGM", output[1].(map[string]any)["encrypted_content"])
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
	output := runResponsesHandler(t, includeEncryptedBody, dto.GeminiPart{Text: "Hello"}, signedText("", "SIGM"))

	c, _ := newResponsesTestContext()
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	contents := got.(*dto.GeminiChatRequest).Contents
	require.Len(t, contents, 3)
	require.Equal(t, "model", contents[1].Role)
	require.Equal(t, "Hello", contents[1].Parts[0].Text)
	require.Equal(t, `"SIGM"`, string(contents[1].Parts[0].ThoughtSignature))
}

func TestGeminiResponsesImageSignatureSurvivesReplay(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedImage("SIGI"))

	c, _ := newResponsesTestContext()
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, newResponsesTestInfo(nil), *replayInput(t, output, userTurn("make it blue")))
	require.NoError(t, err)

	part := got.(*dto.GeminiChatRequest).Contents[1].Parts[0]
	require.NotNil(t, part.InlineData)
	require.Equal(t, `"SIGI"`, string(part.ThoughtSignature))
}

func TestGeminiResponsesMessageAndCallSignaturesSurviveReplayTogether(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Let me check", "SIGM"), signedCall("get_weather", "SIGC"))
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
	require.Equal(t, `"SIGC"`, string(parts[0].ThoughtSignature))
	require.Equal(t, "Let me check", parts[1].Text)
	require.Equal(t, `"SIGM"`, string(parts[1].ThoughtSignature))
}

func TestGeminiResponsesIgnoresMessageSignatureFromAnotherModel(t *testing.T) {
	output := runResponsesHandler(t, includeEncryptedBody, signedText("Hello", "SIGM"))

	c, _ := newResponsesTestContext()
	info := newResponsesTestInfo(nil)
	info.UpstreamModelName = "gemini-3-pro-preview"
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, *replayInput(t, output, userTurn("thanks")))
	require.NoError(t, err)

	require.Equal(t, strconv.Quote(thoughtSignatureBypassValue), string(got.(*dto.GeminiChatRequest).Contents[1].Parts[0].ThoughtSignature))
}
