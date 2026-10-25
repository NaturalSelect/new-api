package trafficcapture

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setInflightBudget(t *testing.T, budget int64) {
	t.Helper()
	previous := inflightBudgetBytes
	inflightBudgetBytes = budget
	t.Cleanup(func() { inflightBudgetBytes = previous })
}

func TestBoundedBufferTruncatesButKeepsCountingSize(t *testing.T) {
	buf := NewBoundedBuffer(time.Now(), 10)
	defer buf.Release()

	n, err := buf.Write([]byte("0123456789abcdef"))
	require.NoError(t, err)
	require.Equal(t, 16, n)
	_, _ = buf.Write([]byte("more"))

	body := buf.Snapshot()
	require.Equal(t, "0123456789", body.Data)
	require.EqualValues(t, 20, body.Size)
	require.True(t, body.Truncated)
	require.Equal(t, EncodingText, body.Encoding)
}

func TestBoundedBufferUntruncatedKeepsEverything(t *testing.T) {
	buf := NewBoundedBuffer(time.Now(), 1024)
	defer buf.Release()
	_, _ = buf.Write([]byte("data: a\n\n"))
	_, _ = buf.Write([]byte(": PING\n\n"))

	body := buf.Snapshot()
	require.Equal(t, "data: a\n\n: PING\n\n", body.Data)
	require.False(t, body.Truncated)
	require.EqualValues(t, len(body.Data), body.Size)
	require.GreaterOrEqual(t, body.LastByteMs, body.FirstByteMs)
}

func TestBoundedBufferEncodingChoice(t *testing.T) {
	t.Run("valid utf8 stays text", func(t *testing.T) {
		buf := NewBoundedBuffer(time.Now(), 1024)
		defer buf.Release()
		_, _ = buf.Write([]byte("你好"))
		require.Equal(t, CapturedBody{Encoding: EncodingText, Data: "你好", Size: 6}, withoutTiming(buf.Snapshot()))
	})
	t.Run("binary becomes base64", func(t *testing.T) {
		buf := NewBoundedBuffer(time.Now(), 1024)
		defer buf.Release()
		_, _ = buf.Write([]byte{0xff, 0xfe, 0x00, 0x01})
		body := buf.Snapshot()
		require.Equal(t, EncodingBase64, body.Encoding)
		require.Equal(t, "//4AAQ==", body.Data)
	})
	t.Run("truncation inside a multibyte rune stays text", func(t *testing.T) {
		buf := NewBoundedBuffer(time.Now(), 4)
		defer buf.Release()
		_, _ = buf.Write([]byte("你好"))
		body := buf.Snapshot()
		require.Equal(t, EncodingText, body.Encoding)
		require.Equal(t, "你", body.Data)
		require.True(t, body.Truncated)
	})
	t.Run("empty body is empty text", func(t *testing.T) {
		buf := NewBoundedBuffer(time.Now(), 1024)
		defer buf.Release()
		require.Equal(t, CapturedBody{Encoding: EncodingText}, buf.Snapshot())
	})
}

func withoutTiming(b CapturedBody) CapturedBody {
	b.FirstByteMs, b.LastByteMs = 0, 0
	return b
}

func TestInflightBudgetExhaustionTruncatesAndReleaseRestores(t *testing.T) {
	setInflightBudget(t, 8)
	startingInflight := inflightBytes.Load()

	first := NewBoundedBuffer(time.Now(), 1024)
	_, _ = first.Write([]byte("12345"))
	second := NewBoundedBuffer(time.Now(), 1024)
	_, _ = second.Write([]byte("abcdef"))

	require.Equal(t, "12345", first.Snapshot().Data)
	secondBody := second.Snapshot()
	require.Equal(t, "abc", secondBody.Data)
	require.True(t, secondBody.Truncated)
	require.EqualValues(t, 6, secondBody.Size)

	first.Release()
	second.Release()
	require.Equal(t, startingInflight, inflightBytes.Load())

	third := NewBoundedBuffer(time.Now(), 1024)
	defer third.Release()
	_, _ = third.Write([]byte("12345678"))
	require.False(t, third.Snapshot().Truncated)
}

