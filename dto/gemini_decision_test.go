package dto

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const geminiDecisionOrderedBody = `{"questions":{"q":{"type":"noul","instructions":"ok?"}},"model":"gemini-decision-1","input":"hello"}`

func TestNewGeminiDecisionRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "valid", body: geminiDecisionOrderedBody},
		{name: "array input", body: `{"model":"m","input":[{"type":"text","text":"a"}],"questions":{}}`},
		{name: "not json", body: `nope`, wantErr: "valid JSON"},
		{name: "not object", body: `[]`, wantErr: "JSON object"},
		{name: "missing model", body: `{"input":"x"}`, wantErr: "model is required"},
		{name: "blank model", body: `{"model":" ","input":"x"}`, wantErr: "model is required"},
		{name: "number model", body: `{"model":7,"input":"x"}`, wantErr: "model is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := NewGeminiDecisionRequest([]byte(tt.body))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []byte(tt.body), request.RequestJSON)
		})
	}
}

func TestGeminiDecisionRequestIsNeverStreamed(t *testing.T) {
	request, err := NewGeminiDecisionRequest([]byte(`{"model":"m","input":"x","stream":true}`))
	require.NoError(t, err)
	require.False(t, request.IsStream(nil))
}

func TestGeminiDecisionSetModelNameKeepsEveryOtherByte(t *testing.T) {
	request, err := NewGeminiDecisionRequest([]byte(geminiDecisionOrderedBody))
	require.NoError(t, err)

	request.SetModelName("gemini-decision-1")
	require.Equal(t, geminiDecisionOrderedBody, string(request.RequestJSON))

	request.SetModelName("")
	require.Equal(t, geminiDecisionOrderedBody, string(request.RequestJSON))

	request.SetModelName("gemini-decision-2")
	require.Equal(t, strings.Replace(geminiDecisionOrderedBody, `"gemini-decision-1"`, `"gemini-decision-2"`, 1), string(request.RequestJSON))
	require.Equal(t, "gemini-decision-2", request.Model)
}

func TestGeminiDecisionRequestMarshalJSONIsTheOriginalText(t *testing.T) {
	request, err := NewGeminiDecisionRequest([]byte(geminiDecisionOrderedBody))
	require.NoError(t, err)
	got, err := request.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, geminiDecisionOrderedBody, string(got))
}

func TestGeminiDecisionGetTokenCountMeta(t *testing.T) {
	t.Run("string input and question text", func(t *testing.T) {
		request, err := NewGeminiDecisionRequest([]byte(`{
			"model":"m",
			"input":"The mug arrived cracked.",
			"questions":{
				"damaged":{"type":"noul","instructions":"Is it damaged?","criteria":{"true_description":"Broken.","false_description":"Fine."}},
				"queue":{"type":"choice","instructions":"Which queue?","criteria":{"billing":"Payments.","other":""}},
				"severity":{"type":"score","instructions":"How severe?","criteria":["Cosmetic.","Blocked."]}
			}
		}`))
		require.NoError(t, err)

		meta := request.GetTokenCountMeta()
		require.Equal(t, types.TokenTypeTokenizer, meta.TokenType)
		require.Empty(t, meta.Files)
		for _, want := range []string{
			"The mug arrived cracked.",
			"Is it damaged?", "Broken.", "Fine.",
			"Which queue?", "Payments.",
			"How severe?", "Cosmetic.", "Blocked.",
		} {
			require.Contains(t, meta.CombineText, want)
		}
	})

	t.Run("content blocks and media", func(t *testing.T) {
		request, err := NewGeminiDecisionRequest([]byte(`{
			"model":"m",
			"input":[
				{"type":"text","text":"Return request #8821."},
				{"type":"image","data":"aGVsbG8=","mime_type":"image/jpeg"},
				{"type":"audio","uri":"https://example.com/clip.mp3","mime_type":"audio/mp3"},
				{"type":"unknown","text":"ignored"}
			],
			"questions":{"q":{"type":"noul","instructions":[{"type":"text","text":"Is the photo damaged?"}]}}
		}`))
		require.NoError(t, err)

		meta := request.GetTokenCountMeta()
		require.Contains(t, meta.CombineText, "Return request #8821.")
		require.Contains(t, meta.CombineText, "Is the photo damaged?")
		require.NotContains(t, meta.CombineText, "ignored")
		require.Len(t, meta.Files, 2)
		require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
		require.Equal(t, types.FileTypeAudio, meta.Files[1].FileType)
	})

	t.Run("single content object", func(t *testing.T) {
		request, err := NewGeminiDecisionRequest([]byte(`{"model":"m","input":{"type":"text","text":"solo"},"questions":{}}`))
		require.NoError(t, err)
		require.Equal(t, "solo", request.GetTokenCountMeta().CombineText)
	})
}

func newGeminiDecisionContext(t *testing.T, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/decisions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func TestParseGeminiDecisionRequest(t *testing.T) {
	t.Run("valid body is kept verbatim", func(t *testing.T) {
		c := newGeminiDecisionContext(t, []byte(geminiDecisionOrderedBody))
		request, err := ParseGeminiDecisionRequest(c)
		require.NoError(t, err)
		require.Equal(t, "gemini-decision-1", request.Model)
		require.Equal(t, geminiDecisionOrderedBody, string(request.RequestJSON))
	})

	t.Run("body can be parsed twice", func(t *testing.T) {
		c := newGeminiDecisionContext(t, []byte(geminiDecisionOrderedBody))
		_, err := ParseGeminiDecisionRequest(c)
		require.NoError(t, err)
		again, err := ParseGeminiDecisionRequest(c)
		require.NoError(t, err)
		require.Equal(t, geminiDecisionOrderedBody, string(again.RequestJSON))
	})

	t.Run("invalid body is rejected", func(t *testing.T) {
		c := newGeminiDecisionContext(t, []byte(`{"input":"x"}`))
		_, err := ParseGeminiDecisionRequest(c)
		require.ErrorContains(t, err, "model is required")
	})
}
