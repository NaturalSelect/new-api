package claude

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeRelayInfo(disguise bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguise: disguise,
			},
		},
	}
}

func makeRelayInfoNil() *relaycommon.RelayInfo {
	return nil
}

// 1. TestApplyClaudeCodeDisguiseHeaders_Disabled — switch false, headers unchanged
func TestApplyClaudeCodeDisguiseHeaders_Disabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "original-agent")
	info := makeRelayInfo(false)

	ApplyClaudeCodeDisguiseHeaders(c, &req, info)

	assert.Equal(t, "original-agent", req.Get("User-Agent"))
	assert.Equal(t, "", req.Get("X-App"))
	assert.Equal(t, "", req.Get("anthropic-beta"))
}

// 2. TestApplyClaudeCodeDisguiseHeaders_Enabled — switch true, headers injected
func TestApplyClaudeCodeDisguiseHeaders_Enabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)

	t.Run("no existing anthropic-beta", func(t *testing.T) {
		req := http.Header{}
		info := makeRelayInfo(true)
		ApplyClaudeCodeDisguiseHeaders(c, &req, info)

		assert.Equal(t, dto.ClaudeCodeDisguiseUserAgent, req.Get("User-Agent"))
		assert.Equal(t, dto.ClaudeCodeDisguiseXApp, req.Get("X-App"))
		assert.Equal(t, claudeCodeDefaultBeta, req.Get("anthropic-beta"))
	})

	t.Run("existing anthropic-beta preserved", func(t *testing.T) {
		req := http.Header{}
		req.Set("anthropic-beta", "custom-beta-value")
		info := makeRelayInfo(true)
		ApplyClaudeCodeDisguiseHeaders(c, &req, info)

		assert.Equal(t, "custom-beta-value", req.Get("anthropic-beta"))
	})
}

// 3. TestApplyClaudeCodeDisguiseBody_Disabled — switch false, body unchanged
func TestApplyClaudeCodeDisguiseBody_Disabled(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		Model: "claude-3-5-sonnet",
	}
	info := makeRelayInfo(false)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	assert.Nil(t, request.System)
	assert.Nil(t, request.Metadata)
}

// 4. TestApplyClaudeCodeDisguiseBody_NilSystem — system nil becomes array with 1 entry
func TestApplyClaudeCodeDisguiseBody_NilSystem(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		System: nil,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 1)
	assert.Equal(t, "text", arr[0].Type)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)
}

// 5. TestApplyClaudeCodeDisguiseBody_StringSystem — system string becomes 2-entry array
func TestApplyClaudeCodeDisguiseBody_StringSystem(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		System: "original system prompt",
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 2)
	assert.Equal(t, "text", arr[0].Type)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)
	assert.Equal(t, "text", arr[1].Type)
	assert.Equal(t, "original system prompt", *arr[1].Text)
}

// 6. TestApplyClaudeCodeDisguiseBody_ArraySystemHasEntry — already has entry, not duplicated
func TestApplyClaudeCodeDisguiseBody_ArraySystemHasEntry(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	existingText := claudeCodeSystemPromptEntry
	otherText := "other prompt"
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &existingText},
			{Type: "text", Text: &otherText},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 2) // NOTE: not duplicated
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)
}

// 7. TestApplyClaudeCodeDisguiseBody_MetadataEmpty — metadata empty, user_id injected
func TestApplyClaudeCodeDisguiseBody_MetadataEmpty(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	assert.True(t, len(request.Metadata) > 0)
	var meta dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta.UserId)
}

// 8. TestApplyClaudeCodeDisguiseBody_MetadataHasValidUserId — valid legacy user_id retained
func TestApplyClaudeCodeDisguiseBody_MetadataHasValidUserId(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	device := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	session := "12345678-1234-1234-1234-123456789012"
	valid := "user_" + device + "_account__session_" + session
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: valid})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	assert.Equal(t, valid, meta2.UserId) // NOTE: valid format preserved
}

// 8b. TestApplyClaudeCodeDisguiseBody_MetadataHasInvalidUserId — malformed user_id replaced
func TestApplyClaudeCodeDisguiseBody_MetadataHasInvalidUserId(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: "user123"})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	assert.NotEqual(t, "user123", meta2.UserId) // NOTE: malformed replaced
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta2.UserId)
}

