package middleware

import (
	"io"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/trafficcapture"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/traffic_capture_setting"

	"github.com/gin-gonic/gin"
)

// captureWriter mirrors every byte written to the client into buf. Only Write and
// WriteString are overridden; Size, Status, Flush and the rest come from the embedded
// writer, so stream keep-alive checks and SSE flushing behave exactly as before.
type captureWriter struct {
	gin.ResponseWriter
	buf *trafficcapture.BoundedBuffer
}

func (w *captureWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if n > 0 {
		_, _ = w.buf.Write(data[:n])
	}
	return n, err
}

func (w *captureWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	if n > 0 {
		_, _ = w.buf.Write([]byte(s[:n]))
	}
	return n, err
}

// enqueueTrafficCapture is a variable so tests can observe records without a database writer.
var enqueueTrafficCapture = service.EnqueueTrafficCapture

// TrafficCapture records the client-facing request and response of relay routes, and exposes
// a session through the context so the upstream layer can add its own attempts. It must be
// registered inside BodyStorageCleanup: the request body is read after c.Next() returns.
func TrafficCapture() gin.HandlerFunc {
	return func(c *gin.Context) {
		setting := traffic_capture_setting.GetSetting()
		if !setting.Enabled || c.IsWebsocket() {
			c.Next()
			return
		}

		session := trafficcapture.NewSession(traffic_capture_setting.GetMaxBodyBytes(), setting.CaptureUpstream)
		common.SetContextKey(c, constant.ContextKeyTrafficCapture, session)
		originalWriter := c.Writer
		c.Writer = &captureWriter{ResponseWriter: originalWriter, buf: session.ClientResponseBuffer()}

		c.Next()

		c.Writer = originalWriter
		failed := !relaySucceeded(c)
		userId := c.GetInt("id")
		if (setting.RecordMode == traffic_capture_setting.RecordModeFailed && !failed) ||
			!userCaptureAllowed(setting.UserIds, userId) {
			session.Discard()
			return
		}

		record := session.BuildRecord(trafficcapture.RecordMeta{
			RequestId: c.GetString(common.RequestIdKey),
			NodeName:  common.NodeName,
			UserId:    userId,
			TokenId:   c.GetInt("token_id"),
			ModelName: c.GetString("original_model"),
			Group:     common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
			ClientIp:  c.ClientIP(),
			Failed:    failed,
		}, trafficcapture.ClientInput{
			Method:          c.Request.Method,
			Url:             trafficcapture.MaskURL(c.Request.URL),
			RequestHeaders:  trafficcapture.MaskHeaders(c.Request.Header),
			RequestBody:     captureClientRequestBody(c, session.MaxBodyBytes()),
			Status:          c.Writer.Status(),
			ResponseHeaders: trafficcapture.MaskHeaders(c.Writer.Header()),
		})
		enqueueTrafficCapture(record)
	}
}

func userCaptureAllowed(allowedUserIds []int, userId int) bool {
	return len(allowedUserIds) == 0 || slices.Contains(allowedUserIds, userId)
}

// captureClientRequestBody reads at most limit bytes from the cached request body, so a
// large upload is never pulled into memory just to be truncated.
func captureClientRequestBody(c *gin.Context, limit int64) trafficcapture.CapturedBody {
	empty := trafficcapture.CapturedBody{Encoding: trafficcapture.EncodingText}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return empty
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return empty
	}
	return trafficcapture.ReadBody(storage, storage.Size(), limit)
}
