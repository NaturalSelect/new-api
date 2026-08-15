package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestIsEasterEggEligiblePath(t *testing.T) {
	eligible := []string{
		"/v1/chat/completions",
		"/pg/chat/completions",
		"/v1/messages",
		"/v1/responses",
		"/v1beta/models/nailong:generateContent",
		"/v1beta/models/nailong:streamGenerateContent",
		"/v1/models/nailong:generateContent",
	}
	for _, path := range eligible {
		t.Run("eligible "+path, func(t *testing.T) {
			require.True(t, isEasterEggEligiblePath(path))
		})
	}

	ineligible := []string{
		"/v1/completions",
		"/v1/moderations",
		"/v1/responses/compact",
		"/v1/embeddings",
		"/v1/images/generations",
		"/v1/images/edits",
		"/v1/audio/speech",
		"/v1/audio/transcriptions",
		"/v1/realtime",
		"/v1beta/models/nailong:countTokens",
		"/v1beta/models/nailong:embedContent",
		"/mj/submit/imagine",
		"/suno/submit/music",
	}
	for _, path := range ineligible {
		t.Run("ineligible "+path, func(t *testing.T) {
			require.False(t, isEasterEggEligiblePath(path))
		})
	}
}

func TestIsEasterEggRequest(t *testing.T) {
	orig := operation_setting.GetEasterEggSetting()
	origVal := *orig
	t.Cleanup(func() { *orig = origVal })

	*orig = operation_setting.EasterEggSetting{Enabled: true, ModelName: "nailong"}
	require.True(t, isEasterEggRequest("/v1/chat/completions", "nailong"))
	require.False(t, isEasterEggRequest("/v1/embeddings", "nailong"), "non chat-like endpoints must not be hijacked")
	require.False(t, isEasterEggRequest("/v1/chat/completions", "gpt-4o"), "unrelated model name must not match")

	*orig = operation_setting.EasterEggSetting{Enabled: false, ModelName: "nailong"}
	require.False(t, isEasterEggRequest("/v1/chat/completions", "nailong"), "disabled setting must never match")
}
