package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// artMarker 取字符画首行，用于在 SSE 分片体中定位内容，避免测试硬编码整幅画面
var artMarker = strings.SplitN(nailongASCIIArt, "\n", 2)[0]

func newEasterEggTestContext(t *testing.T, format types.RelayFormat, isStream bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     format,
		IsStream:        isStream,
		OriginModelName: "nailong",
	}
	if format == types.RelayFormatClaude {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}
	return c, w, info
}

func TestEasterEggHelper_NonStream(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatOpenAI, false)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)
		require.Equal(t, http.StatusOK, w.Code)

		var resp dto.OpenAITextResponse
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Choices, 1)
		require.Equal(t, nailongASCIIArt, resp.Choices[0].Message.StringContent())
		require.Equal(t, "stop", resp.Choices[0].FinishReason)
		require.Equal(t, 0, resp.Usage.TotalTokens)
	})

	t.Run("claude", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatClaude, false)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		var resp dto.ClaudeResponse
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Content, 1)
		require.Equal(t, nailongASCIIArt, resp.Content[0].GetText())
	})

	t.Run("gemini", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatGemini, false)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		var resp dto.GeminiChatResponse
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Candidates, 1)
		require.Len(t, resp.Candidates[0].Content.Parts, 1)
		require.Equal(t, nailongASCIIArt, resp.Candidates[0].Content.Parts[0].Text)
	})

	t.Run("responses", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatOpenAIResponses, false)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		var resp dto.OpenAIResponsesResponse
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Output, 1)
		require.Len(t, resp.Output[0].Content, 1)
		require.Equal(t, nailongASCIIArt, resp.Output[0].Content[0].Text)
	})
}

func TestEasterEggHelper_Stream(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatOpenAI, true)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		body := w.Body.String()
		require.Contains(t, body, artMarker)
		require.Contains(t, body, "data: [DONE]")
	})

	t.Run("claude", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatClaude, true)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		body := w.Body.String()
		require.Contains(t, body, "message_start")
		require.Contains(t, body, artMarker)
		require.Contains(t, body, "message_delta")
		require.Contains(t, body, "message_stop")
		require.True(t, info.ClaudeConvertInfo.Done, "stream must be marked done so retries/cleanup treat it as finished")
	})

	t.Run("gemini", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatGemini, true)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		body := w.Body.String()
		require.Contains(t, body, artMarker)
		require.Contains(t, body, `"finishReason":"STOP"`)
		require.NotContains(t, body, "[DONE]", "gemini native stream has no DONE sentinel")
	})

	t.Run("responses", func(t *testing.T) {
		c, w, info := newEasterEggTestContext(t, types.RelayFormatOpenAIResponses, true)
		apiErr := EasterEggHelper(c, info)
		require.Nil(t, apiErr)

		body := w.Body.String()
		require.Contains(t, body, "response.created")
		require.Contains(t, body, artMarker)
		require.Contains(t, body, "response.completed")
	})
}

func TestEasterEggHelper_UnsupportedFormat(t *testing.T) {
	c, _, info := newEasterEggTestContext(t, types.RelayFormatEmbedding, false)
	apiErr := EasterEggHelper(c, info)
	require.NotNil(t, apiErr)
}
