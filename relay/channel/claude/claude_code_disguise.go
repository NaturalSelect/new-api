package claude

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
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

// parseClaudeCodeUserID parses a metadata.user_id string in either legacy or JSON
// format and returns the extracted components. Returns ok=false if parsing fails.
func parseClaudeCodeUserID(userID string) (deviceID, accountUUID, sessionID string, ok bool) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", "", "", false
	}
	// JSON format
	if userID[0] == '{' {
		var j claudeCodeJSONUserID
		if err := common.UnmarshalJsonStr(userID, &j); err != nil {
			return "", "", "", false
		}
		if j.DeviceID == "" || j.SessionID == "" {
			return "", "", "", false
		}
		return j.DeviceID, j.AccountUUID, j.SessionID, true
	}
	// Legacy format
	matches := claudeCodeLegacyUserIDRe.FindStringSubmatch(userID)
	if matches == nil {
		return "", "", "", false
	}
	return matches[1], matches[2], matches[3], true
}

// formatLegacyClaudeCodeUserID builds a legacy-format metadata.user_id from components.
// Format: user_{deviceID}_account_{accountUUID}_session_{sessionID}
func formatLegacyClaudeCodeUserID(deviceID, accountUUID, sessionID string) string {
	return "user_" + deviceID + "_account_" + accountUUID + "_session_" + sessionID
}

// ApplyClaudeCodeDisguiseHeaders injects Claude Code CLI headers according to the
// channel's disguise mode bitmask (dto.ClaudeDisguiseUA / ClaudeDisguiseHeader).
// The UA and the remaining headers (X-App, anthropic-beta) are independently
// controlled so callers can enable e.g. only the UA dimension.
func ApplyClaudeCodeDisguiseHeaders(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	mode := info.ChannelOtherSettings.EffectiveClaudeCodeDisguiseMode()
	if mode&dto.ClaudeDisguiseUA != 0 {
		req.Set("User-Agent", dto.ClaudeCodeDisguiseUserAgent)
	}
	if mode&dto.ClaudeDisguiseHeader != 0 {
		req.Set("X-App", dto.ClaudeCodeDisguiseXApp)
		if req.Get("anthropic-beta") == "" {
			req.Set("anthropic-beta", claudeCodeDefaultBeta)
		}
	}
}

// ApplyClaudeCodeDisguiseBody injects Claude Code CLI body fields when the channel's
// disguise mode bitmask includes dto.ClaudeDisguiseSystemPrompt. It also moves user
// system prompt entries into the first user message wrapped in <system-reminder>
// tags, so that only the Claude Code disguise system prompt remains in the system
// field, and normalizes metadata.user_id — all three steps are bundled under the
// System Prompt dimension since they only make sense together.
func ApplyClaudeCodeDisguiseBody(c *gin.Context, request *dto.ClaudeRequest, info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	mode := info.ChannelOtherSettings.EffectiveClaudeCodeDisguiseMode()
	if mode&dto.ClaudeDisguiseSystemPrompt == 0 {
		return
	}
	injectClaudeCodeSystem(request)
	moveUserSystemToFirstUserMessage(request)
	ensureClaudeCodeMetadataUserID(request)
}

// moveUserSystemToFirstUserMessage extracts user system prompt entries from request.System,
// wraps them in <system-reminder> tags, and prepends to the first user message.
// The Claude Code disguise entry is kept in request.System.
// If there are no messages to inject into, user system entries remain in System unchanged.
// If any moved entry carried a cache_control marker, it is preserved on the merged block
// so prompt caching is not silently broken by the move.
func moveUserSystemToFirstUserMessage(request *dto.ClaudeRequest) {
	if len(request.Messages) == 0 {
		return
	}

	systemEntries := getTypedSystemEntries(request)
	if len(systemEntries) == 0 {
		return
	}

	// Separate Claude Code entry from user entries
	var claudeCodeEntries []dto.ClaudeMediaMessage
	var userSystemTexts []string
	var mergedCacheControl json.RawMessage
	for _, entry := range systemEntries {
		if entry.Text != nil && *entry.Text == claudeCodeSystemPromptEntry {
			claudeCodeEntries = append(claudeCodeEntries, entry)
		} else if entry.Text != nil && *entry.Text != "" {
			userSystemTexts = append(userSystemTexts, *entry.Text)
			// NOTE: cache_control marks "cache everything up to and including this
			// block". All user system entries are merged into a single block below,
			// so the last entry carrying cache_control defines the boundary for the
			// merged content and takes precedence over earlier ones.
			if len(entry.CacheControl) > 0 {
				mergedCacheControl = entry.CacheControl
			}
		}
	}

	if len(userSystemTexts) == 0 {
		return
	}

	// Build <system-reminder> wrapped content
	wrappedContent := "<system-reminder>\n" + strings.Join(userSystemTexts, "\n") + "\n</system-reminder>"

	// Inject into the first user message, or prepend a new user message
	injected := false
	for i, msg := range request.Messages {
		if msg.Role != "user" {
			continue
		}

		switch content := msg.Content.(type) {
		case string:
			if mergedCacheControl != nil {
				// NOTE: cache_control can only be set on a content block, not on a
				// plain string message — convert to block form to preserve it.
				request.Messages[i].Content = []dto.ClaudeMediaMessage{
					{Type: "text", Text: common.GetPointer(wrappedContent), CacheControl: mergedCacheControl},
					{Type: "text", Text: common.GetPointer(content)},
				}
			} else {
				request.Messages[i].Content = wrappedContent + "\n" + content
			}
		case []dto.ClaudeMediaMessage:
			newEntry := dto.ClaudeMediaMessage{
				Type:         "text",
				Text:         common.GetPointer(wrappedContent),
				CacheControl: mergedCacheControl,
			}
			request.Messages[i].Content = append([]dto.ClaudeMediaMessage{newEntry}, content...)
		default:
			// Try round-trip conversion for untyped arrays
			data, err := common.Marshal(content)
			if err != nil {
				continue
			}
			var typed []dto.ClaudeMediaMessage
			if err := common.Unmarshal(data, &typed); err != nil {
				continue
			}
			newEntry := dto.ClaudeMediaMessage{
				Type:         "text",
				Text:         common.GetPointer(wrappedContent),
				CacheControl: mergedCacheControl,
			}
			request.Messages[i].Content = append([]dto.ClaudeMediaMessage{newEntry}, typed...)
		}
		injected = true
		break
	}

	if !injected {
		// No user message found — prepend one
		var newContent any = wrappedContent
		if mergedCacheControl != nil {
			// NOTE: same block-form conversion as above to preserve cache_control.
			newContent = []dto.ClaudeMediaMessage{
				{Type: "text", Text: common.GetPointer(wrappedContent), CacheControl: mergedCacheControl},
			}
		}
		newMessage := dto.ClaudeMessage{
			Role:    "user",
			Content: newContent,
		}
		request.Messages = append([]dto.ClaudeMessage{newMessage}, request.Messages...)
	}

	// Only remove user entries from System after successful injection
	if len(claudeCodeEntries) > 0 {
		request.System = claudeCodeEntries
	} else {
		request.System = nil
	}
}

