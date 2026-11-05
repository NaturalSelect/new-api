package typesafe

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const orderedBody = `{"state":{"z":1,"a":2},"model":"jev-latest","questions":{"q":{"type":"noul","instructions":"ok?"}}}`

func newInfo(t *testing.T, baseURL string, request *dto.TypeSafeRequest) *relaycommon.RelayInfo {
	t.Helper()
	return &relaycommon.RelayInfo{
		Request: request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeTypeSafe,
			ChannelBaseUrl: baseURL,
			ApiKey:         "sk-test",
		},
	}
}

func newRequest(t *testing.T, body string, multipartBody, longContext bool) *dto.TypeSafeRequest {
	t.Helper()
	request, err := dto.NewTypeSafeRequest([]byte(body), multipartBody, longContext)
	require.NoError(t, err)
	return request
}

func newContext(t *testing.T, contentType string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
	c.Request.Header.Set("Content-Type", contentType)
	return c, recorder
}

func TestGetRequestURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "default", baseURL: "", want: "https://api.typesafe.ai/v1/systemone"},
		{name: "trailing slash", baseURL: "https://api.typesafe.ai/", want: "https://api.typesafe.ai/v1/systemone"},
		{name: "v1 suffix", baseURL: "https://api.typesafe.ai/v1", want: "https://api.typesafe.ai/v1/systemone"},
		{name: "v1 suffix with slash", baseURL: "https://api.typesafe.ai/v1/", want: "https://api.typesafe.ai/v1/systemone"},
		{name: "other gateway", baseURL: "https://gw.example.com/typesafe", want: "https://gw.example.com/typesafe/v1/systemone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := newInfo(t, tt.baseURL, newRequest(t, orderedBody, false, false))
			got, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSetupRequestHeader(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		c, _ := newContext(t, "application/json; charset=utf-8")
		info := newInfo(t, "", newRequest(t, orderedBody, false, false))
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

		require.Equal(t, "Bearer sk-test", header.Get("Authorization"))
		require.Equal(t, "application/json", header.Get("Content-Type"))
		require.Equal(t, "application/json", header.Get("Accept"))
		require.Empty(t, header.Get(dto.TypeSafeImagesHeader))
		require.Empty(t, header.Get(dto.TypeSafeLongContextHeader))
	})

	t.Run("images and long context", func(t *testing.T) {
		c, _ := newContext(t, "application/json")
		body := `{"model":"m","state":"s","questions":{"q":{"type":"box"}}}`
		info := newInfo(t, "", newRequest(t, body, false, true))
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

		require.Equal(t, "true", header.Get(dto.TypeSafeImagesHeader))
		require.Equal(t, "true", header.Get(dto.TypeSafeLongContextHeader))
	})

	t.Run("multipart keeps the client boundary", func(t *testing.T) {
		c, _ := newContext(t, "multipart/form-data; boundary=abc123")
		info := newInfo(t, "", newRequest(t, orderedBody, true, false))
		header := http.Header{}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))

		require.Equal(t, "multipart/form-data; boundary=abc123", header.Get("Content-Type"))
	})

	t.Run("api key required", func(t *testing.T) {
		c, _ := newContext(t, "application/json")
		info := newInfo(t, "", newRequest(t, orderedBody, false, false))
		info.ApiKey = ""
		header := http.Header{}
		require.Error(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
	})
}

func buildMultipart(t *testing.T) ([]byte, string, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.SetBoundary("fixedboundary42"))
	require.NoError(t, writer.WriteField("note", "before"))
	require.NoError(t, writer.WriteField("request", orderedBody))
	file, err := writer.CreateFormFile("img", "a.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("PNG\x00\x01DATA"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes(), writer.FormDataContentType(), writer.Boundary()
}

func TestRewriteRequestPartKeepsBoundaryOrderAndFiles(t *testing.T) {
	body, contentType, boundary := buildMultipart(t)
	rewritten := strings.Replace(orderedBody, `"jev-latest"`, `"jev-1.14.0"`, 1)

	out, err := RewriteRequestPart(body, contentType, []byte(rewritten))
	require.NoError(t, err)

	reader := multipart.NewReader(bytes.NewReader(out), boundary)
	var names []string
	values := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		names = append(names, part.FormName())
		values[part.FormName()] = string(data)
	}
	require.Equal(t, []string{"note", "request", "img"}, names)
	require.Equal(t, "before", values["note"])
	require.Equal(t, rewritten, values["request"])
	require.Equal(t, "PNG\x00\x01DATA", values["img"])
}

