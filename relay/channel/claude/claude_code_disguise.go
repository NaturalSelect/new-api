package claude

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	claudeCodeUserAgent         = "claude-cli/2.1.50"
	claudeCodeXApp              = "claude-code"
	claudeCodeDefaultBeta       = "claude-code-20250219"
	claudeCodeSystemPromptEntry = "You are Claude Code, Anthropic's official CLI for Claude."
)

// claudeCodeLegacyUserIDRe matches the legacy metadata.user_id format:
//
//	user_{64hex}_account_{optional-uuid}_session_{36uuid}
//
// Mirrors sub2api ParseMetadataUserID legacy branch.
var claudeCodeLegacyUserIDRe = regexp.MustCompile(`^user_([a-fA-F0-9]{64})_account_([a-fA-F0-9-]*)_session_([a-fA-F0-9-]{36})$`)

// claudeCodeJSONUserID mirrors the new (CLI >= 2.1.78) JSON metadata format.
type claudeCodeJSONUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

// isValidClaudeCodeUserID reports whether userID matches either the legacy
// concatenated format or the new JSON format expected by Claude Code CLI.
func isValidClaudeCodeUserID(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	// New JSON format:{"device_id":"...","account_uuid":"...","session_id":"..."}
	if userID[0] == '{' {
		var j claudeCodeJSONUserID
		if err := common.UnmarshalJsonStr(userID, &j); err != nil {
			return false
		}
		return j.DeviceID != "" && j.SessionID != ""
	}
	// Legacy concatenated format.
	return claudeCodeLegacyUserIDRe.MatchString(userID)
}

// ApplyClaudeCodeDisguiseHeaders injects Claude Code CLI headers when the channel setting is enabled.
func ApplyClaudeCodeDisguiseHeaders(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelOtherSettings.ClaudeCodeDisguise == false {
		return
	}
	req.Set("User-Agent", claudeCodeUserAgent)
	req.Set("X-App", claudeCodeXApp)
	if req.Get("anthropic-beta") == "" {
		req.Set("anthropic-beta", claudeCodeDefaultBeta)
	}
}

// ApplyClaudeCodeDisguiseBody injects Claude Code CLI body fields when the channel setting is enabled.
func ApplyClaudeCodeDisguiseBody(c *gin.Context, request *dto.ClaudeRequest, info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelOtherSettings.ClaudeCodeDisguise == false {
		return
	}
	injectClaudeCodeSystem(request)
	ensureClaudeCodeMetadataUserID(request)
}

// injectClaudeCodeSystem prepends the Claude Code system prompt entry to request.System.
// If the entry already exists, it is not duplicated.
func injectClaudeCodeSystem(request *dto.ClaudeRequest) {
	makeEntry := func() dto.ClaudeMediaMessage {
		text := claudeCodeSystemPromptEntry
		return dto.ClaudeMediaMessage{
			Type: "text",
			Text: &text,
		}
	}

	switch sys := request.System.(type) {
	case nil:
		// NOTE: system is nil — set to single-entry array
		request.System = []dto.ClaudeMediaMessage{makeEntry()}
	case string:
		// NOTE: system is a plain string — convert to array with disguise entry first
		original := sys
		request.System = []dto.ClaudeMediaMessage{
			makeEntry(),
			{Type: "text", Text: &original},
		}
	case []dto.ClaudeMediaMessage:
		// NOTE: already typed — check for existing entry and prepend if absent
		for _, entry := range sys {
			if entry.Text != nil && *entry.Text == claudeCodeSystemPromptEntry {
				return
			}
		}
		request.System = append([]dto.ClaudeMediaMessage{makeEntry()}, sys...)
	case []any:
		// NOTE: untyped array from JSON passthrough — convert to typed, then process
		data, err := common.Marshal(sys)
		if err != nil {
			return
		}
		var typed []dto.ClaudeMediaMessage
		if err := common.Unmarshal(data, &typed); err != nil {
			// NOTE: conversion failed — conservatively ignore
			return
		}
		for _, entry := range typed {
			if entry.Text != nil && *entry.Text == claudeCodeSystemPromptEntry {
				return
			}
		}
		request.System = append([]dto.ClaudeMediaMessage{makeEntry()}, typed...)
	default:
		// NOTE: unknown type — try round-trip conversion via common.Marshal/Unmarshal
		data, err := common.Marshal(sys)
		if err != nil {
			return
		}
		var typed []dto.ClaudeMediaMessage
		if err := common.Unmarshal(data, &typed); err != nil {
			return
		}
		for _, entry := range typed {
			if entry.Text != nil && *entry.Text == claudeCodeSystemPromptEntry {
				return
			}
		}
		request.System = append([]dto.ClaudeMediaMessage{makeEntry()}, typed...)
	}
}