func TestBoundedBufferIgnoresWritesAfterRelease(t *testing.T) {
	buf := NewBoundedBuffer(time.Now(), 1024)
	_, _ = buf.Write([]byte("abc"))
	buf.Release()
	buf.Release()
	n, err := buf.Write([]byte("late"))
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Empty(t, buf.Snapshot().Data)
}

func TestReadBodyReportsRealSizeWhenOnlyPrefixIsRead(t *testing.T) {
	body := ReadBody(strings.NewReader("abcdefghij"), 100, 4)
	require.Equal(t, "abcd", body.Data)
	require.EqualValues(t, 100, body.Size)
	require.True(t, body.Truncated)

	whole := ReadBody(strings.NewReader("abc"), 3, 1024)
	require.Equal(t, "abc", whole.Data)
	require.False(t, whole.Truncated)
}

func TestTeeReadCloserCopiesReadsAndPassesClose(t *testing.T) {
	closed := false
	source := &closeSpy{Reader: strings.NewReader("payload"), onClose: func() { closed = true }}
	buf := NewBoundedBuffer(time.Now(), 1024)
	defer buf.Release()

	tee := TeeReadCloser(source, buf)
	got, err := io.ReadAll(tee)
	require.NoError(t, err)
	require.Equal(t, "payload", string(got))
	require.Equal(t, "payload", buf.Snapshot().Data)

	require.NoError(t, tee.Close())
	require.True(t, closed)
}

type closeSpy struct {
	io.Reader
	onClose func()
}

func (c *closeSpy) Close() error {
	c.onClose()
	return nil
}

func TestMaskHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer sk-abcdefghijklmnop")
	headers.Set("X-Api-Key", "short")
	headers.Set("X-Goog-Api-Key", "AIzaSyABCDEFGHIJKLMN")
	headers.Set("X-Custom-Secret", "supersecretvalue1234")
	headers.Set("X-Auth-Token", "tok_1234567890abcdef")
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Ratelimit-Remaining-Tokens", "999")
	headers.Add("Set-Cookie", "session=abcdefghijklmnop")

	masked := MaskHeaders(headers)

	require.Equal(t, []string{"***mnop"}, masked["Authorization"])
	require.Equal(t, []string{"***"}, masked["X-Api-Key"])
	require.Equal(t, []string{"***KLMN"}, masked["X-Goog-Api-Key"])
	require.Equal(t, []string{"***1234"}, masked["X-Custom-Secret"])
	require.Equal(t, []string{"***cdef"}, masked["X-Auth-Token"])
	require.Equal(t, []string{"***mnop"}, masked["Set-Cookie"])
	require.Equal(t, []string{"application/json"}, masked["Content-Type"])
	require.Equal(t, []string{"999"}, masked["X-Ratelimit-Remaining-Tokens"])
	require.Equal(t, "Bearer sk-abcdefghijklmnop", headers.Get("Authorization"), "input must not be mutated")
}

func TestMaskURL(t *testing.T) {
	parse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return u
	}

	require.Equal(t,
		"https://example.com/v1beta/models/x:streamGenerateContent?alt=sse&key=***WXYZ",
		MaskURL(parse("https://example.com/v1beta/models/x:streamGenerateContent?alt=sse&key=AIzaSyABCDEFGHIJKLMNWXYZ")),
	)
	require.Equal(t,
		"https://example.com/p?access_token=***&other=1",
		MaskURL(parse("https://example.com/p?access_token=short&other=1")),
	)
	require.Equal(t, "https://example.com/plain?a=1", MaskURL(parse("https://example.com/plain?a=1")))
	require.Equal(t, "", MaskURL(nil))
}

