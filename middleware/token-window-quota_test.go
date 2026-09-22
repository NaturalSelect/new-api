package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// performTokenWindowQuotaRequest wires up a minimal chain that mimics TokenAuth() having
// already populated token_id and the per-window quota limits in context, then runs
// TokenWindowQuotaLimit() in front of a trivial 200 handler.
func performTokenWindowQuotaRequest(t *testing.T, tokenId, limit5h, limit7d int) *httptest.ResponseRecorder {
	t.Helper()

	common.RedisEnabled = false // exercise the in-memory window store, no Redis in tests

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/v1/test", func(c *gin.Context) {
		c.Set("token_id", tokenId)
		common.SetContextKey(c, constant.ContextKeyTokenQuotaLimit5h, limit5h)
		common.SetContextKey(c, constant.ContextKeyTokenQuotaLimit7d, limit7d)
	}, TokenWindowQuotaLimit(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestTokenWindowQuotaLimit_NoLimitConfigured_NoHeaders(t *testing.T) {
	recorder := performTokenWindowQuotaRequest(t, 900001, 0, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-5h-Used"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-5h-Limit"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-7d-Used"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-7d-Limit"))
}

func TestTokenWindowQuotaLimit_OnlyFiveHourLimit_SetsOnlyThatHeaderPair(t *testing.T) {
	recorder := performTokenWindowQuotaRequest(t, 900002, 100, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "0", recorder.Header().Get("X-New-Api-Quota-5h-Used"))
	require.Equal(t, "100", recorder.Header().Get("X-New-Api-Quota-5h-Limit"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-7d-Used"))
	require.Empty(t, recorder.Header().Get("X-New-Api-Quota-7d-Limit"))
}

func TestTokenWindowQuotaLimit_BothLimitsConfigured_SetsBothHeaderPairs(t *testing.T) {
	recorder := performTokenWindowQuotaRequest(t, 900003, 100, 1000)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "0", recorder.Header().Get("X-New-Api-Quota-5h-Used"))
	require.Equal(t, "100", recorder.Header().Get("X-New-Api-Quota-5h-Limit"))
	require.Equal(t, "0", recorder.Header().Get("X-New-Api-Quota-7d-Used"))
	require.Equal(t, "1000", recorder.Header().Get("X-New-Api-Quota-7d-Limit"))
}
