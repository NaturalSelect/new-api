package helper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedRecorder makes ResponseRecorder safe to read while the keep-alive goroutine is writing.
type lockedRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (l *lockedRecorder) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ResponseRecorder.Write(p)
}

func (l *lockedRecorder) bodyString() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Body.String()
}

func newKeepAliveTestContext() (*gin.Context, *lockedRecorder) {
	recorder := &lockedRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, recorder
}

func TestStreamKeepAlive_StopWaitsForGoroutineAndHaltsWrites(t *testing.T) {
	t.Parallel()

	c, recorder := newKeepAliveTestContext()
	stop := StartStreamKeepAlive(c, StreamKeepAliveOptions{Interval: 50 * time.Millisecond})

	require.Eventually(t, func() bool {
		return strings.Contains(recorder.bodyString(), ": PING")
	}, 2*time.Second, 10*time.Millisecond)

	stop()
	stop()

	sizeAfterStop := len(recorder.bodyString())
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, sizeAfterStop, len(recorder.bodyString()), "no keep-alive may be written after stop returns")
}

func TestStreamKeepAlive_WritesAreSerializedWithWriteLocker(t *testing.T) {
	t.Parallel()

	c, recorder := newKeepAliveTestContext()
	locker := &sync.Mutex{}
	locker.Lock()
	stop := StartStreamKeepAlive(c, StreamKeepAliveOptions{Interval: 20 * time.Millisecond, WriteLocker: locker})

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, recorder.bodyString(), "keep-alive must wait while the caller holds the write lock")

	locker.Unlock()
	require.Eventually(t, func() bool {
		return strings.Contains(recorder.bodyString(), ": PING")
	}, 2*time.Second, 10*time.Millisecond)
	stop()
}

type failingResponseWriter struct {
	*httptest.ResponseRecorder
}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client connection broken")
}

func TestStreamKeepAlive_ReportsWriteFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(&failingResponseWriter{httptest.NewRecorder()})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	failed := make(chan error, 1)
	stop := StartStreamKeepAlive(c, StreamKeepAliveOptions{
		Interval:  20 * time.Millisecond,
		OnFailure: func(err error) { failed <- err },
	})
	defer stop()

	select {
	case err := <-failed:
		assert.ErrorContains(t, err, "client connection broken")
	case <-time.After(2 * time.Second):
		t.Fatal("expected OnFailure to be called after a failed write")
	}
}

func TestStreamKeepAlive_CancelledRequestStopsWithoutFailure(t *testing.T) {
	t.Parallel()

	c, recorder := newKeepAliveTestContext()
	reqCtx, cancel := context.WithCancel(context.Background())
	c.Request = c.Request.WithContext(reqCtx)

	failed := make(chan error, 1)
	stop := StartStreamKeepAlive(c, StreamKeepAliveOptions{
		Interval:  20 * time.Millisecond,
		OnFailure: func(err error) { failed <- err },
	})
	defer stop()

	require.Eventually(t, func() bool {
		return strings.Contains(recorder.bodyString(), ": PING")
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	time.Sleep(100 * time.Millisecond)
	sizeAfterCancel := len(recorder.bodyString())
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, sizeAfterCancel, len(recorder.bodyString()), "keep-alive must stop on its own when the client is gone")
	assert.Empty(t, failed, "a cancelled request is not a keep-alive failure")
}

func TestStreamKeepAlive_StopsAtMaxDuration(t *testing.T) {
	c, recorder := newKeepAliveTestContext()
	stop := StartStreamKeepAlive(c, StreamKeepAliveOptions{Interval: 20 * time.Millisecond, MaxDuration: 100 * time.Millisecond})
	defer stop()

	time.Sleep(300 * time.Millisecond)
	sizeAfterMax := len(recorder.bodyString())
	time.Sleep(100 * time.Millisecond)

	assert.Positive(t, sizeAfterMax, "keep-alive should have been sent before the max duration")
	assert.Equal(t, sizeAfterMax, len(recorder.bodyString()), "keep-alive must stop on its own once max duration elapses")
}

func TestHasWrittenPayload(t *testing.T) {
	t.Parallel()

	t.Run("nothing written", func(t *testing.T) {
		c, _ := newKeepAliveTestContext()
		assert.False(t, HasWrittenPayload(c))
	})

	t.Run("only keep-alive written", func(t *testing.T) {
		c, recorder := newKeepAliveTestContext()
		require.NoError(t, PingData(c))
		require.NoError(t, PingData(c))
		assert.Equal(t, 2*len(pingFrame), len(recorder.bodyString()))
		assert.False(t, HasWrittenPayload(c))
	})

	t.Run("payload after keep-alive", func(t *testing.T) {
		c, _ := newKeepAliveTestContext()
		require.NoError(t, PingData(c))
		require.NoError(t, StringData(c, "hello"))
		assert.True(t, HasWrittenPayload(c))
	})

	t.Run("payload only", func(t *testing.T) {
		c, _ := newKeepAliveTestContext()
		require.NoError(t, StringData(c, "hello"))
		assert.True(t, HasWrittenPayload(c))
	})
}

func TestWriteStreamError(t *testing.T) {
	t.Parallel()

	apiErr := types.NewOpenAIError(errors.New("upstream exploded"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)

	t.Run("claude format uses error event", func(t *testing.T) {
		c, recorder := newKeepAliveTestContext()
		WriteStreamError(c, types.RelayFormatClaude, apiErr)
		body := recorder.bodyString()
		assert.Contains(t, body, "event: error")
		assert.Contains(t, body, "upstream exploded")
		assert.NotContains(t, body, "[DONE]")
	})

	t.Run("openai format uses data frame and DONE", func(t *testing.T) {
		c, recorder := newKeepAliveTestContext()
		WriteStreamError(c, types.RelayFormatOpenAI, apiErr)
		body := recorder.bodyString()
		assert.Contains(t, body, `data: {"error"`)
		assert.Contains(t, body, "upstream exploded")
		assert.Contains(t, body, "data: [DONE]")
	})
}
