package openai

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestConvertOpenAIRequestPoeCopiesUnsupportedParamsToExtraBody(t *testing.T) {
	adaptor := &Adaptor{}
	seed := 123.5
	presencePenalty := 0.0
	store := json.RawMessage(`false`)

	request := &dto.GeneralOpenAIRequest{
		Model:           "gpt-4o",
		Messages:        []dto.Message{{Role: "user", Content: "hi"}},
		PromptCacheKey:  "cache-key",
		ReasoningEffort: "medium",
		Store:           store,
		Metadata:        json.RawMessage(`{"prompt_cache_key":"metadata-key","tag":"x"}`),
		ResponseFormat:  &dto.ResponseFormat{Type: "json_object"},
		Seed:            &seed,
		PresencePenalty: &presencePenalty,
		LogitBias:       json.RawMessage(`null`),
		ExtraBody:       json.RawMessage(`{"existing":"value","prompt_cache_key":"old","reasoning_effort":"old","store":true,"seed":999,"presence_penalty":1}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, newPoeRelayInfo(), request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	if convertedRequest.PromptCacheKey != "cache-key" {
		t.Fatalf("PromptCacheKey = %q, want %q", convertedRequest.PromptCacheKey, "cache-key")
	}
	if convertedRequest.ReasoningEffort != "medium" {
		t.Fatalf("ReasoningEffort = %q, want %q", convertedRequest.ReasoningEffort, "medium")
	}
	if convertedRequest.Store == nil {
		t.Fatalf("Store = nil, want preserved value")
	}
	if convertedRequest.Metadata == nil {
		t.Fatalf("Metadata = nil, want preserved value")
	}
	if convertedRequest.ResponseFormat == nil {
		t.Fatalf("ResponseFormat = nil, want preserved value")
	}
	if convertedRequest.Seed == nil || *convertedRequest.Seed != seed {
		t.Fatalf("Seed = %v, want %v", convertedRequest.Seed, seed)
	}
	if convertedRequest.PresencePenalty == nil || *convertedRequest.PresencePenalty != 0 {
		t.Fatalf("PresencePenalty = %v, want explicit 0", convertedRequest.PresencePenalty)
	}
	if convertedRequest.LogitBias == nil {
		t.Fatalf("LogitBias = nil, want preserved null value")
	}

	extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["existing"], `"value"`)
	assertRawJSONEqual(t, extraBody["prompt_cache_key"], `"cache-key"`)
	assertRawJSONEqual(t, extraBody["reasoning_effort"], `"medium"`)
	assertRawJSONEqual(t, extraBody["store"], `false`)
	assertRawJSONEqual(t, extraBody["metadata"], `{"prompt_cache_key":"metadata-key","tag":"x"}`)
	assertRawJSONEqual(t, extraBody["response_format"], `{"type":"json_object"}`)
	assertRawJSONEqual(t, extraBody["seed"], `123.5`)
	assertRawJSONEqual(t, extraBody["presence_penalty"], `0`)
	assertRawJSONEqual(t, extraBody["logit_bias"], `null`)
}

func TestConvertOpenAIRequestPoePreservesAllowedMinimalFields(t *testing.T) {
	adaptor := &Adaptor{}
	maxTokens := uint(0)
	maxCompletionTokens := uint(0)
	temperature := 0.0
	topP := 0.0
	stream := false
	parallelToolCalls := false
	n := 2

	request := &dto.GeneralOpenAIRequest{
		Model:               "gpt-4o",
		Messages:            []dto.Message{{Role: "user", Content: "hi"}},
		MaxTokens:           &maxTokens,
		MaxCompletionTokens: &maxCompletionTokens,
		Temperature:         &temperature,
		TopP:                &topP,
		Stream:              &stream,
		StreamOptions:       &dto.StreamOptions{IncludeUsage: true},
		Stop:                []string{"stop"},
		Tools: []dto.ToolCallRequest{{
			Type: "function",
			Function: dto.FunctionRequest{
				Name: "lookup",
			},
		}},
		ToolChoice:       map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
		ParallelTooCalls: &parallelToolCalls,
		N:                &n,
		ExtraBody:        json.RawMessage(`{"existing":true}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, newPoeRelayInfo(), request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	if convertedRequest.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want %q", convertedRequest.Model, "gpt-4o")
	}
	if len(convertedRequest.Messages) != 1 {
		t.Fatalf("Messages length = %d, want 1", len(convertedRequest.Messages))
	}
	if convertedRequest.MaxTokens == nil || *convertedRequest.MaxTokens != 0 {
		t.Fatalf("MaxTokens = %v, want explicit 0", convertedRequest.MaxTokens)
	}
	if convertedRequest.MaxCompletionTokens == nil || *convertedRequest.MaxCompletionTokens != 0 {
		t.Fatalf("MaxCompletionTokens = %v, want explicit 0", convertedRequest.MaxCompletionTokens)
	}
	if convertedRequest.Temperature == nil || *convertedRequest.Temperature != 0 {
		t.Fatalf("Temperature = %v, want explicit 0", convertedRequest.Temperature)
	}
	if convertedRequest.TopP == nil || *convertedRequest.TopP != 0 {
		t.Fatalf("TopP = %v, want explicit 0", convertedRequest.TopP)
	}
	if convertedRequest.Stream == nil || *convertedRequest.Stream {
		t.Fatalf("Stream = %v, want explicit false", convertedRequest.Stream)
	}
	if convertedRequest.StreamOptions == nil || !convertedRequest.StreamOptions.IncludeUsage {
		t.Fatalf("StreamOptions = %#v, want include_usage true", convertedRequest.StreamOptions)
	}
	if convertedRequest.Stop == nil {
		t.Fatalf("Stop = nil, want preserved stop value")
	}
	if len(convertedRequest.Tools) != 1 {
		t.Fatalf("Tools length = %d, want 1", len(convertedRequest.Tools))
	}
	if convertedRequest.ToolChoice == nil {
		t.Fatalf("ToolChoice = nil, want preserved value")
	}
	if convertedRequest.ParallelTooCalls == nil || *convertedRequest.ParallelTooCalls {
		t.Fatalf("ParallelTooCalls = %v, want explicit false", convertedRequest.ParallelTooCalls)
	}
	if convertedRequest.N == nil || *convertedRequest.N != 1 {
		t.Fatalf("N = %v, want forced 1", convertedRequest.N)
	}

	extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["existing"], `true`)
	for _, key := range []string{"model", "messages", "max_tokens", "max_completion_tokens", "temperature", "top_p", "stream", "stream_options", "stop", "tools", "tool_choice", "parallel_tool_calls", "n"} {
		if _, ok := extraBody[key]; ok {
			t.Fatalf("extra_body unexpectedly contains allowed field %q: %s", key, extraBody[key])
		}
	}
}

