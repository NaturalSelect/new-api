package middleware

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
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

func newTypeSafeModelContext(t *testing.T, path, contentType string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	return c
}

func TestGetModelRequestTypeSafe(t *testing.T) {
	const jsonBody = `{"state":{"z":1,"a":2},"model":"jev-latest","questions":{"q":{"type":"noul"}}}`

	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	require.NoError(t, writer.WriteField("request", jsonBody))
	file, err := writer.CreateFormFile("img", "a.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("PNG"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	for _, path := range []string{"/v1/systemone", "/typesafe/v1/systemone"} {
		t.Run("json "+path, func(t *testing.T) {
			c := newTypeSafeModelContext(t, path, "application/json", []byte(jsonBody))

			modelRequest, shouldSelect, err := getModelRequest(c)
			require.NoError(t, err)
			require.True(t, shouldSelect)
			require.Equal(t, "jev-latest", modelRequest.Model)
			require.Equal(t, relayconstant.RelayModeTypeSafeSystemOne, c.GetInt("relay_mode"))
		})
		t.Run("multipart "+path, func(t *testing.T) {
			c := newTypeSafeModelContext(t, path, writer.FormDataContentType(), multipartBody.Bytes())

			modelRequest, shouldSelect, err := getModelRequest(c)
			require.NoError(t, err)
			require.True(t, shouldSelect)
			require.Equal(t, "jev-latest", modelRequest.Model)
			require.Equal(t, relayconstant.RelayModeTypeSafeSystemOne, c.GetInt("relay_mode"))
		})
	}

	t.Run("multipart without a request field", func(t *testing.T) {
		var empty bytes.Buffer
		w := multipart.NewWriter(&empty)
		require.NoError(t, w.WriteField("other", "x"))
		require.NoError(t, w.Close())
		c := newTypeSafeModelContext(t, "/v1/systemone", w.FormDataContentType(), empty.Bytes())

		_, _, err := getModelRequest(c)
		require.Error(t, err)
	})

	t.Run("model must be a string", func(t *testing.T) {
		c := newTypeSafeModelContext(t, "/v1/systemone", "application/json", []byte(`{"model":7}`))

		_, _, err := getModelRequest(c)
		require.ErrorContains(t, err, "model")
	})
}

func TestGetModelRequestGeminiDecision(t *testing.T) {
	const body = `{"model":"gemini-decision-1","input":"hi","questions":{"q":{"type":"noul","instructions":"ok?"}}}`

	c := newTypeSafeModelContext(t, "/v1beta/decisions", "application/json", []byte(body))

	modelRequest, shouldSelect, err := getModelRequest(c)
	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "gemini-decision-1", modelRequest.Model)
}
