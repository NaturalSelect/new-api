package claude

import (
	"encoding/base64"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
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

		decoded, kind := decodeClaudeThinking(enc)
		require.Equal(t, openaicompat.ReasoningSignatureForBlock, kind)
		var block openaicompat.ClaudeThinkingBlock
		require.NoError(t, common.UnmarshalJsonStr(decoded, &block))
		require.Equal(t, openaicompat.ClaudeThinkingBlock{
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

		decoded, kind := decodeClaudeThinking(enc)
		require.Equal(t, openaicompat.ReasoningSignatureForBlock, kind)
		var block openaicompat.ClaudeThinkingBlock
		require.NoError(t, common.UnmarshalJsonStr(decoded, &block))
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

		decoded, kind := decodeClaudeThinking(enc)
		require.Equal(t, openaicompat.ReasoningSignatureForBlock, kind)
		var block openaicompat.ClaudeThinkingBlock
		require.NoError(t, common.UnmarshalJsonStr(decoded, &block))
		require.Equal(t, openaicompat.ClaudeThinkingBlock{
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

		decoded, kind := decodeClaudeThinking(enc)
		require.Equal(t, openaicompat.ReasoningSignatureForBlock, kind)
		var block openaicompat.ClaudeThinkingBlock
		require.NoError(t, common.UnmarshalJsonStr(decoded, &block))
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
			decoded, kind := decodeClaudeThinking(bad)
			require.Empty(t, decoded, bad)
			require.Equal(t, openaicompat.ReasoningSignatureNone, kind, bad)
		}
	})

	t.Run("encode returns empty when signature or data missing", func(t *testing.T) {
		text := "no sig"
		require.Empty(t, encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "thinking", Thinking: &text}))
		require.Empty(t, encodeClaudeThinking("claude-sonnet-4-5", dto.ClaudeMediaMessage{Type: "redacted_thinking"}))
		require.Empty(t, encodeClaudeThinking("", dto.ClaudeMediaMessage{Type: "redacted_thinking", Data: "x"}))
	})
}