func TestConvertOpenAIRequestPoeExtractsMetadataPromptCacheKeyToExtraBody(t *testing.T) {
	adaptor := &Adaptor{}
	request := &dto.GeneralOpenAIRequest{
		Metadata: json.RawMessage(`{"prompt_cache_key":"metadata-key","user_id":"user-key","tag":"x"}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, newPoeRelayInfo(), request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	if convertedRequest.PromptCacheKey != "metadata-key" {
		t.Fatalf("PromptCacheKey = %q, want %q", convertedRequest.PromptCacheKey, "metadata-key")
	}
	if convertedRequest.Metadata == nil {
		t.Fatalf("Metadata = nil, want preserved value")
	}

	extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["prompt_cache_key"], `"metadata-key"`)
	assertRawJSONEqual(t, extraBody["metadata"], `{"prompt_cache_key":"metadata-key","user_id":"user-key","tag":"x"}`)
}

func TestConvertOpenAIRequestExtractsMetadataUserIDPromptCacheKey(t *testing.T) {
	adaptor := &Adaptor{}

	tests := []struct {
		name      string
		info      *relaycommon.RelayInfo
		wantExtra bool
	}{
		{
			name: "openai",
			info: newOpenAIRelayInfo(),
		},
		{
			name:      "poe",
			info:      newPoeRelayInfo(),
			wantExtra: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Metadata:  json.RawMessage(`{"user_id":"user-key","tag":"x"}`),
				ExtraBody: json.RawMessage(`{"existing":true}`),
			}

			converted, err := adaptor.ConvertOpenAIRequest(nil, tt.info, request)
			if err != nil {
				t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
			}

			convertedRequest := converted.(*dto.GeneralOpenAIRequest)
			if convertedRequest.PromptCacheKey != "user-key" {
				t.Fatalf("PromptCacheKey = %q, want %q", convertedRequest.PromptCacheKey, "user-key")
			}

			extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
			assertRawJSONEqual(t, extraBody["existing"], `true`)
			if tt.wantExtra {
				assertRawJSONEqual(t, extraBody["prompt_cache_key"], `"user-key"`)
				assertRawJSONEqual(t, extraBody["metadata"], `{"user_id":"user-key","tag":"x"}`)
			} else if _, ok := extraBody["prompt_cache_key"]; ok {
				t.Fatalf("extra_body unexpectedly contains prompt_cache_key for non-Poe channel: %s", extraBody["prompt_cache_key"])
			}
		})
	}
}

func TestConvertOpenAIRequestPreservesTopLevelPromptCacheKeyOverMetadataUserID(t *testing.T) {
	adaptor := &Adaptor{}
	request := &dto.GeneralOpenAIRequest{
		PromptCacheKey: "top-level-key",
		Metadata:       json.RawMessage(`{"user_id":"user-key"}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, newPoeRelayInfo(), request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	if convertedRequest.PromptCacheKey != "top-level-key" {
		t.Fatalf("PromptCacheKey = %q, want %q", convertedRequest.PromptCacheKey, "top-level-key")
	}

	extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["prompt_cache_key"], `"top-level-key"`)
}

func TestConvertOpenAIResponsesRequestExtractsMetadataUserIDPromptCacheKey(t *testing.T) {
	adaptor := &Adaptor{}

	tests := []struct {
		name           string
		promptCacheKey json.RawMessage
		metadata       json.RawMessage
		want           string
	}{
		{
			name:     "missing prompt cache key",
			metadata: json.RawMessage(`{"user_id":"user-key"}`),
			want:     `"user-key"`,
		},
		{
			name:           "empty string prompt cache key",
			promptCacheKey: json.RawMessage(`""`),
			metadata:       json.RawMessage(`{"user_id":"user-key"}`),
			want:           `"user-key"`,
		},
		{
			name:           "null prompt cache key",
			promptCacheKey: json.RawMessage(`null`),
			metadata:       json.RawMessage(`{"user_id":"user-key"}`),
			want:           `"user-key"`,
		},
		{
			name:           "top level prompt cache key wins",
			promptCacheKey: json.RawMessage(`"top-level-key"`),
			metadata:       json.RawMessage(`{"user_id":"user-key"}`),
			want:           `"top-level-key"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := dto.OpenAIResponsesRequest{
				Model:          "gpt-4o",
				PromptCacheKey: tt.promptCacheKey,
				Metadata:       tt.metadata,
			}

			converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, newOpenAIRelayInfo(), request)
			if err != nil {
				t.Fatalf("ConvertOpenAIResponsesRequest returned error: %v", err)
			}

			convertedRequest := converted.(dto.OpenAIResponsesRequest)
			assertRawJSONEqual(t, convertedRequest.PromptCacheKey, tt.want)
			assertRawJSONEqual(t, convertedRequest.Metadata, string(tt.metadata))
		})
	}
}

func TestConvertOpenAIRequestNonPoeDoesNotMergeOrStripUnsupportedParams(t *testing.T) {
	adaptor := &Adaptor{}
	seed := 123.5
	request := &dto.GeneralOpenAIRequest{
		PromptCacheKey:  "cache-key",
		ReasoningEffort: "medium",
		Store:           json.RawMessage(`false`),
		Metadata:        json.RawMessage(`{"tag":"x"}`),
		ResponseFormat:  &dto.ResponseFormat{Type: "json_object"},
		Seed:            &seed,
		ExtraBody:       json.RawMessage(`{"existing":"value"}`),
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, newOpenAIRelayInfo(), request)
	if err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	convertedRequest := converted.(*dto.GeneralOpenAIRequest)
	if convertedRequest.PromptCacheKey != "cache-key" {
		t.Fatalf("PromptCacheKey = %q, want %q", convertedRequest.PromptCacheKey, "cache-key")
	}
	if convertedRequest.ReasoningEffort != "medium" {
		t.Fatalf("ReasoningEffort = %q, want %q", convertedRequest.ReasoningEffort, "medium")
	}
	if convertedRequest.Store == nil {
		t.Fatalf("Store = nil, want preserved value")
	}
	if convertedRequest.Metadata == nil {
		t.Fatalf("Metadata = nil, want preserved value")
	}
	if convertedRequest.ResponseFormat == nil {
		t.Fatalf("ResponseFormat = nil, want preserved value")
	}
	if convertedRequest.Seed == nil || *convertedRequest.Seed != seed {
		t.Fatalf("Seed = %v, want %v", convertedRequest.Seed, seed)
	}

	extraBody := unmarshalRawObject(t, convertedRequest.ExtraBody)
	assertRawJSONEqual(t, extraBody["existing"], `"value"`)
	for _, key := range []string{"prompt_cache_key", "reasoning_effort", "store", "metadata", "response_format", "seed"} {
		if _, ok := extraBody[key]; ok {
			t.Fatalf("extra_body unexpectedly contains %q for non-Poe channel: %s", key, extraBody[key])
		}
	}
}

func newPoeRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypePoeOpenAI,
			UpstreamModelName: "gpt-4o",
		},
	}
}

func newOpenAIRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gpt-4o",
		},
	}
}

func unmarshalRawObject(t *testing.T, data json.RawMessage) map[string]json.RawMessage {
	t.Helper()

	var object map[string]json.RawMessage
	if err := common.Unmarshal(data, &object); err != nil {
		t.Fatalf("failed to unmarshal JSON object: %v", err)
	}
	return object
}

func assertRawJSONEqual(t *testing.T, actual json.RawMessage, want string) {
	t.Helper()

	if string(actual) != want {
		t.Fatalf("JSON value = %s, want %s", actual, want)
	}
}
