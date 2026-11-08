package middleware

import (
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

const (
	brotliQuality    = 5
	brotliWindowBits = 24
)

// NOTE: gzip's 32KB window cannot dedupe the repeated bodies inside a capture record; brotli's 16MB window can.
var brotliPathPrefixes = []string{"/api/traffic_capture/record/"}

type brotliWriter struct {
	gin.ResponseWriter
	writer *brotli.Writer
}

func (w *brotliWriter) Write(data []byte) (int, error) {
	w.Header().Del("Content-Length")
	return w.writer.Write(data)
}

func (w *brotliWriter) WriteString(s string) (int, error) {
	w.Header().Del("Content-Length")
	return w.writer.Write([]byte(s))
}

func (w *brotliWriter) WriteHeader(code int) {
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(code)
}

func (w *brotliWriter) Flush() {
	_ = w.writer.Flush()
	w.ResponseWriter.Flush()
}

// ApiCompression compresses API responses with gzip, except for the routes in
// brotliPathPrefixes, which use brotli when the client accepts it.
func ApiCompression() gin.HandlerFunc {
	gzipHandler := gzip.Gzip(gzip.DefaultCompression)
	return func(c *gin.Context) {
		if !shouldUseBrotli(c) {
			gzipHandler(c)
			return
		}
		bw := brotli.NewWriterOptions(c.Writer, brotli.WriterOptions{
			Quality: brotliQuality,
			LGWin:   brotliWindowBits,
		})
		defer func() { _ = bw.Close() }()
		c.Header("Content-Encoding", "br")
		c.Header("Vary", "Accept-Encoding")
		c.Writer = &brotliWriter{ResponseWriter: c.Writer, writer: bw}
		c.Next()
	}
}

func shouldUseBrotli(c *gin.Context) bool {
	path := c.Request.URL.Path
	matched := false
	for _, prefix := range brotliPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			matched = true
			break
		}
	}
	return matched && acceptsEncoding(c.GetHeader("Accept-Encoding"), "br")
}

func acceptsEncoding(header, encoding string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(part, ";")
		if strings.TrimSpace(token) != encoding {
			continue
		}
		return strings.ReplaceAll(strings.TrimSpace(params), " ", "") != "q=0"
	}
	return false
}