func TestRewriteRequestPartErrors(t *testing.T) {
	body, contentType, _ := buildMultipart(t)

	_, err := RewriteRequestPart(body, "application/json", []byte("{}"))
	require.Error(t, err)

	_, err = RewriteRequestPart(body, "multipart/form-data", []byte("{}"))
	require.ErrorContains(t, err, "boundary")

	var noRequest bytes.Buffer
	writer := multipart.NewWriter(&noRequest)
	require.NoError(t, writer.WriteField("note", "x"))
	require.NoError(t, writer.Close())
	_, err = RewriteRequestPart(noRequest.Bytes(), writer.FormDataContentType(), []byte("{}"))
	require.ErrorContains(t, err, "missing")

	_, err = RewriteRequestPart(body, contentType, []byte("{}"))
	require.NoError(t, err)
}

func upstreamResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDoResponseRelaysBodyAndBillsInputTokens(t *testing.T) {
	c, recorder := newContext(t, "application/json")
	info := newInfo(t, "", newRequest(t, orderedBody, false, false))
	info.SetEstimatePromptTokens(dto.TypeSafeMaxInputTokens)
	upstream := `{"model":"jev-latest","answers":{"q":{"type":"noul","value":true}},"usage":{"input_tokens":1234}}`

	usage, apiErr := (&Adaptor{}).DoResponse(c, upstreamResponse(http.StatusOK, upstream), info)
	require.Nil(t, apiErr)

	require.Equal(t, upstream, recorder.Body.String())
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, &dto.Usage{PromptTokens: 1234, TotalTokens: 1234}, usage)
}

