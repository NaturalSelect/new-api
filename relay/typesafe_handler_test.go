package relay

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const typeSafeHelperBody = `{"state":{"z":1,"a":2},"model":"jev-latest","questions":{"q":{"type":"noul"}}}`

func newTypeSafeHelperContext(t *testing.T, contentType string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	return c
}

func readAll(t *testing.T, reader io.Reader) string {
	t.Helper()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

func TestTypeSafeRequestBodyJSON(t *testing.T) {
	t.Run("unchanged request forwards the client's bytes", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, "application/json", []byte(typeSafeHelperBody))
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{}

		body, closer, err := typeSafeRequestBody(c, info, request)
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Equal(t, typeSafeHelperBody, readAll(t, body))
		require.Equal(t, int64(len(typeSafeHelperBody)), info.UpstreamRequestBodySize)
	})

	t.Run("redirected model changes only the model value", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, "application/json", []byte(typeSafeHelperBody))
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		request.SetModelName("jev-1.14.0")
		info := &relaycommon.RelayInfo{}

		body, closer, err := typeSafeRequestBody(c, info, request)
		require.NoError(t, err)
		require.NotNil(t, closer)
		defer closer.Close()
		want := `{"state":{"z":1,"a":2},"model":"jev-1.14.0","questions":{"q":{"type":"noul"}}}`
		require.Equal(t, want, readAll(t, body))
		require.Equal(t, int64(len(want)), info.UpstreamRequestBodySize)
	})

	t.Run("a retry on a channel without a mapping restores the client's model", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, "application/json", []byte(typeSafeHelperBody))
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		request.SetModelName("jev-1.14.0")
		request.SetModelName("jev-latest")

		body, closer, err := typeSafeRequestBody(c, &relaycommon.RelayInfo{}, request)
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Equal(t, typeSafeHelperBody, readAll(t, body))
	})
}

func TestTypeSafeRequestBodyMultipart(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.WriteField("request", typeSafeHelperBody))
	file, err := writer.CreateFormFile("img", "a.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("PNG\x00DATA"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	original := buf.String()

	t.Run("unchanged request forwards the multipart bytes", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, writer.FormDataContentType(), buf.Bytes())
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		require.True(t, request.Multipart)

		body, closer, err := typeSafeRequestBody(c, &relaycommon.RelayInfo{}, request)
		require.NoError(t, err)
		require.Nil(t, closer)
		require.Equal(t, original, readAll(t, body))
	})

	t.Run("redirected model rewrites only the request part", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, writer.FormDataContentType(), buf.Bytes())
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		request.SetModelName("jev-1.14.0")

		body, closer, err := typeSafeRequestBody(c, &relaycommon.RelayInfo{}, request)
		require.NoError(t, err)
		require.NotNil(t, closer)
		defer closer.Close()

		reader := multipart.NewReader(body, writer.Boundary())
		values := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values[part.FormName()] = readAll(t, part)
		}
		require.Equal(t, `{"state":{"z":1,"a":2},"model":"jev-1.14.0","questions":{"q":{"type":"noul"}}}`, values["request"])
		require.Equal(t, "PNG\x00DATA", values["img"])
	})
}

func TestSystemOneRequestBodyByChannel(t *testing.T) {
	const body = `{"state":"The mug arrived cracked.","model":"jev-latest","questions":{"q":{"type":"noul","instructions":"Is it damaged?"}}}`

	newRequest := func(t *testing.T) (*gin.Context, *dto.TypeSafeRequest) {
		t.Helper()
		c := newTypeSafeHelperContext(t, "application/json", []byte(body))
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)
		return c, request
	}
	newInfo := func(apiType int) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, UpstreamModelName: "gemini-decision-1"}}
	}

	t.Run("typesafe channel gets the client's request", func(t *testing.T) {
		c, request := newRequest(t)

		reader, closer, apiErr := systemOneRequestBody(c, newInfo(constant.APITypeTypeSafe), request)

		require.Nil(t, apiErr)
		require.Nil(t, closer)
		require.Equal(t, body, readAll(t, reader))
	})

	t.Run("gemini channel gets a decisions request", func(t *testing.T) {
		c, request := newRequest(t)
		info := newInfo(constant.APITypeGemini)

		reader, closer, apiErr := systemOneRequestBody(c, info, request)

		require.Nil(t, apiErr)
		require.NotNil(t, closer)
		defer closer.Close()
		want := `{"model":"gemini-decision-1","input":"The mug arrived cracked.","questions":{"q":{"type":"noul","instructions":"Is it damaged?"}}}`
		require.JSONEq(t, want, readAll(t, reader))
		require.Equal(t, int64(len(want)), info.UpstreamRequestBodySize)
	})

	t.Run("a request Google cannot take is a 400 that is not retried", func(t *testing.T) {
		c := newTypeSafeHelperContext(t, "application/json", []byte(`{"model":"jev-latest","state":"s","questions":{"q":{"type":"box","instructions":"find it"}}}`))
		request, err := dto.ParseTypeSafeRequest(c)
		require.NoError(t, err)

		_, _, apiErr := systemOneRequestBody(c, newInfo(constant.APITypeGemini), request)

		require.NotNil(t, apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.Contains(t, apiErr.Error(), "box questions are not supported")
		require.True(t, types.IsSkipRetryError(apiErr))
	})

	t.Run("other channel types are rejected", func(t *testing.T) {
		c, request := newRequest(t)

		_, _, apiErr := systemOneRequestBody(c, newInfo(constant.APITypeOpenAI), request)

		require.NotNil(t, apiErr)
		require.Equal(t, types.ErrorCodeInvalidApiType, apiErr.GetErrorCode())
		require.True(t, types.IsSkipRetryError(apiErr))
	})
}
