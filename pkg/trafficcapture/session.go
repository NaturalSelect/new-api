package trafficcapture

import (
	"net/http"
	"sync"
	"time"
)

// RecordMeta is the request-level identity filled in by the caller after the request ends.
type RecordMeta struct {
	RequestId string
	NodeName  string
	UserId    int
	TokenId   int
	ModelName string
	Group     string
	ClientIp  string
	Failed    bool
}

// ClientInput is the client-facing half of a request. The response body is not part of it
// because the Session already captured those bytes while they were being written.
type ClientInput struct {
	Method          string
	Url             string
	RequestHeaders  map[string][]string
	RequestBody     CapturedBody
	Status          int
	ResponseHeaders map[string][]string
}

// Session collects the capture state of one request: the client response bytes and one
// recorder per upstream attempt. It is released by exactly one of BuildRecord or Discard.
type Session struct {
	start           time.Time
	maxBodyBytes    int64
	captureUpstream bool
	clientResponse  *BoundedBuffer

	mu       sync.Mutex
	attempts []*UpstreamRecorder
}

func NewSession(maxBodyBytes int64, captureUpstream bool) *Session {
	start := time.Now()
	return &Session{
		start:           start,
		maxBodyBytes:    maxBodyBytes,
		captureUpstream: captureUpstream,
		clientResponse:  NewBoundedBuffer(start, maxBodyBytes),
	}
}

func (s *Session) MaxBodyBytes() int64 {
	return s.maxBodyBytes
}

func (s *Session) ClientResponseBuffer() *BoundedBuffer {
	return s.clientResponse
}

// BeginUpstream must be called before the request is sent, because it wraps req.Body to
// observe what the transport actually uploads. Returns nil when upstream capture is off;
// every UpstreamRecorder method tolerates a nil receiver.
func (s *Session) BeginUpstream(req *http.Request, retryIndex, channelId, channelType int) *UpstreamRecorder {
	if s == nil || !s.captureUpstream {
		return nil
	}
	recorder := &UpstreamRecorder{
		session:     s,
		retryIndex:  retryIndex,
		channelId:   channelId,
		channelType: channelType,
		method:      req.Method,
		url:         MaskURL(req.URL),
		reqHeaders:  MaskHeaders(req.Header),
		reqBody:     NewBoundedBuffer(s.start, s.maxBodyBytes),
		respBody:    NewBoundedBuffer(s.start, s.maxBodyBytes),
	}
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = TeeReadCloser(req.Body, recorder.reqBody)
	}
	s.mu.Lock()
	s.attempts = append(s.attempts, recorder)
	s.mu.Unlock()
	return recorder
}

// BuildRecord snapshots everything captured so far and releases the session's buffers.
func (s *Session) BuildRecord(meta RecordMeta, client ClientInput) Record {
	s.mu.Lock()
	attempts := s.attempts
	s.attempts = nil
	s.mu.Unlock()

	rec := Record{
		Version:     RecordVersion,
		RequestId:   meta.RequestId,
		NodeName:    meta.NodeName,
		StartedAtMs: s.start.UnixMilli(),
		DurationMs:  time.Since(s.start).Milliseconds(),
		UserId:      meta.UserId,
		TokenId:     meta.TokenId,
		ModelName:   meta.ModelName,
		Group:       meta.Group,
		ClientIp:    meta.ClientIp,
		Failed:      meta.Failed,
		Client: Exchange{
			Method:          client.Method,
			Url:             client.Url,
			RequestHeaders:  client.RequestHeaders,
			RequestBody:     client.RequestBody,
			Status:          client.Status,
			ResponseHeaders: client.ResponseHeaders,
			ResponseBody:    s.clientResponse.Snapshot(),
		},
		Upstream: make([]UpstreamAttempt, 0, len(attempts)),
	}
	for _, attempt := range attempts {
		rec.Upstream = append(rec.Upstream, attempt.snapshot())
		attempt.release()
	}
	s.clientResponse.Release()
	return rec
}

// Discard frees the session without producing a record.
func (s *Session) Discard() {
	s.mu.Lock()
	attempts := s.attempts
	s.attempts = nil
	s.mu.Unlock()
	for _, attempt := range attempts {
		attempt.release()
	}
	s.clientResponse.Release()
}

// UpstreamRecorder captures one attempt against an upstream provider.
type UpstreamRecorder struct {
	session     *Session
	retryIndex  int
	channelId   int
	channelType int
	method      string
	url         string
	reqHeaders  map[string][]string
	reqBody     *BoundedBuffer
	respBody    *BoundedBuffer

	mu          sync.Mutex
	status      int
	respHeaders map[string][]string
	errText     string
}

// Finish records the outcome of client.Do and wraps resp.Body so the bytes the handler
// reads are captured. It must be called before resp is handed to anyone else.
func (u *UpstreamRecorder) Finish(resp *http.Response, err error) {
	if u == nil {
		return
	}
	u.mu.Lock()
	if err != nil {
		u.errText = err.Error()
	}
	if resp != nil {
		u.status = resp.StatusCode
		u.respHeaders = MaskHeaders(resp.Header)
	}
	u.mu.Unlock()
	if resp != nil && resp.Body != nil {
		resp.Body = TeeReadCloser(resp.Body, u.respBody)
	}
}

func (u *UpstreamRecorder) snapshot() UpstreamAttempt {
	u.mu.Lock()
	defer u.mu.Unlock()
	return UpstreamAttempt{
		RetryIndex:  u.retryIndex,
		ChannelId:   u.channelId,
		ChannelType: u.channelType,
		Error:       u.errText,
		Exchange: Exchange{
			Method:          u.method,
			Url:             u.url,
			RequestHeaders:  u.reqHeaders,
			RequestBody:     u.reqBody.Snapshot(),
			Status:          u.status,
			ResponseHeaders: u.respHeaders,
			ResponseBody:    u.respBody.Snapshot(),
		},
	}
}

func (u *UpstreamRecorder) release() {
	u.reqBody.Release()
	u.respBody.Release()
}
