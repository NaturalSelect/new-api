package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const geminiEmbeddingTestPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func convertGeminiEmbeddingForTest(t *testing.T, body string) (any, error, *relaycommon.RelayInfo) {
	t.Helper()
	return convertGeminiEmbeddingForModel(t, "gemini-embedding-2-preview", body)
}

func convertGeminiEmbeddingForModel(t *testing.T, model string, body string) (any, error, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
	var req dto.EmbeddingRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))

	converted, err := (&Adaptor{}).ConvertEmbeddingRequest(c, info, req)
	return converted, err, info
}

func TestConvertEmbeddingRequestDimensions(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: "gemini-embedding-2", want: true},
		{model: "gemini-embedding-2-preview", want: true},
		{model: "gemini-embedding-001", want: true},
		{model: "gemini-embedding-exp-03-07", want: true},
		{model: "text-embedding-004", want: true},
		{model: "embedding-001", want: false},
		{model: "text-embedding-003", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			converted, err, _ := convertGeminiEmbeddingForModel(t, tc.model, `{"model":"m","input":"hello","dimensions":768}`)
			require.NoError(t, err)

			payload, err := common.Marshal(converted)
			require.NoError(t, err)
			if tc.want {
				require.Contains(t, string(payload), `"outputDimensionality":768`)
			} else {
				require.NotContains(t, string(payload), "outputDimensionality")
			}
		})
	}
}

func TestConvertEmbeddingRequestMultimodal(t *testing.T) {
	body := `{"model":"gemini-embedding-2-preview","input":[
		"plain text",
		["a dog",{"type":"image_url","image_url":{"url":"` + geminiEmbeddingTestPNG + `"}}],
		{"type":"image_url","image_url":{"url":"` + geminiEmbeddingTestPNG + `"}}
	]}`

	converted, err, info := convertGeminiEmbeddingForTest(t, body)
	require.NoError(t, err)
	require.True(t, info.IsGeminiBatchEmbedding)

	payload, err := common.Marshal(converted)
	require.NoError(t, err)

	const pngData = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	require.JSONEq(t, `{"requests":[
		{"model":"models/gemini-embedding-2-preview","content":{"parts":[{"text":"plain text"}]}},
		{"model":"models/gemini-embedding-2-preview","content":{"parts":[{"text":"a dog"},{"inlineData":{"mimeType":"image/png","data":"`+pngData+`"}}]}},
		{"model":"models/gemini-embedding-2-preview","content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"`+pngData+`"}}]}}
	]}`, string(payload))
}

func TestConvertEmbeddingRequestAudioPart(t *testing.T) {
	body := `{"model":"gemini-embedding-2-preview","input":[{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]}`

	converted, err, _ := convertGeminiEmbeddingForTest(t, body)
	require.NoError(t, err)

	payload, err := common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, `{"requests":[{"model":"models/gemini-embedding-2-preview","content":{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"UklGRg=="}}]}}]}`, string(payload))
}

func TestConvertEmbeddingRequestRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "token ids", body: `{"model":"m","input":[[1,2,3]]}`, want: "invalid input"},
		{name: "unknown part type", body: `{"model":"m","input":[{"type":"image","image":"x"}]}`, want: "unsupported content part type"},
		{name: "unsupported mime type", body: `{"model":"m","input":[{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"ogg"}}]}`, want: "mime type is not supported by Gemini"},
		{name: "empty array", body: `{"model":"m","input":[]}`, want: "input is empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err, _ := convertGeminiEmbeddingForTest(t, tc.body)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
