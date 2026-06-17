package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestChatCompletionsRequestToResponsesRequestPromptCacheKeyFromMetadata(t *testing.T) {
	tests := []struct {
		name           string
		promptCacheKey string
		metadata       json.RawMessage
		want           string
	}{
		{
			name:     "user_id fallback",
			metadata: json.RawMessage(`{"user_id":"user-key"}`),
			want:     `"user-key"`,
		},
		{
			name:     "prompt_cache_key priority",
			metadata: json.RawMessage(`{"prompt_cache_key":"metadata-key","user_id":"user-key"}`),
			want:     `"metadata-key"`,
		},
		{
			name:           "top level priority",
			promptCacheKey: "top-level-key",
			metadata:       json.RawMessage(`{"user_id":"user-key"}`),
			want:           `"top-level-key"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := ChatCompletionsRequestToResponsesRequest(&dto.GeneralOpenAIRequest{
				Model:          "gpt-4o",
				Messages:       []dto.Message{{Role: "user", Content: "hi"}},
				PromptCacheKey: tt.promptCacheKey,
				Metadata:       tt.metadata,
			})
			if err != nil {
				t.Fatalf("ChatCompletionsRequestToResponsesRequest returned error: %v", err)
			}

			if string(out.PromptCacheKey) != tt.want {
				t.Fatalf("PromptCacheKey = %s, want %s", out.PromptCacheKey, tt.want)
			}
		})
	}
}