// 8c. TestApplyClaudeCodeDisguiseBody_MetadataHasValidJSONUserId — JSON user_id normalized to legacy
func TestApplyClaudeCodeDisguiseBody_MetadataHasValidJSONUserId(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	device := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	session := "12345678-1234-1234-1234-123456789012"
	validJSON := `{"device_id":"` + device + `","account_uuid":"","session_id":"` + session + `"}`
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: validJSON})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	// NOTE: JSON format must be normalized to legacy format (matching UA 2.1.50)
	expectedLegacy := "user_" + device + "_account__session_" + session
	assert.Equal(t, expectedLegacy, meta2.UserId)
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account_[a-fA-F0-9-]*_session_[a-fA-F0-9-]{36}$`), meta2.UserId)
}

// 8c2. TestApplyClaudeCodeDisguiseBody_MetadataHasValidJSONUserIdWithAccount — JSON with account_uuid converted to legacy
func TestApplyClaudeCodeDisguiseBody_MetadataHasValidJSONUserIdWithAccount(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	device := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	account := "11111111-2222-3333-4444-555555555555"
	session := "12345678-1234-1234-1234-123456789012"
	validJSON := `{"device_id":"` + device + `","account_uuid":"` + account + `","session_id":"` + session + `"}`
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: validJSON})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	// NOTE: JSON format normalized to legacy, account_uuid preserved
	expectedLegacy := "user_" + device + "_account_" + account + "_session_" + session
	assert.Equal(t, expectedLegacy, meta2.UserId)
}

// 8c3. TestApplyClaudeCodeDisguiseBody_MetadataHasValidLegacyUserIdWithAccount — legacy with account preserved
func TestApplyClaudeCodeDisguiseBody_MetadataHasValidLegacyUserIdWithAccount(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	device := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	account := "11111111-2222-3333-4444-555555555555"
	session := "12345678-1234-1234-1234-123456789012"
	valid := "user_" + device + "_account_" + account + "_session_" + session
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: valid})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	// NOTE: legacy format re-formatted to same string
	assert.Equal(t, valid, meta2.UserId)
}

// 8d. TestApplyClaudeCodeDisguiseBody_MetadataHasInvalidJSONUserId — JSON missing session_id replaced
func TestApplyClaudeCodeDisguiseBody_MetadataHasInvalidJSONUserId(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	invalidJSON := `{"device_id":"abc","session_id":""}`
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: invalidJSON})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta2 dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta2)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta2.UserId)
}

// 8e. TestIsValidClaudeCodeUserID — direct validator coverage
func TestIsValidClaudeCodeUserID(t *testing.T) {
	validLegacy := "user_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef_account__session_12345678-1234-1234-1234-123456789012"
	validLegacyWithAccount := "user_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef_account_11111111-2222-3333-4444-555555555555_session_12345678-1234-1234-1234-123456789012"
	validJSON := `{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"12345678-1234-1234-1234-123456789012"}`

	assert.True(t, isValidClaudeCodeUserID(validLegacy))
	assert.True(t, isValidClaudeCodeUserID(validLegacyWithAccount))
	assert.True(t, isValidClaudeCodeUserID(validJSON))

	assert.False(t, isValidClaudeCodeUserID(""))
	assert.False(t, isValidClaudeCodeUserID("user123"))
	assert.False(t, isValidClaudeCodeUserID("user_abc_account__session_def"))
	assert.False(t, isValidClaudeCodeUserID(`{"device_id":"abc","session_id":""}`)) // missing session_id
	assert.False(t, isValidClaudeCodeUserID(`{"device_id":"abc"}`))                 // missing session_id
	assert.False(t, isValidClaudeCodeUserID(`not-json`))
}

// 8f. TestDeriveLegacyClaudeCodeUserID_Deterministic — same seed → same output (replayability)
func TestDeriveLegacyClaudeCodeUserID_Deterministic(t *testing.T) {
	seed := "user123"
	a1 := deriveLegacyClaudeCodeUserID(seed)
	a2 := deriveLegacyClaudeCodeUserID(seed)
	assert.Equal(t, a1, a2, "same seed must produce same derived user_id for replayability")
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), a1)
}

// 8g. TestDeriveLegacyClaudeCodeUserID_DifferentSeeds — different seeds → different outputs
func TestDeriveLegacyClaudeCodeUserID_DifferentSeeds(t *testing.T) {
	a := deriveLegacyClaudeCodeUserID("seed-one")
	b := deriveLegacyClaudeCodeUserID("seed-two")
	assert.NotEqual(t, a, b)
}

// 8h. TestApplyClaudeCodeDisguiseBody_MetadataInvalidReplayable — malformed user_id derivations are stable across calls
func TestApplyClaudeCodeDisguiseBody_MetadataInvalidReplayable(t *testing.T) {
	mk := func() string {
		c, _ := gin.CreateTestContext(nil)
		existing, _ := common.Marshal(dto.ClaudeMetadata{UserId: "client-uid-42"})
		req := &dto.ClaudeRequest{Metadata: existing}
		ApplyClaudeCodeDisguiseBody(c, req, makeRelayInfo(true))
		var meta dto.ClaudeMetadata
		_ = common.Unmarshal(req.Metadata, &meta)
		return meta.UserId
	}
	first := mk()
	second := mk()
	assert.Equal(t, first, second, "same source user_id must map to same derived id across calls")
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), first)
}

// 8i. TestApplyClaudeCodeDisguiseBody_OnOpenAIToClaudePath — disguise applies on the
// ConvertOpenAIRequest path used by channel tests and OpenAI-SDK callers.
func TestApplyClaudeCodeDisguiseBody_OnOpenAIToClaudePath(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := makeRelayInfo(true)
	adaptor := &Adaptor{}

	// NOTE: RequestOpenAI2ClaudeMessage uses OpenAI PromptCacheKey as the seed for
	// metadata.user_id (EnsureClaudeMetadataUserIDFromPromptCacheKey). When the
	// client provides one, the disguise derivation must be deterministic from it.
	cacheKey := "openai-client-uid-7"
	req := &dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
		PromptCacheKey: cacheKey,
	}
	converted, err := adaptor.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	claudeReq, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)

	// system 应被注入伪装 entry
	arr, ok := claudeReq.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	assert.GreaterOrEqual(t, len(arr), 1)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)

	// metadata.user_id 应被确定性派生（源自 PromptCacheKey）
	var meta dto.ClaudeMetadata
	require.NoError(t, common.Unmarshal(claudeReq.Metadata, &meta))
	expected := deriveLegacyClaudeCodeUserID(cacheKey)
	assert.Equal(t, expected, meta.UserId)
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta.UserId)

	// 重放：再跑一次应得到相同派生结果
	converted2, _ := adaptor.ConvertOpenAIRequest(c, info, req)
	cr2, _ := converted2.(*dto.ClaudeRequest)
	var meta2 dto.ClaudeMetadata
	_ = common.Unmarshal(cr2.Metadata, &meta2)
	assert.Equal(t, meta.UserId, meta2.UserId, "openai->claude path should be replayable")
}

// 8j. TestApplyClaudeCodeDisguiseBody_OnOpenAIToClaudePath_NoSeed — without PromptCacheKey,
// metadata is empty and a fresh random identity is generated each call (no replayability).
func TestApplyClaudeCodeDisguiseBody_OnOpenAIToClaudePath_NoSeed(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := makeRelayInfo(true)
	adaptor := &Adaptor{}
	req := &dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
		},
	}
	converted, err := adaptor.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	cr, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	// system injected
	arr, ok := cr.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.GreaterOrEqual(t, len(arr), 1)
	// metadata present and well-formed (random, no seed to derive from)
	require.Greater(t, len(cr.Metadata), 0)
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), protoUID(t, cr))
}

func protoUID(t *testing.T, r *dto.ClaudeRequest) string {
	var m dto.ClaudeMetadata
	require.NoError(t, common.Unmarshal(r.Metadata, &m))
	return m.UserId
}

// 9. TestGenerateLegacyClaudeCodeUserID_Format — format matches expected regex
func TestGenerateLegacyClaudeCodeUserID_Format(t *testing.T) {
	userID := generateLegacyClaudeCodeUserID()
	pattern := `^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`
	assert.Regexp(t, regexp.MustCompile(pattern), userID)
}

// 10. TestGenerateLegacyClaudeCodeUserID_Unique — two calls produce different values
func TestGenerateLegacyClaudeCodeUserID_Unique(t *testing.T) {
	id1 := generateLegacyClaudeCodeUserID()
	id2 := generateLegacyClaudeCodeUserID()
	assert.NotEqual(t, id1, id2)
}

// 8k. TestSetMetadataUserID_StripsOtherFields — disguise must expose only user_id
func TestSetMetadataUserID_StripsOtherFields(t *testing.T) {
	// NOTE: original metadata has both user_id (malformed) and a custom field.
	// When disguise is enabled, only user_id may be sent upstream — anything
	// else could leak identifying signals and expose the disguise.
	original := map[string]any{
		"user_id":     "client-uid-42",
		"custom_key":  "important-data",
		"other_field": 123,
	}
	origData, err := common.Marshal(original)
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	req := &dto.ClaudeRequest{Metadata: origData}
	ApplyClaudeCodeDisguiseBody(c, req, makeRelayInfo(true))

	// NOTE: user_id must be replaced with a valid Claude Code format
	var meta dto.ClaudeMetadata
	require.NoError(t, common.Unmarshal(req.Metadata, &meta))
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta.UserId)
	assert.NotEqual(t, "client-uid-42", meta.UserId)

	// NOTE: other metadata fields must be stripped — only user_id survives
	var rawMap map[string]any
	require.NoError(t, common.Unmarshal(req.Metadata, &rawMap))
	assert.Len(t, rawMap, 1)
	_, hasUID := rawMap["user_id"]
	assert.True(t, hasUID)
	assert.NotContains(t, rawMap, "custom_key")
	assert.NotContains(t, rawMap, "other_field")
}

// 8l. TestSetMetadataUserID_PreservesOtherFields_EmptyMetadata — no clobber when starting fresh
func TestSetMetadataUserID_PreservesOtherFields_EmptyMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := &dto.ClaudeRequest{} // empty metadata
	ApplyClaudeCodeDisguiseBody(c, req, makeRelayInfo(true))

	var rawMap map[string]any
	require.NoError(t, common.Unmarshal(req.Metadata, &rawMap))
	// NOTE: only user_id should exist
	assert.Len(t, rawMap, 1)
	_, hasUID := rawMap["user_id"]
	assert.True(t, hasUID)
}

// Extra: nil info should not panic
func TestApplyClaudeCodeDisguiseHeaders_NilInfo(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "original")
	ApplyClaudeCodeDisguiseHeaders(c, &req, makeRelayInfoNil())
	assert.Equal(t, "original", req.Get("User-Agent"))
}

func TestApplyClaudeCodeDisguiseBody_NilInfo(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{System: "hello"}
	ApplyClaudeCodeDisguiseBody(c, request, makeRelayInfoNil())
	assert.Equal(t, "hello", request.System) // unchanged
}

// 11. TestMoveUserSystemToFirstUserMessage — user system prompt moved to first user message
func TestMoveUserSystemToFirstUserMessage_StringContent(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		System: "user custom system prompt",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	// System should only contain the Claude Code disguise entry
	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 1)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)

	// First user message should contain the wrapped system prompt
	content, ok := request.Messages[0].Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "<system-reminder>")
	assert.Contains(t, content, "user custom system prompt")
	assert.Contains(t, content, "</system-reminder>")
	assert.Contains(t, content, "hello")
}

// 11b. TestMoveUserSystemToFirstUserMessage_NoMessages — no messages, system preserved
func TestMoveUserSystemToFirstUserMessage_NoMessages(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		System: "user custom system prompt",
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	// System should keep both entries since there are no messages to inject into
	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 2)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)
	assert.Equal(t, "user custom system prompt", *arr[1].Text)
}

// 11c. TestMoveUserSystemToFirstUserMessage_MultipleSystemEntries — multiple system entries merged
func TestMoveUserSystemToFirstUserMessage_MultipleSystemEntries(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	text1 := "prompt part 1"
	text2 := "prompt part 2"
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &text1},
			{Type: "text", Text: &text2},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hi"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	// System should only contain the Claude Code entry
	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, arr, 1)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)

	// First user message should contain both prompts wrapped
	content, ok := request.Messages[0].Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "prompt part 1")
	assert.Contains(t, content, "prompt part 2")
	assert.Contains(t, content, "<system-reminder>")
}

// 11d. TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_StringContent — cache_control
// on the user system entry must survive the move into the first (string-content) user message.
func TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_StringContent(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	cacheControl := json.RawMessage(`{"type":"ephemeral"}`)
	text := "user custom system prompt"
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &text, CacheControl: cacheControl},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	// String content must be converted to block form so cache_control has somewhere to live.
	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.Contains(t, *blocks[0].Text, "<system-reminder>")
	assert.Contains(t, *blocks[0].Text, "user custom system prompt")
	assert.JSONEq(t, `{"type":"ephemeral"}`, string(blocks[0].CacheControl))
	assert.Equal(t, "hello", *blocks[1].Text)
	assert.Nil(t, blocks[1].CacheControl)
}

// 11e. TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_ArrayContent — cache_control
// preserved when the target user message content is already a block array.
func TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_ArrayContent(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	cacheControl := json.RawMessage(`{"type":"ephemeral","ttl":"1h"}`)
	text := "user custom system prompt"
	existingText := "hi"
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &text, CacheControl: cacheControl},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: &existingText}}},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.JSONEq(t, `{"type":"ephemeral","ttl":"1h"}`, string(blocks[0].CacheControl))
}

// 11f. TestMoveUserSystemToFirstUserMessage_MultipleEntries_LastCacheControlWins — when several
// user system entries are merged into one block, the last cache_control marker wins, matching
// Anthropic's "cache everything up to this block" semantics.
func TestMoveUserSystemToFirstUserMessage_MultipleEntries_LastCacheControlWins(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	text1 := "prompt part 1"
	text2 := "prompt part 2"
	cc1 := json.RawMessage(`{"type":"ephemeral","ttl":"5m"}`)
	cc2 := json.RawMessage(`{"type":"ephemeral","ttl":"1h"}`)
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &text1, CacheControl: cc1},
			{Type: "text", Text: &text2, CacheControl: cc2},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hi"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 2)
	assert.JSONEq(t, `{"type":"ephemeral","ttl":"1h"}`, string(blocks[0].CacheControl))
}

// 11g. TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_NoExistingUserMessage — when no
// user message exists yet, the newly-prepended message must still carry cache_control.
func TestMoveUserSystemToFirstUserMessage_PreservesCacheControl_NoExistingUserMessage(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	cacheControl := json.RawMessage(`{"type":"ephemeral"}`)
	text := "user custom system prompt"
	request := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: "text", Text: &text, CacheControl: cacheControl},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "assistant", Content: "prior turn"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	require.Equal(t, "user", request.Messages[0].Role)
	blocks, ok := request.Messages[0].Content.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.Len(t, blocks, 1)
	assert.JSONEq(t, `{"type":"ephemeral"}`, string(blocks[0].CacheControl))
}

// 11h. TestMoveUserSystemToFirstUserMessage_NoCacheControl_KeepsStringConcat — without any
// cache_control marker, the move keeps the simpler string-concatenation form (no behavior change).
func TestMoveUserSystemToFirstUserMessage_NoCacheControl_KeepsStringConcat(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{
		System: "user custom system prompt",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	content, ok := request.Messages[0].Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "<system-reminder>")
}

// 12. TestEnsureClaudeCodeMetadataUserID_InvalidJSONComponents — JSON with short device_id derived instead of formatted
func TestEnsureClaudeCodeMetadataUserID_InvalidJSONComponents(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	// JSON format that parses but device_id is too short for legacy format
	invalidJSON := `{"device_id":"abc123","account_uuid":"","session_id":"12345678-1234-1234-1234-123456789012"}`
	existingMeta, _ := common.Marshal(dto.ClaudeMetadata{UserId: invalidJSON})
	request := &dto.ClaudeRequest{
		Metadata: existingMeta,
	}
	info := makeRelayInfo(true)

	ApplyClaudeCodeDisguiseBody(c, request, info)

	var meta dto.ClaudeMetadata
	err := common.Unmarshal(request.Metadata, &meta)
	require.NoError(t, err)
	// Should be derived (deterministic) instead of using invalid short device_id
	assert.Regexp(t, regexp.MustCompile(`^user_[a-fA-F0-9]{64}_account__session_[a-fA-F0-9-]{36}$`), meta.UserId)
	// Should be deterministic from the original JSON string
	expected := deriveLegacyClaudeCodeUserID(invalidJSON)
	assert.Equal(t, expected, meta.UserId)
}

// ========== Bitmask mode tests ==========

func ptrInt(v int) *int { return &v }

func makeRelayInfoMode(mode int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguiseMode: ptrInt(mode),
			},
		},
	}
}

// TestApplyClaudeCodeDisguiseHeaders_UAOnly — only UA set, X-App/beta untouched
func TestApplyClaudeCodeDisguiseHeaders_UAOnly(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	ApplyClaudeCodeDisguiseHeaders(c, &req, makeRelayInfoMode(dto.ClaudeDisguiseUA))

	assert.Equal(t, dto.ClaudeCodeDisguiseUserAgent, req.Get("User-Agent"))
	assert.Equal(t, "", req.Get("X-App"), "X-App must NOT be set when only UA dimension is on")
	assert.Equal(t, "", req.Get("anthropic-beta"), "anthropic-beta must NOT be set when only UA dimension is on")
}

// TestApplyClaudeCodeDisguiseHeaders_HeaderOnly — X-App/beta set, UA untouched
func TestApplyClaudeCodeDisguiseHeaders_HeaderOnly(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	ApplyClaudeCodeDisguiseHeaders(c, &req, makeRelayInfoMode(dto.ClaudeDisguiseHeader))

	assert.Equal(t, "", req.Get("User-Agent"), "User-Agent must NOT be set when only Header dimension is on")
	assert.Equal(t, dto.ClaudeCodeDisguiseXApp, req.Get("X-App"))
	assert.Equal(t, claudeCodeDefaultBeta, req.Get("anthropic-beta"))
}

// TestApplyClaudeCodeDisguiseHeaders_ModeZeroExplicit — mode=0 via pointer disables all even if old bool were true
func TestApplyClaudeCodeDisguiseHeaders_ModeZeroExplicit(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	req := http.Header{}
	req.Set("User-Agent", "original")
	// legacy bool is true, but mode pointer explicitly 0 → should be disabled
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguise:     true,
				ClaudeCodeDisguiseMode: ptrInt(0),
			},
		},
	}
	ApplyClaudeCodeDisguiseHeaders(c, &req, info)

	assert.Equal(t, "original", req.Get("User-Agent"), "disguise must be off when mode=ptr(0)")
	assert.Equal(t, "", req.Get("X-App"))
}

// TestApplyClaudeCodeDisguiseBody_SystemPromptOnly — body injected, headers unchanged
func TestApplyClaudeCodeDisguiseBody_SystemPromptOnly(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{System: nil}
	ApplyClaudeCodeDisguiseBody(c, request, makeRelayInfoMode(dto.ClaudeDisguiseSystemPrompt))

	arr, ok := request.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok, "system must be injected when SystemPrompt dimension is on")
	require.Len(t, arr, 1)
	assert.Equal(t, claudeCodeSystemPromptEntry, *arr[0].Text)
}

// TestApplyClaudeCodeDisguiseBody_UAOnly_NoBodyChange — body untouched when only UA dimension
func TestApplyClaudeCodeDisguiseBody_UAOnly_NoBodyChange(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{System: "keep me"}
	ApplyClaudeCodeDisguiseBody(c, request, makeRelayInfoMode(dto.ClaudeDisguiseUA))

	assert.Equal(t, "keep me", request.System, "body must NOT be modified when SystemPrompt dimension is off")
	assert.Nil(t, request.Metadata)
}

// TestApplyClaudeCodeDisguiseBody_ModeZeroExplicit_NoBodyChange
func TestApplyClaudeCodeDisguiseBody_ModeZeroExplicit_NoBodyChange(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	request := &dto.ClaudeRequest{System: "original"}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguise:     true, // legacy true
				ClaudeCodeDisguiseMode: ptrInt(0),
			},
		},
	}
	ApplyClaudeCodeDisguiseBody(c, request, info)

	assert.Equal(t, "original", request.System, "body must NOT be modified when mode=ptr(0) overrides legacy true")
}
