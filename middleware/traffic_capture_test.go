package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/trafficcapture"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/traffic_capture_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// trafficCaptureSettingPointer reaches the live setting through the config registry, which
// holds a pointer to it, so tests need no production-side setter.
func trafficCaptureSettingPointer(t *testing.T) *traffic_capture_setting.TrafficCaptureSetting {
	t.Helper()
	target, ok := config.GlobalConfig.Get("traffic_capture_setting").(*traffic_capture_setting.TrafficCaptureSetting)
	require.True(t, ok)
	return target
}

func useTrafficCaptureSetting(t *testing.T, mutate func(s *traffic_capture_setting.TrafficCaptureSetting)) {
	t.Helper()
	target := trafficCaptureSettingPointer(t)
	original := *target
	t.Cleanup(func() { *target = original })
	target.Enabled = true
	mutate(target)
}

type captureResult struct {
	response *httptest.ResponseRecorder
	records  []trafficcapture.Record
	// sessionDuringHandler is what the handler saw in the context, i.e. what upstream code would see.
	sessionDuringHandler *trafficcapture.Session
}

func runCapture(t *testing.T, req *http.Request, handler gin.HandlerFunc) captureResult {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var result captureResult
	original := enqueueTrafficCapture
	enqueueTrafficCapture = func(rec trafficcapture.Record) bool {
		result.records = append(result.records, rec)
		return true
	}
	t.Cleanup(func() { enqueueTrafficCapture = original })

	engine := gin.New()
	engine.Use(BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("token_id", 7)
		c.Set("original_model", "gpt-test")
		c.Next()
	})
	engine.Use(TrafficCapture())
	engine.Any("/v1/chat/completions", func(c *gin.Context) {
		result.sessionDuringHandler, _ = common.GetContextKeyType[*trafficcapture.Session](c, constant.ContextKeyTrafficCapture)
		handler(c)
	})

	result.response = httptest.NewRecorder()
	engine.ServeHTTP(result.response, req)
	return result
}

func newChatRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=AIzaSyABCDEFGHIJKLMNWXYZ", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-abcdefghijklmnop")
	return req
}

const sseBody = "data: one\n\n: PING\n\ndata: [DONE]\n\n"

func writeSSE(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write([]byte("data: one\n\n"))
	c.Writer.Flush()
	_, _ = c.Writer.WriteString(": PING\n\n")
	c.Writer.Flush()
	_, _ = c.Writer.WriteString("data: [DONE]\n\n")
}

func TestTrafficCaptureLeavesClientResponseAndWriterSizeIntact(t *testing.T) {
	useTrafficCaptureSetting(t, func(*traffic_capture_setting.TrafficCaptureSetting) {})
	var sizeAfterTwoWrites int
	result := runCapture(t, newChatRequest(`{"stream":true}`), func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.Write([]byte("data: one\n\n"))
		_, _ = c.Writer.WriteString(": PING\n\n")
		sizeAfterTwoWrites = c.Writer.Size()
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
	})

	require.Equal(t, sseBody, result.response.Body.String())
	require.Equal(t, len("data: one\n\n: PING\n\n"), sizeAfterTwoWrites, "Writer.Size must reflect the inner writer")
}

func TestTrafficCaptureKeepsFlushWorking(t *testing.T) {
	useTrafficCaptureSetting(t, func(*traffic_capture_setting.TrafficCaptureSetting) {})
	result := runCapture(t, newChatRequest(`{}`), writeSSE)

	require.True(t, result.response.Flushed)
	require.Equal(t, sseBody, result.response.Body.String())
}

func TestTrafficCaptureEnqueuesCompleteMaskedRecord(t *testing.T) {
	useTrafficCaptureSetting(t, func(*traffic_capture_setting.TrafficCaptureSetting) {})
	result := runCapture(t, newChatRequest(`{"stream":true}`), writeSSE)

	require.NotNil(t, result.sessionDuringHandler, "upstream code must find the session in the context")
	require.Len(t, result.records, 1)
	rec := result.records[0]

	require.Equal(t, 42, rec.UserId)
	require.Equal(t, 7, rec.TokenId)
	require.Equal(t, "gpt-test", rec.ModelName)
	require.False(t, rec.Failed)
	require.Equal(t, http.MethodPost, rec.Client.Method)
	require.Equal(t, http.StatusOK, rec.Client.Status)
	require.Equal(t, `{"stream":true}`, rec.Client.RequestBody.Data)
	require.Equal(t, sseBody, rec.Client.ResponseBody.Data)
	require.Equal(t, []string{"***mnop"}, rec.Client.RequestHeaders["Authorization"])
	require.Contains(t, rec.Client.Url, "key=***WXYZ")
	require.NotContains(t, rec.Client.Url, "AIzaSy")
}

