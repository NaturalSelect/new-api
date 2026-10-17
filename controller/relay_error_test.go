package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRelayErrorTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder
}

func newUpstreamServerError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("upstream exploded"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
}

func TestShouldRetry_OnlyKeepAliveWrittenStaysRetryable(t *testing.T) {
	c, _ := newRelayErrorTestContext()
	require.NoError(t, helper.PingData(c))
	require.NoError(t, helper.PingData(c))

	assert.True(t, shouldRetry(c, newUpstreamServerError(), 3))
}

func TestShouldRetry_PayloadWrittenIsNotRetryable(t *testing.T) {
	c, _ := newRelayErrorTestContext()
	require.NoError(t, helper.PingData(c))
	require.NoError(t, helper.StringData(c, "partial"))

	assert.False(t, shouldRetry(c, newUpstreamServerError(), 3))
}

func TestRenderRelayError_NotCommittedReturnsRealStatusAsJSON(t *testing.T) {
	c, recorder := newRelayErrorTestContext()
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	apiErr := types.NewOpenAIError(errors.New("slow down"), types.ErrorCodeBadResponseBody, http.StatusTooManyRequests)

	renderRelayError(c, types.RelayFormatOpenAI, nil, apiErr)

	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, recorder.Body.String(), "slow down")
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyRelayFailed))
}

func TestRenderRelayError_CommittedSSEGetsInStreamErrorEvent(t *testing.T) {
	tests := []struct {
		name     string
		format   types.RelayFormat
		expected []string
	}{
		{"openai", types.RelayFormatOpenAI, []string{`data: {"error"`, "upstream exploded", "data: [DONE]"}},
		{"claude", types.RelayFormatClaude, []string{"event: error", "upstream exploded"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newRelayErrorTestContext()
			helper.SetEventStreamHeaders(c)
			require.NoError(t, helper.PingData(c))

			renderRelayError(c, tt.format, nil, newUpstreamServerError())

			assert.Equal(t, http.StatusOK, recorder.Code)
			body := recorder.Body.String()
			for _, want := range tt.expected {
				assert.Contains(t, body, want)
			}
			assert.False(t, strings.Contains(body, `{"error":{"message"`) && !strings.Contains(body, "data: "),
				"the error must be framed as an SSE event, not appended as bare JSON")
			assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyRelayFailed))
		})
	}
}

func TestRenderRelayError_CommittedNonSSEAppendsNothing(t *testing.T) {
	c, recorder := newRelayErrorTestContext()
	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := c.Writer.Write([]byte(`{"partial":true}`))
	require.NoError(t, err)

	renderRelayError(c, types.RelayFormatOpenAI, nil, newUpstreamServerError())

	assert.Equal(t, `{"partial":true}`, recorder.Body.String())
	assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyRelayFailed))
}
