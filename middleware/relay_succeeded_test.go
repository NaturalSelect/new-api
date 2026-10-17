package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRelaySucceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		return c
	}

	t.Run("committed 200 without failure mark succeeds", func(t *testing.T) {
		assert.True(t, relaySucceeded(newContext()))
	})

	t.Run("committed 200 marked failed does not count as success", func(t *testing.T) {
		c := newContext()
		common.SetContextKey(c, constant.ContextKeyRelayFailed, true)
		assert.False(t, relaySucceeded(c))
	})

	t.Run("error status does not count as success", func(t *testing.T) {
		c := newContext()
		c.Status(http.StatusTooManyRequests)
		assert.False(t, relaySucceeded(c))
	})
}
