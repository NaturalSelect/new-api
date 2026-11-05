package dto

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const typeSafeOrderedBody = `{"state":{"z":1,"a":2},"model":"jev-latest","questions":{"q":{"type":"noul","instructions":"ok?"}}}`

func TestNewTypeSafeRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "valid", body: typeSafeOrderedBody},
		{name: "text state", body: `{"model":"m","state":"hello","questions":{"q":{"type":"choice"}}}`},
		{name: "array state", body: `{"model":"m","state":[1,2],"questions":{"q":{"type":"score"}}}`},
		{name: "not json", body: `nope`, wantErr: "valid JSON"},
		{name: "not object", body: `[]`, wantErr: "JSON object"},
		{name: "extra field", body: `{"model":"m","state":"s","questions":{"q":{"type":"noul"}},"temperature":0}`, wantErr: "does not accept the request field temperature"},
		{name: "missing model", body: `{"state":"s","questions":{"q":{"type":"noul"}}}`, wantErr: "model is required"},
		{name: "blank model", body: `{"model":" ","state":"s","questions":{"q":{"type":"noul"}}}`, wantErr: "model is required"},
		{name: "null state", body: `{"model":"m","state":null,"questions":{"q":{"type":"noul"}}}`, wantErr: "state must be"},
		{name: "number state", body: `{"model":"m","state":3,"questions":{"q":{"type":"noul"}}}`, wantErr: "state must be"},
		{name: "empty questions", body: `{"model":"m","state":"s","questions":{}}`, wantErr: "nonempty object"},
		{name: "array questions", body: `{"model":"m","state":"s","questions":[]}`, wantErr: "nonempty object"},
		{name: "bad question type", body: `{"model":"m","state":"s","questions":{"q":{"type":"essay"}}}`, wantErr: "each question must have type"},
		{name: "question not object", body: `{"model":"m","state":"s","questions":{"q":"noul"}}`, wantErr: "each question must have type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := NewTypeSafeRequest([]byte(tt.body), false, false)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []byte(tt.body), request.RequestJSON)
		})
	}
}

func TestNewTypeSafeRequestNeedsImages(t *testing.T) {
	plain, err := NewTypeSafeRequest([]byte(typeSafeOrderedBody), false, false)
	require.NoError(t, err)
	require.False(t, plain.NeedsImages)

	withAssets, err := NewTypeSafeRequest([]byte(`{"model":"m","state":"s","questions":{"q":{"type":"noul"}},"assets":{}}`), false, false)
	require.NoError(t, err)
	require.True(t, withAssets.NeedsImages)

	withBox, err := NewTypeSafeRequest([]byte(`{"model":"m","state":"s","questions":{"q":{"type":"box"}}}`), false, false)
	require.NoError(t, err)
	require.True(t, withBox.NeedsImages)
}

func TestTypeSafeSetModelNameKeepsEveryOtherByte(t *testing.T) {
	request, err := NewTypeSafeRequest([]byte(typeSafeOrderedBody), false, false)
	require.NoError(t, err)

	request.SetModelName("jev-latest")
	require.Equal(t, typeSafeOrderedBody, string(request.RequestJSON))

	request.SetModelName("")
	require.Equal(t, typeSafeOrderedBody, string(request.RequestJSON))

	request.SetModelName("jev-1.14.0")
	require.Equal(t, strings.Replace(typeSafeOrderedBody, `"jev-latest"`, `"jev-1.14.0"`, 1), string(request.RequestJSON))
	require.Equal(t, "jev-1.14.0", request.Model)

	request.SetModelName("jev-latest")
	require.Equal(t, typeSafeOrderedBody, string(request.RequestJSON))
}

func TestTypeSafeRequestMarshalJSONIsTheOriginalText(t *testing.T) {
	request, err := NewTypeSafeRequest([]byte(typeSafeOrderedBody), false, false)
	require.NoError(t, err)
	got, err := request.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, typeSafeOrderedBody, string(got))
}

func newTypeSafeContext(t *testing.T, contentType string, body []byte, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", contentType)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c
}

func TestParseTypeSafeRequestJSONBody(t *testing.T) {
	c := newTypeSafeContext(t, "application/json", []byte(typeSafeOrderedBody), map[string]string{TypeSafeLongContextHeader: " TRUE "})

	request, err := ParseTypeSafeRequest(c)
	require.NoError(t, err)
	require.Equal(t, typeSafeOrderedBody, string(request.RequestJSON))
	require.False(t, request.Multipart)
	require.True(t, request.LongContext)
}

func TestParseTypeSafeRequestLongContextHeaderMustBeTrue(t *testing.T) {
	c := newTypeSafeContext(t, "application/json", []byte(typeSafeOrderedBody), map[string]string{TypeSafeLongContextHeader: "false"})

	request, err := ParseTypeSafeRequest(c)
	require.NoError(t, err)
	require.False(t, request.LongContext)
}

func buildTypeSafeMultipart(t *testing.T, requestFields ...string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, field := range requestFields {
		require.NoError(t, writer.WriteField("request", field))
	}
	file, err := writer.CreateFormFile("img", "a.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("PNGDATA"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes(), writer.FormDataContentType()
}

func TestParseTypeSafeRequestMultipart(t *testing.T) {
	body, contentType := buildTypeSafeMultipart(t, typeSafeOrderedBody)
	c := newTypeSafeContext(t, contentType, body, nil)

	request, err := ParseTypeSafeRequest(c)
	require.NoError(t, err)
	require.True(t, request.Multipart)
	require.Equal(t, typeSafeOrderedBody, string(request.RequestJSON))

	again, multipart, err := ReadTypeSafeRequestJSON(c)
	require.NoError(t, err)
	require.True(t, multipart)
	require.Equal(t, typeSafeOrderedBody, string(again))
}

func TestReadTypeSafeRequestJSONMultipartNeedsExactlyOneRequestField(t *testing.T) {
	missing, contentType := buildTypeSafeMultipart(t)
	_, _, err := ReadTypeSafeRequestJSON(newTypeSafeContext(t, contentType, missing, nil))
	require.ErrorContains(t, err, "one request field")

	twice, contentType := buildTypeSafeMultipart(t, typeSafeOrderedBody, typeSafeOrderedBody)
	_, _, err = ReadTypeSafeRequestJSON(newTypeSafeContext(t, contentType, twice, nil))
	require.ErrorContains(t, err, "one request field")
}
