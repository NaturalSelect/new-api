package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamKeepAliveCoversPreparationAndRetries(t *testing.T) {
	// 使用真实 HTTP 连接，分别阻塞请求准备和一次渠道重试。
	prepareDone := make(chan struct{})
	retryDone := make(chan struct{})
	defer func() {
		select {
		case <-prepareDone:
		default:
			close(prepareDone)
		}
		select {
		case <-retryDone:
		default:
			close(retryDone)
		}
	}()
	engine := gin.New()
	engine.GET("/stream", func(c *gin.Context) {
		stop := StartStreamKeepAlive(c, 10*time.Millisecond)
		defer stop()
		select {
		case <-prepareDone:
		case <-c.Request.Context().Done():
			return
		}
		// 渠道层的清理不能结束外层持有的保活。
		stopAttempt := StartStreamKeepAlive(c, time.Hour)
		stopAttempt()
		select {
		case <-retryDone:
		case <-c.Request.Context().Done():
			return
		}
		_ = StringData(c, `{"result":"ok"}`)
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(server.URL + "/stream")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	reader := bufio.NewReader(response.Body)
	readPing := func() {
		t.Helper()
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, ": PING\n", line)
		line, readErr = reader.ReadString('\n')
		require.NoError(t, readErr)
		require.Equal(t, "\n", line)
	}
	readPing()
	readPing()
	close(prepareDone)
	readPing()
	close(retryDone)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(rest), `data: {"result":"ok"}`)
}

func TestStreamKeepAliveHandoffAndCancellation(t *testing.T) {
	for _, action := range []string{"write", "write-string", "flush", "header", "header-now", "cancel"} {
		t.Run(action, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(requestContext)
			stop := StartStreamKeepAlive(c, time.Millisecond)
			defer stop()
			writer := c.Writer.(*streamKeepAliveWriter)
			switch action {
			case "write":
				_, _ = c.Writer.Write([]byte("data: ok\n\n"))
			case "write-string":
				_, _ = c.Writer.WriteString("data: ok\n\n")
			case "flush":
				c.Writer.Flush()
			case "header":
				c.Writer.WriteHeader(http.StatusOK)
			case "header-now":
				c.Writer.WriteHeaderNow()
			case "cancel":
				cancel()
			}
			select {
			case <-writer.done:
			case <-time.After(time.Second):
				t.Fatal("keepalive did not stop")
			}
			stop()
			require.True(t, strings.HasPrefix(recorder.Body.String(), ": PING\n\n"))
			if action == "write" || action == "write-string" {
				require.True(t, strings.HasSuffix(recorder.Body.String(), "data: ok\n\n"), fmt.Sprintf("interleaved output: %q", recorder.Body.String()))
			}
		})
	}
}