// getTypedSystemEntries converts request.System (any type) to typed []ClaudeMediaMessage.
func getTypedSystemEntries(request *dto.ClaudeRequest) []dto.ClaudeMediaMessage {
	switch sys := request.System.(type) {
	case nil:
		return nil
	case string:
		if sys == "" {
			return nil
		}
		return []dto.ClaudeMediaMessage{
			{Type: "text", Text: common.GetPointer(sys)},
		}
	case []dto.ClaudeMediaMessage:
		return sys
	case []any:
		data, err := common.Marshal(sys)
		if err != nil {
			return nil
		}
		var typed []dto.ClaudeMediaMessage
		if err := common.Unmarshal(data, &typed); err != nil {
			return nil
		}
		return typed
	default:
		data, err := common.Marshal(sys)
		if err != nil {
			return nil
		}
		var typed []dto.ClaudeMediaMessage
		if err := common.Unmarshal(data, &typed); err != nil {
			return nil
		}
		return typed
	}
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
//
// setMetadataUserID rebuilds request.Metadata with ONLY user_id, discarding
// any other metadata fields. When Claude Code disguise is enabled, the request
// must look exactly like a genuine claude-cli request, which carries only
// user_id in metadata — keeping extra fields could leak identifying signals
// (e.g. proxy-specific keys) and expose the disguise.
func setMetadataUserID(request *dto.ClaudeRequest, userID string) {
	data, err := common.Marshal(dto.ClaudeMetadata{UserId: userID})
	if err != nil {
		return
	}
	request.Metadata = data
}

// ensureClaudeCodeMetadataUserID overwrites metadata.user_id with a validly
// formatted Claude Code identifier in the legacy concatenated format, which
// matches our disguise UA version (claude-cli/2.1.50, < 2.1.78).
//
// If an existing user_id can be parsed (either legacy or JSON format), its
// components are extracted and re-formatted in legacy format. This normalizes
// JSON-format user_ids to legacy format to avoid a JSON-user_id + legacy-UA
// mismatch that upstream could detect.
//
// A non-empty but malformed user_id (e.g. from a plain OpenAI client) would
// fail upstream Claude Code identity checks, so it is replaced with a
// deterministically derived legacy identifier.
//
// Replayability: when an existing (malformed) user_id must be rewritten, it is
// used as a deterministic seed so the same input always maps to the same
// derived identifier across requests. When the existing user_id is parseable,
// re-formatting is also deterministic. Only when there is no seed at all
// (metadata absent/empty) a fresh random identifier is generated.
func ensureClaudeCodeMetadataUserID(request *dto.ClaudeRequest) {
	if len(request.Metadata) > 0 {
		var meta dto.ClaudeMetadata
		if err := common.Unmarshal(request.Metadata, &meta); err == nil && meta.UserId != "" {
			// NOTE: existing user_id present — try to parse and re-format in legacy
			if deviceID, accountUUID, sessionID, ok := parseClaudeCodeUserID(meta.UserId); ok {
				// Valid format — normalize to legacy (matching our UA version 2.1.50)
				legacyID := formatLegacyClaudeCodeUserID(deviceID, accountUUID, sessionID)
				// Validate the formatted result — JSON components may not produce a valid
				// legacy string (e.g. device_id shorter than 64 hex chars). If invalid,
				// fall through to deterministic derivation instead of emitting a malformed ID.
				if claudeCodeLegacyUserIDRe.MatchString(legacyID) {
					setMetadataUserID(request, legacyID)
					return
				}
			}
			// Malformed or invalid after conversion — derive deterministically from the raw value
			setMetadataUserID(request, deriveLegacyClaudeCodeUserID(meta.UserId))
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
