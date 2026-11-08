package openai

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

func newImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing.png") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	t.Cleanup(server.Close)

	fetchSetting := system_setting.GetFetchSetting()
	originalSSRF := fetchSetting.EnableSSRFProtection
	originalMaxMB := constant.MaxFileDownloadMB
	fetchSetting.EnableSSRFProtection = false
	constant.MaxFileDownloadMB = 64
	t.Cleanup(func() {
		fetchSetting.EnableSSRFProtection = originalSSRF
		constant.MaxFileDownloadMB = originalMaxMB
	})
	service.InitHttpClient()
	return server
}

func runImageHandler(t *testing.T, responseFormat string, upstreamBody string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		RelayMode:   relayconstant.RelayModeImagesGenerations,
		Request:     &dto.ImageRequest{Model: "gpt-image-1", Prompt: "a cat", ResponseFormat: responseFormat},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}
	_, apiErr := OpenaiHandlerWithUsage(c, info, resp)
	if apiErr != nil {
		return recorder, apiErr
	}
	return recorder, nil
}

func TestOpenaiHandlerWithUsageConvertsURLToB64JSON(t *testing.T) {
	server := newImageServer(t)
	upstream := `{"created":1794157579,"data":[{"url":"` + server.URL + `/a.png","revised_prompt":"p"},{"b64_json":"already"},{"url":"` + server.URL + `/b.png"}],"usage":{"prompt_tokens":2954,"completion_tokens":5000,"total_tokens":7954}}`

	recorder, err := runImageHandler(t, "b64_json", upstream)
	require.NoError(t, err)

	body := recorder.Body.Bytes()
	want := base64.StdEncoding.EncodeToString(testPNG)
	require.Equal(t, want, gjson.GetBytes(body, "data.0.b64_json").String())
	require.False(t, gjson.GetBytes(body, "data.0.url").Exists())
	require.Equal(t, "p", gjson.GetBytes(body, "data.0.revised_prompt").String())
	require.Equal(t, "already", gjson.GetBytes(body, "data.1.b64_json").String())
	require.Equal(t, want, gjson.GetBytes(body, "data.2.b64_json").String())
	require.False(t, gjson.GetBytes(body, "data.2.url").Exists())
	require.Equal(t, int64(7954), gjson.GetBytes(body, "usage.total_tokens").Int())
	require.Equal(t, int64(1794157579), gjson.GetBytes(body, "created").Int())
}

func TestOpenaiHandlerWithUsageKeepsURLWhenB64NotRequested(t *testing.T) {
	server := newImageServer(t)
	upstream := `{"data":[{"url":"` + server.URL + `/a.png"}],"usage":{"total_tokens":1}}`

	for _, format := range []string{"", "url"} {
		recorder, err := runImageHandler(t, format, upstream)
		require.NoError(t, err)
		require.Equal(t, server.URL+"/a.png", gjson.GetBytes(recorder.Body.Bytes(), "data.0.url").String())
		require.False(t, gjson.GetBytes(recorder.Body.Bytes(), "data.0.b64_json").Exists())
	}
}

func TestOpenaiHandlerWithUsageReturnsErrorWhenDownloadFails(t *testing.T) {
	server := newImageServer(t)
	upstream := `{"data":[{"url":"` + server.URL + `/missing.png"}],"usage":{"total_tokens":1}}`

	recorder, err := runImageHandler(t, "b64_json", upstream)
	require.Error(t, err)
	require.Empty(t, recorder.Body.String())
	require.NotContains(t, err.Error(), server.URL)
}