// ensureClaudeCodeMetadataUserID overwrites metadata.user_id with a validly
// formatted Claude Code identifier unless the existing value already conforms
// to either the legacy concatenated format or the new JSON format.
// A non-empty but malformed user_id (e.g. from a plain OpenAI client) would
// fail upstream Claude Code identity checks, so it must be replaced.
//
// Replayability: when an existing (malformed) user_id must be rewritten, it is
// used as a deterministic seed so the same input always maps to the same
// derived identifier across requests. Only when there is no seed at all
// (metadata absent/empty) a fresh random identifier is generated.
// setMetadataUserID overwrites only the user_id key in request.Metadata,
// preserving any other metadata fields that may already exist.
func setMetadataUserID(request *dto.ClaudeRequest, userID string) {
	metadataMap := make(map[string]any)
	if len(request.Metadata) > 0 {
		if err := common.Unmarshal(request.Metadata, &metadataMap); err != nil {
			// NOTE: unparseable metadata — start fresh with only user_id
			metadataMap = make(map[string]any)
		}
	}
	metadataMap["user_id"] = userID
	data, err := common.Marshal(metadataMap)
	if err != nil {
		return
	}
	request.Metadata = data
}

func ensureClaudeCodeMetadataUserID(request *dto.ClaudeRequest) {
	if len(request.Metadata) > 0 {
		var meta dto.ClaudeMetadata
		if err := common.Unmarshal(request.Metadata, &meta); err == nil && isValidClaudeCodeUserID(meta.UserId) {
			return
		}
		// NOTE: existing user_id present but malformed — derive deterministically from it
		if err := common.Unmarshal(request.Metadata, &meta); err == nil && meta.UserId != "" {
			derived := deriveLegacyClaudeCodeUserID(meta.UserId)
			setMetadataUserID(request, derived)
			return
		}
	}
	// NOTE: no metadata/no usable seed — fall back to a fresh random identifier
	userID := generateLegacyClaudeCodeUserID()
	setMetadataUserID(request, userID)
}

// generateLegacyClaudeCodeUserID produces a fresh random legacy-format Claude
// Code user identifier:user_{64hex}_account__session_{36uuid}
// Used when there is no seed to derive from. Not replayable.
func generateLegacyClaudeCodeUserID() string {
	deviceBytes := make([]byte, 32)
	_, _ = rand.Read(deviceBytes)
	deviceID := hex.EncodeToString(deviceBytes)
	sessionID := uuid.New().String()
	return "user_" + deviceID + "_account__session_" + sessionID
}

// deriveLegacyClaudeCodeUserID deterministically produces a legacy-format
// Claude Code user identifier from an arbitrary seed string. The same seed
// always yields the same output, so requests carrying the same (malformed)
// original user_id map to a stable derived identity across replays.
//
// Construction: SHA-512(seed) → first 32 bytes → 64-char hex device_id;
// next 16 bytes reshaped into a v4 UUID (version/variant bits set per RFC 4122)
// → session_id. account_uuid stays empty (legacy form accepts it).
func deriveLegacyClaudeCodeUserID(seed string) string {
	h := sha512.Sum512([]byte(seed))
	deviceID := hex.EncodeToString(h[:32]) // 64 hex chars

	var sessBytes [16]byte
	copy(sessBytes[:], h[32:48])
	// RFC 4122 v4 variant and version bits
	sessBytes[6] = (sessBytes[6] & 0x0f) | 0x40
	sessBytes[8] = (sessBytes[8] & 0x3f) | 0x80
	sessionID := uuid.Must(uuid.FromBytes(sessBytes[:])).String()
	return "user_" + deviceID + "_account__session_" + sessionID
}
