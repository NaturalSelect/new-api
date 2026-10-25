package trafficcapture

import (
	"encoding/base64"
	"io"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// inflightBudgetBytes caps the memory held by all not-yet-released buffers combined, so a
// burst of large concurrent requests cannot exhaust the process. Exceeding it truncates.
var inflightBudgetBytes int64 = 256 << 20

var inflightBytes atomic.Int64

func reserveInflight(want int64) int64 {
	for {
		current := inflightBytes.Load()
		available := inflightBudgetBytes - current
		if available <= 0 {
			return 0
		}
		granted := min(want, available)
		if inflightBytes.CompareAndSwap(current, current+granted) {
			return granted
		}
	}
}

// BoundedBuffer is an io.Writer that never fails and never blocks its caller: once the
// byte limit or the global in-flight budget is hit it keeps counting but stops storing.
type BoundedBuffer struct {
	mu          sync.Mutex
	start       time.Time
	limit       int64
	data        []byte
	reserved    int64
	size        int64
	truncated   bool
	seenByte    bool
	firstByteMs int64
	lastByteMs  int64
	released    bool
}

func NewBoundedBuffer(start time.Time, limit int64) *BoundedBuffer {
	return &BoundedBuffer{start: start, limit: limit}
}

func (b *BoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released || len(p) == 0 {
		return len(p), nil
	}
	b.size += int64(len(p))
	elapsedMs := time.Since(b.start).Milliseconds()
	if !b.seenByte {
		b.firstByteMs = elapsedMs
		b.seenByte = true
	}
	b.lastByteMs = elapsedMs

	kept := int64(0)
	if room := b.limit - int64(len(b.data)); room > 0 {
		kept = reserveInflight(min(room, int64(len(p))))
	}
	b.data = append(b.data, p[:kept]...)
	b.reserved += kept
	if kept < int64(len(p)) {
		b.truncated = true
	}
	return len(p), nil
}

func (b *BoundedBuffer) Snapshot() CapturedBody {
	b.mu.Lock()
	defer b.mu.Unlock()
	body := encodeBody(b.data, b.truncated)
	body.Size = b.size
	body.Truncated = b.truncated
	body.FirstByteMs = b.firstByteMs
	body.LastByteMs = b.lastByteMs
	return body
}

// Release returns the buffer's share of the in-flight budget and drops its bytes. Later
// writes are ignored, so a goroutine still draining a stream cannot resurrect memory.
func (b *BoundedBuffer) Release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.released {
		return
	}
	b.released = true
	inflightBytes.Add(-b.reserved)
	b.reserved = 0
	b.data = nil
}

// ReadBody captures a body that already exists elsewhere (e.g. the cached request body).
// totalSize is the real length, so reading only the first limit bytes still reports it.
func ReadBody(r io.Reader, totalSize, limit int64) CapturedBody {
	buf := NewBoundedBuffer(time.Now(), limit)
	defer buf.Release()
	_, _ = io.Copy(buf, io.LimitReader(r, limit))
	body := buf.Snapshot()
	body.FirstByteMs, body.LastByteMs = 0, 0
	if totalSize > body.Size {
		body.Size = totalSize
		body.Truncated = true
	}
	return body
}

func encodeBody(data []byte, truncated bool) CapturedBody {
	if len(data) == 0 {
		return CapturedBody{Encoding: EncodingText}
	}
	if text, ok := asValidUTF8(data, truncated); ok {
		return CapturedBody{Encoding: EncodingText, Data: string(text)}
	}
	return CapturedBody{Encoding: EncodingBase64, Data: base64.StdEncoding.EncodeToString(data)}
}

// asValidUTF8 tolerates a multi-byte character cut in half by truncation, which would
// otherwise push an ordinary text body into base64.
func asValidUTF8(data []byte, truncated bool) ([]byte, bool) {
	if utf8.Valid(data) {
		return data, true
	}
	if !truncated {
		return nil, false
	}
	for cut := 1; cut < utf8.UTFMax && cut < len(data); cut++ {
		if prefix := data[:len(data)-cut]; utf8.Valid(prefix) {
			return prefix, true
		}
	}
	return nil, false
}

type teeReadCloser struct {
	rc  io.ReadCloser
	buf *BoundedBuffer
}

// TeeReadCloser copies everything read from rc into buf. Close is passed through untouched.
func TeeReadCloser(rc io.ReadCloser, buf *BoundedBuffer) io.ReadCloser {
	return &teeReadCloser{rc: rc, buf: buf}
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		_, _ = t.buf.Write(p[:n])
	}
	return n, err
}

func (t *teeReadCloser) Close() error {
	return t.rc.Close()
}
