package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}

// ============================================================================
// applyDisguiseHeaderProtection tests
// ============================================================================

func ptrMode(v int) *int { return &v }

func makeInfoWithMode(mode *int, codex bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguiseMode: mode,
				CodexDisguise:          codex,
			},
		},
	}
}

// TestApplyDisguiseHeaderProtection_ClaudeUAProtected — after an override sets
// a client UA, the protection re-asserts the disguise UA.
func TestApplyDisguiseHeaderProtection_ClaudeUAProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hdr := http.Header{}
	hdr.Set("User-Agent", "client-original")

	info := makeInfoWithMode(ptrMode(dto.ClaudeDisguiseFull), false)
	applyDisguiseHeaderProtection(hdr, info)

	require.Equal(t, dto.ClaudeCodeDisguiseUserAgent, hdr.Get("User-Agent"),
		"disguise UA must win over client UA when UA dimension is enabled")
	require.Equal(t, dto.ClaudeCodeDisguiseXApp, hdr.Get("X-App"),
		"X-App must be re-asserted when Header dimension is enabled")
}

// TestApplyDisguiseHeaderProtection_UADimOff_OverrideWins — when UA dimension
// is off, pass-through client UA is preserved.
func TestApplyDisguiseHeaderProtection_UADimOff_OverrideWins(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("User-Agent", "client-ua")

	// Header+SystemPrompt, but NOT UA
	info := makeInfoWithMode(ptrMode(dto.ClaudeDisguiseHeader|dto.ClaudeDisguiseSystemPrompt), false)
	applyDisguiseHeaderProtection(hdr, info)

	require.Equal(t, "client-ua", hdr.Get("User-Agent"),
		"client UA must be preserved when UA dimension is off")
}

// TestApplyDisguiseHeaderProtection_ExplicitZeroMode — mode=ptr(0) means user
// disabled all disguise; the legacy bool being true must NOT trigger protection.
func TestApplyDisguiseHeaderProtection_ExplicitZeroMode(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("User-Agent", "client-ua")

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ClaudeCodeDisguise:     true, // legacy true
				ClaudeCodeDisguiseMode: ptrMode(0),
			},
		},
	}
	applyDisguiseHeaderProtection(hdr, info)

	require.Equal(t, "client-ua", hdr.Get("User-Agent"),
		"protection must be a no-op when mode=ptr(0) explicitly disables disguise")
}

// TestApplyDisguiseHeaderProtection_CodexDisguise — Codex UA is re-asserted.
func TestApplyDisguiseHeaderProtection_CodexDisguise(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("User-Agent", "client-ua")

	info := makeInfoWithMode(nil, true /* codex */)
	applyDisguiseHeaderProtection(hdr, info)

	require.Equal(t, dto.CodexDisguiseUserAgent, hdr.Get("User-Agent"),
		"Codex disguise UA must be re-asserted")
}

// TestApplyDisguiseHeaderProtection_NilInfo — nil info is a no-op.
func TestApplyDisguiseHeaderProtection_NilInfo(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("User-Agent", "keep-me")
	applyDisguiseHeaderProtection(hdr, nil)
	require.Equal(t, "keep-me", hdr.Get("User-Agent"))
}

// TestApplyDisguiseHeaderProtection_NilHeader — nil header is a no-op (no panic).
func TestApplyDisguiseHeaderProtection_NilHeader(t *testing.T) {
	info := makeInfoWithMode(ptrMode(dto.ClaudeDisguiseFull), false)
	require.NotPanics(t, func() {
		applyDisguiseHeaderProtection(nil, info)
	})
}
