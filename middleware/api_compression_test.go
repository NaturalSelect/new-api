package middleware

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// repeatedPayload repeats an incompressible block farther apart than gzip's 32KB window.
func repeatedPayload() []byte {
	block := make([]byte, 96<<10)
	rand.New(rand.NewSource(1)).Read(block)
	encoded := []byte(base64.StdEncoding.EncodeToString(block))
	return bytes.Join([][]byte{encoded, encoded}, []byte(`","again":"`))
}

func serveCompressed(t *testing.T, path, acceptEncoding string, payload []byte) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")
	api.Use(ApiCompression())
	handler := func(c *gin.Context) { c.Data(http.StatusOK, "application/json", payload) }
	api.GET("/traffic_capture/record/:request_id", handler)
	api.GET("/other", handler)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, data []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return out
}

func TestApiCompressionUsesBrotliForCaptureRecord(t *testing.T) {
	payload := repeatedPayload()
	rec := serveCompressed(t, "/api/traffic_capture/record/abc", "gzip, deflate, br, zstd", payload)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "br", rec.Header().Get("Content-Encoding"))
	require.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
	got, err := io.ReadAll(brotli.NewReader(rec.Body))
	require.NoError(t, err)
	require.Equal(t, payload, got)

	gzipRec := serveCompressed(t, "/api/traffic_capture/record/abc", "gzip", payload)
	require.Less(t, rec.Body.Len()*10, gzipRec.Body.Len()*7, "brotli should beat gzip by at least 30%% on repeated bodies")
}

func TestApiCompressionFallsBackToGzipWithoutBrotliSupport(t *testing.T) {
	payload := repeatedPayload()
	for _, acceptEncoding := range []string{"gzip", "gzip, br;q=0"} {
		rec := serveCompressed(t, "/api/traffic_capture/record/abc", acceptEncoding, payload)
		require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"), acceptEncoding)
		require.Equal(t, payload, gunzip(t, rec.Body.Bytes()), acceptEncoding)
	}
}

func TestApiCompressionKeepsGzipForOtherRoutes(t *testing.T) {
	payload := repeatedPayload()
	rec := serveCompressed(t, "/api/other", "gzip, deflate, br", payload)

	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	require.Equal(t, payload, gunzip(t, rec.Body.Bytes()))
}

func TestApiCompressionSkipsClientsWithoutEncodings(t *testing.T) {
	payload := repeatedPayload()
	rec := serveCompressed(t, "/api/traffic_capture/record/abc", "", payload)

	require.Empty(t, rec.Header().Get("Content-Encoding"))
	require.Equal(t, payload, rec.Body.Bytes())
}