func TestDoResponseBillsTheReservedEstimateWhenUsageIsUnusable(t *testing.T) {
	answers := `"model":"jev-latest","answers":{"q":{"type":"noul","value":true}}`
	tests := []struct {
		name string
		body string
	}{
		{name: "missing", body: `{` + answers + `}`},
		{name: "negative", body: `{` + answers + `,"usage":{"input_tokens":-1}}`},
		{name: "fraction", body: `{` + answers + `,"usage":{"input_tokens":1.5}}`},
		{name: "string", body: `{` + answers + `,"usage":{"input_tokens":"12"}}`},
		{name: "over budget", body: `{` + answers + `,"usage":{"input_tokens":262401}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newContext(t, "application/json")
			info := newInfo(t, "", newRequest(t, orderedBody, false, false))
			info.SetEstimatePromptTokens(dto.TypeSafeMaxInputTokens)

			usage, apiErr := (&Adaptor{}).DoResponse(c, upstreamResponse(http.StatusOK, tt.body), info)
			require.Nil(t, apiErr)
			require.Equal(t, &dto.Usage{PromptTokens: dto.TypeSafeMaxInputTokens, TotalTokens: dto.TypeSafeMaxInputTokens}, usage)
		})
	}
}

func TestDoResponseRejectsAnswersThatDoNotMatchTheQuestions(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no model", body: `{"answers":{"q":{"type":"noul"}},"usage":{"input_tokens":1}}`},
		{name: "no answers", body: `{"model":"m","usage":{"input_tokens":1}}`},
		{name: "missing answer", body: `{"model":"m","answers":{},"usage":{"input_tokens":1}}`},
		{name: "wrong type", body: `{"model":"m","answers":{"q":{"type":"choice"}},"usage":{"input_tokens":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newContext(t, "application/json")
			info := newInfo(t, "", newRequest(t, orderedBody, false, false))

			_, apiErr := (&Adaptor{}).DoResponse(c, upstreamResponse(http.StatusOK, tt.body), info)
			require.NotNil(t, apiErr)
			require.False(t, c.Writer.Written())
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestDoResponseFindsAnswersForQuestionIDsWithPathCharacters(t *testing.T) {
	body := `{"model":"jev-latest","state":"s","questions":{"a.b":{"type":"noul"},"c*d":{"type":"choice"}}}`
	c, _ := newContext(t, "application/json")
	info := newInfo(t, "", newRequest(t, body, false, false))
	upstream := `{"model":"jev-latest","answers":{"a.b":{"type":"noul"},"c*d":{"type":"choice"}},"usage":{"input_tokens":5}}`

	usage, apiErr := (&Adaptor{}).DoResponse(c, upstreamResponse(http.StatusOK, upstream), info)
	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.(*dto.Usage).PromptTokens)
}

func TestErrorFromResponseKeepsTypeSafeDetail(t *testing.T) {
	c, _ := newContext(t, "application/json")
	raw := `{"detail":[{"loc":["body","state"],"msg":"field required","type":"missing"}]}`

	apiErr := ErrorFromResponse(c, upstreamResponse(http.StatusUnprocessableEntity, raw))
	require.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseStatusCode, apiErr.GetErrorCode())

	WriteError(c, apiErr)
	recorder := c.Writer.(interface{ Status() int })
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Status())
}

func TestWriteErrorUsesDetailShape(t *testing.T) {
	t.Run("upstream body is returned as sent", func(t *testing.T) {
		c, recorder := newContext(t, "application/json")
		raw := `{"detail":"Invalid API key"}`
		apiErr := ErrorFromResponse(c, upstreamResponse(http.StatusUnauthorized, raw))

		WriteError(c, apiErr)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Equal(t, raw, recorder.Body.String())
	})

	t.Run("upstream body without detail falls back to a detail message", func(t *testing.T) {
		c, recorder := newContext(t, "application/json")
		apiErr := ErrorFromResponse(c, upstreamResponse(http.StatusBadGateway, `<html>bad gateway</html>`))

		WriteError(c, apiErr)
		require.Equal(t, http.StatusBadGateway, recorder.Code)
		require.JSONEq(t, `{"detail":"bad response status code 502"}`, recorder.Body.String())
	})

	t.Run("relay error is wrapped in detail", func(t *testing.T) {
		c, recorder := newContext(t, "application/json")
		apiErr := types.NewErrorWithStatusCode(io.ErrUnexpectedEOF, types.ErrorCodeInvalidRequest, http.StatusBadRequest)

		WriteError(c, apiErr)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.JSONEq(t, `{"detail":"unexpected EOF"}`, recorder.Body.String())
	})

	t.Run("a stale upstream body is not reused for another error", func(t *testing.T) {
		c, recorder := newContext(t, "application/json")
		ErrorFromResponse(c, upstreamResponse(http.StatusUnauthorized, `{"detail":"Invalid API key"}`))
		ClearUpstreamError(c)
		apiErr := types.NewOpenAIError(io.ErrUnexpectedEOF, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)

		WriteError(c, apiErr)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.JSONEq(t, `{"detail":"unexpected EOF"}`, recorder.Body.String())
	})

	t.Run("an upstream body is not returned for a different error code", func(t *testing.T) {
		c, recorder := newContext(t, "application/json")
		ErrorFromResponse(c, upstreamResponse(http.StatusUnauthorized, `{"detail":"Invalid API key"}`))
		apiErr := types.NewErrorWithStatusCode(io.ErrUnexpectedEOF, types.ErrorCodeInvalidRequest, http.StatusBadRequest)

		WriteError(c, apiErr)
		require.JSONEq(t, `{"detail":"unexpected EOF"}`, recorder.Body.String())
	})
}

func TestRequestReachesTheUpstreamByteForByte(t *testing.T) {
	service.InitHttpClient()
	var gotBody []byte
	var gotHeader http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHeader = r.Header.Clone()
		require.Equal(t, "/v1/systemone", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{"q":{"type":"noul"}},"usage":{"input_tokens":7}}`))
	}))
	defer upstream.Close()

	request := newRequest(t, orderedBody, false, true)
	info := newInfo(t, upstream.URL, request)
	c, _ := newContext(t, "application/json")

	adaptor := &Adaptor{}
	resp, err := adaptor.DoRequest(c, info, strings.NewReader(string(request.RequestJSON)))
	require.NoError(t, err)
	httpResp := resp.(*http.Response)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	_ = httpResp.Body.Close()

	require.Equal(t, orderedBody, string(gotBody))
	require.Equal(t, "Bearer sk-test", gotHeader.Get("Authorization"))
	require.Equal(t, "true", gotHeader.Get(dto.TypeSafeLongContextHeader))
}
