package claude

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeClaudeThinking(t *testing.T) {
	t.Run("thinking with text and signature", func(t *testing.T) {
		text := "let me think step by step"
		enc := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &text,
			Signature: "sig_abc123",
		})
		require.NotEmpty(t, enc)

		block, ok := decodeClaudeThinkingBlock(enc)
		require.True(t, ok)
		require.Equal(t, claudeThinkingBlock{
			Model:     "claude-sonnet-4-5",
			Type:      "thinking",
			Thinking:  text,
			Signature: "sig_abc123",
		}, block)
	})

	t.Run("omitted thinking text with signature", func(t *testing.T) {
		empty := ""
		enc := encodeClaudeThinking("claude-opus-4-7", dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &empty,
			Signature: "sig_omitted",
		})
		require.NotEmpty(t, enc)

		block, ok := decodeClaudeThinkingBlock(enc)
		require.True(t, ok)
		require.Equal(t, "", block.Thinking)
		require.Equal(t, "sig_omitted", block.Signature)
		require.Equal(t, "thinking", block.Type)
	})

	t.Run("redacted thinking round trip", func(t *testing.T) {
		enc := encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{
			Type: "redacted_thinking",
			Data: "EmwKAhgB_redacted",
		})
		require.NotEmpty(t, enc)

		block, ok := decodeClaudeThinkingBlock(enc)
		require.True(t, ok)
		require.Equal(t, claudeThinkingBlock{
			Model: "claude-sonnet-4-5",
			Type:  "redacted_thinking",
			Data:  "EmwKAhgB_redacted",
		}, block)
	})

	t.Run("bedrock model id with colons", func(t *testing.T) {
		text := "bedrock thought"
		model := "anthropic.claude-sonnet-4-5-20250929-v1:0"
		enc := encodeClaudeThinking(model, dto.ClaudeMediaMessage{
			Type:      "thinking",
			Thinking:  &text,
			Signature: "sig_bedrock",
		})
		require.NotEmpty(t, enc)

		block, ok := decodeClaudeThinkingBlock(enc)
		require.True(t, ok)
		require.Equal(t, model, block.Model)
		require.Equal(t, text, block.Thinking)
		require.Equal(t, "sig_bedrock", block.Signature)
	})

	t.Run("rejects invalid inputs", func(t *testing.T) {
		missingSig := claudeThinkingMarker + "claude-sonnet-4-5:" + base64.RawURLEncoding.EncodeToString([]byte(`{"type":"thinking","thinking":"x"}`))
		missingData := claudeThinkingMarker + "claude-sonnet-4-5:" + base64.RawURLEncoding.EncodeToString([]byte(`{"type":"redacted_thinking"}`))
		unknownType := claudeThinkingMarker + "claude-sonnet-4-5:" + base64.RawURLEncoding.EncodeToString([]byte(`{"type":"text","data":"x"}`))

		for _, bad := range []string{
			"",
			"gemini-thought-sig-v1:gemini-2.5-pro:YWJj",
			"wrong-prefix:claude-sonnet-4-5:e30",
			claudeThinkingMarker + ":e30",
			claudeThinkingMarker + "claude-sonnet-4-5:",
			claudeThinkingMarker + "claude-sonnet-4-5:!!!not-base64!!!",
			missingSig,
			missingData,
			unknownType,
		} {
			_, ok := decodeClaudeThinkingBlock(bad)
			require.False(t, ok, bad)
		}
	})

	t.Run("encode returns empty when signature or data missing", func(t *testing.T) {
		text := "no sig"
		require.Empty(t, encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &text}))
		require.Empty(t, encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "redacted_thinking"}))
		require.Empty(t, encodeClaudeThinking("", dto.ClaudeMediaMessage{Type: "redacted_thinking", Data: "x"}))
	})
}

func TestCanonicalClaudeModelNameAndMatch(t *testing.T) {
	matchingGroups := [][]string{
		{
			"claude-sonnet-4-5",
			"claude-sonnet-4-5-20250929",
			"anthropic.claude-sonnet-4-5-20250929-v1:0",
			"us.anthropic.claude-sonnet-4-5-20250929-v1:0",
			"claude-sonnet-4-5@20250929",
			"publishers/anthropic/models/claude-sonnet-4-5@20250929",
			"claude-sonnet-4-5-thinking",
			"claude-sonnet-4-5-20250929-thinking",
		},
		{
			"claude-opus-4-6",
			"global.anthropic.claude-opus-4-6-v1",
			"us.anthropic.claude-opus-4-6-v1:0",
			"claude-opus-4-6-high",
		},
		{
			"claude-opus-4-7",
			"us.anthropic.claude-opus-4-7-v1:0",
			"claude-opus-4-7-xhigh",
		},
	}

	for _, group := range matchingGroups {
		for _, a := range group {
			for _, b := range group {
				require.True(t, claudeModelsMatch(a, b), "expected %q to match %q", a, b)
			}
		}
	}

	nonMatchingPairs := [][2]string{
		{"claude-sonnet-4-5", "claude-opus-4-5"},
		{"claude-opus-4-6", "claude-opus-4-7"},
		{"claude-sonnet-4-5", "claude-sonnet-4-6"},
		{"", "claude-sonnet-4-5"},
		{"claude-sonnet-4-5", ""},
	}
	for _, pair := range nonMatchingPairs {
		require.False(t, claudeModelsMatch(pair[0], pair[1]), "expected %q not to match %q", pair[0], pair[1])
	}
}

func TestAdaptiveAndOpus47ModelDetection(t *testing.T) {
	require.True(t, isClaudeAdaptiveThinkingModel("claude-opus-4-6"))
	require.True(t, isClaudeAdaptiveThinkingModel("us.anthropic.claude-opus-4-6-v1:0"))
	require.True(t, isClaudeAdaptiveThinkingModel("claude-sonnet-4-6"))
	require.True(t, isClaudeAdaptiveThinkingModel("claude-opus-4-7"))
	require.True(t, isClaudeAdaptiveThinkingModel("global.anthropic.claude-opus-4-7-v1"))
	require.False(t, isClaudeAdaptiveThinkingModel("claude-sonnet-4-5-20250929"))
	require.False(t, isClaudeAdaptiveThinkingModel("claude-3-7-sonnet@20250219"))

	require.True(t, isClaudeOpus47Model("claude-opus-4-7"))
	require.True(t, isClaudeOpus47Model("us.anthropic.claude-opus-4-7-v1:0"))
	require.False(t, isClaudeOpus47Model("claude-opus-4-6"))
}
