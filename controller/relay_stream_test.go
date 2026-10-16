package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayStreamErrorAfterKeepAlive(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			stop := helper.StartStreamKeepAlive(c, time.Hour)
			stop()
			apiError := types.NewErrorWithStatusCode(errors.New("upstream rejected input"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
			writeRelayStreamError(c, format, apiError)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			require.Contains(t, recorder.Body.String(), "upstream rejected input")
			require.Contains(t, recorder.Body.String(), "data: ")
			if format == types.RelayFormatClaude {
				require.Contains(t, recorder.Body.String(), "event: error")
			} else {
				require.Contains(t, recorder.Body.String(), "data: [DONE]")
			}
		})
	}
}