func TestCodecRoundTrip(t *testing.T) {
	original := Record{
		Version:    RecordVersion,
		RequestId:  "20260101000000000000abcd",
		UserId:     7,
		ModelName:  "gpt-test",
		Failed:     true,
		DurationMs: 1234,
		Client: Exchange{
			Method:         "POST",
			Url:            "/v1/chat/completions",
			RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
			RequestBody:    CapturedBody{Encoding: EncodingText, Data: `{"stream":true}`, Size: 15},
			Status:         200,
			ResponseBody:   CapturedBody{Encoding: EncodingText, Data: strings.Repeat("data: x\n\n", 500), Size: 4500},
		},
		Upstream: []UpstreamAttempt{{
			RetryIndex: 1, ChannelId: 3, ChannelType: 1, Error: "boom",
			Exchange: Exchange{Method: "POST", Status: 502},
		}},
	}

	payload, rawSize, err := Encode(original)
	require.NoError(t, err)
	require.Greater(t, rawSize, int64(len(payload)), "repetitive SSE should compress")

	decoded, err := Decode(payload)
	require.NoError(t, err)
	require.Equal(t, original, decoded)
}

func TestDecodeRejectsGarbage(t *testing.T) {
	_, err := Decode([]byte("not zstd"))
	require.Error(t, err)
}

func TestSessionRecordsUpstreamAttemptsAndMasksSecrets(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Set-Cookie", "upstream-session-0123456789")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	session := NewSession(1024, true)
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v1/chat?key=AIzaSyABCDEFGHIJKLMNWXYZ", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer sk-abcdefghijklmnop")

	recorder := session.BeginUpstream(req, 2, 11, 1)
	resp, err := http.DefaultClient.Do(req)
	recorder.Finish(resp, err)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	_, _ = session.ClientResponseBuffer().Write([]byte("client-bytes"))
	rec := session.BuildRecord(RecordMeta{RequestId: "rid", UserId: 5}, ClientInput{Method: "POST", Status: 200})

	require.Equal(t, "client-bytes", rec.Client.ResponseBody.Data)
	require.Len(t, rec.Upstream, 1)
	attempt := rec.Upstream[0]
	require.Equal(t, 2, attempt.RetryIndex)
	require.Equal(t, 11, attempt.ChannelId)
	require.Equal(t, 200, attempt.Status)
	require.Equal(t, []string{"***mnop"}, attempt.RequestHeaders["Authorization"])
	require.Contains(t, attempt.Url, "key=***WXYZ")
	require.NotContains(t, attempt.Url, "AIzaSy")
	require.Equal(t, "{}", attempt.RequestBody.Data)
	require.Equal(t, "data: [DONE]\n\n", attempt.ResponseBody.Data)
	require.Equal(t, []string{"***6789"}, attempt.ResponseHeaders["Set-Cookie"])
}

func TestSessionRecordsTransportError(t *testing.T) {
	session := NewSession(1024, true)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/unreachable", nil)
	require.NoError(t, err)

	recorder := session.BeginUpstream(req, 0, 1, 1)
	resp, doErr := http.DefaultClient.Do(req)
	recorder.Finish(resp, doErr)
	require.Error(t, doErr)

	rec := session.BuildRecord(RecordMeta{}, ClientInput{})
	require.Len(t, rec.Upstream, 1)
	require.NotEmpty(t, rec.Upstream[0].Error)
	require.Zero(t, rec.Upstream[0].Status)
}

func TestSessionWithUpstreamCaptureDisabledAndNilRecorderAreNoOps(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://example.com", bytes.NewReader([]byte("x")))
	require.NoError(t, err)

	session := NewSession(1024, false)
	recorder := session.BeginUpstream(req, 0, 1, 1)
	require.Nil(t, recorder)
	_, wrapped := req.Body.(*teeReadCloser)
	require.False(t, wrapped, "request body must be left untouched when upstream capture is off")
	recorder.Finish(nil, nil)

	require.Empty(t, session.BuildRecord(RecordMeta{}, ClientInput{}).Upstream)
}

func TestSessionReleaseReturnsInflightBudget(t *testing.T) {
	startingInflight := inflightBytes.Load()

	built := NewSession(1024, true)
	_, _ = built.ClientResponseBuffer().Write([]byte("hello"))
	require.Greater(t, inflightBytes.Load(), startingInflight)
	built.BuildRecord(RecordMeta{}, ClientInput{})
	require.Equal(t, startingInflight, inflightBytes.Load())

	discarded := NewSession(1024, true)
	_, _ = discarded.ClientResponseBuffer().Write([]byte("hello"))
	discarded.Discard()
	require.Equal(t, startingInflight, inflightBytes.Load())
}