func TestTrafficCaptureRecordsErrorResponses(t *testing.T) {
	useTrafficCaptureSetting(t, func(*traffic_capture_setting.TrafficCaptureSetting) {})
	result := runCapture(t, newChatRequest(`{}`), func(c *gin.Context) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "slow down"})
	})

	require.Len(t, result.records, 1)
	require.True(t, result.records[0].Failed)
	require.Equal(t, http.StatusTooManyRequests, result.records[0].Client.Status)
	require.Contains(t, result.records[0].Client.ResponseBody.Data, "slow down")
}

func TestTrafficCaptureTreatsFailureMarkedOn200AsFailed(t *testing.T) {
	useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) {
		s.RecordMode = traffic_capture_setting.RecordModeFailed
	})
	result := runCapture(t, newChatRequest(`{}`), func(c *gin.Context) {
		writeSSE(c)
		common.SetContextKey(c, constant.ContextKeyRelayFailed, true)
	})

	require.Len(t, result.records, 1)
	require.True(t, result.records[0].Failed)
}

func TestTrafficCaptureFailedModeDropsSuccessfulRequests(t *testing.T) {
	useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) {
		s.RecordMode = traffic_capture_setting.RecordModeFailed
	})
	result := runCapture(t, newChatRequest(`{}`), writeSSE)

	require.Empty(t, result.records)
	require.Equal(t, sseBody, result.response.Body.String())
}

func TestTrafficCaptureUserAllowList(t *testing.T) {
	t.Run("user outside the list is skipped", func(t *testing.T) {
		useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) { s.UserIds = []int{1, 2} })
		require.Empty(t, runCapture(t, newChatRequest(`{}`), writeSSE).records)
	})
	t.Run("user inside the list is captured", func(t *testing.T) {
		useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) { s.UserIds = []int{42} })
		require.Len(t, runCapture(t, newChatRequest(`{}`), writeSSE).records, 1)
	})
}

func TestTrafficCaptureDisabledDoesNothing(t *testing.T) {
	useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) { s.Enabled = false })
	result := runCapture(t, newChatRequest(`{}`), writeSSE)

	require.Nil(t, result.sessionDuringHandler)
	require.Empty(t, result.records)
	require.Equal(t, sseBody, result.response.Body.String())
}

func TestTrafficCaptureSkipsWebsocketUpgrade(t *testing.T) {
	useTrafficCaptureSetting(t, func(*traffic_capture_setting.TrafficCaptureSetting) {})
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	result := runCapture(t, req, func(c *gin.Context) { c.Status(http.StatusOK) })

	require.Nil(t, result.sessionDuringHandler)
	require.Empty(t, result.records)
}

func TestTrafficCaptureDiscardedSessionIgnoresLateWrites(t *testing.T) {
	useTrafficCaptureSetting(t, func(s *traffic_capture_setting.TrafficCaptureSetting) {
		s.RecordMode = traffic_capture_setting.RecordModeFailed
	})
	result := runCapture(t, newChatRequest(`{}`), writeSSE)

	require.Empty(t, result.records)
	_, err := result.sessionDuringHandler.ClientResponseBuffer().Write([]byte("late"))
	require.NoError(t, err, "a discarded session must tolerate late writes")
	require.Empty(t, result.sessionDuringHandler.ClientResponseBuffer().Snapshot().Data)
}

func TestUserCaptureAllowed(t *testing.T) {
	require.True(t, userCaptureAllowed(nil, 5), "empty allow-list means every user")
	require.True(t, userCaptureAllowed([]int{}, 5))
	require.True(t, userCaptureAllowed([]int{1, 5}, 5))
	require.False(t, userCaptureAllowed([]int{1, 2}, 5))
}

func TestCaptureClientRequestBodyReadsOnlyLimitButReportsFullSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("a", 100)))

	body := captureClientRequestBody(c, 10)
	common.CleanupBodyStorage(c)

	require.Equal(t, strings.Repeat("a", 10), body.Data)
	require.EqualValues(t, 100, body.Size)
	require.True(t, body.Truncated)
}
