package gemini

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type embeddingHandlerFunc func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError)

func runGeminiEmbeddingHandler(t *testing.T, handler embeddingHandlerFunc, batch bool, estimate int, body string) (*dto.Usage, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-embedding-2"},
	}
	info.IsGeminiBatchEmbedding = batch
	info.SetEstimatePromptTokens(estimate)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}
	usage, apiErr := handler(c, info, resp)
	require.Nil(t, apiErr)
	return usage, recorder
}

func nativeEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	return NativeGeminiEmbeddingHandler(c, resp, info)
}

func TestGeminiEmbeddingHandlerBillsReportedTokens(t *testing.T) {
	const body = `{"embeddings":[{"values":[0.5,0.25]}],"usageMetadata":{"promptTokenCount":300,"promptTokenDetails":[{"modality":"AUDIO","tokenCount":100},{"modality":"TEXT","tokenCount":200}]}}`

	usage, recorder := runGeminiEmbeddingHandler(t, GeminiEmbeddingHandler, true, 7, body)

	require.Equal(t, 300, usage.PromptTokens)
	require.Equal(t, 300, usage.TotalTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Equal(t, 100, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.TextTokens)

	var openAI dto.OpenAIEmbeddingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &openAI))
	require.Equal(t, 300, openAI.Usage.PromptTokens)
	require.Len(t, openAI.Data, 1)
}

func TestGeminiEmbeddingHandlerFallsBackToEstimate(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "no usage metadata", body: `{"embeddings":[{"values":[0.5]}]}`},
		{name: "zero prompt tokens", body: `{"embeddings":[{"values":[0.5]}],"usageMetadata":{"promptTokenCount":0}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage, _ := runGeminiEmbeddingHandler(t, GeminiEmbeddingHandler, true, 7, tc.body)
			require.Equal(t, 7, usage.PromptTokens)
			require.Equal(t, 7, usage.TotalTokens)
		})
	}
}

func TestNativeGeminiEmbeddingHandlerBillsReportedTokens(t *testing.T) {
	tests := []struct {
		name  string
		batch bool
		body  string
	}{
		{name: "single", batch: false, body: `{"embedding":{"values":[0.5]},"usageMetadata":{"promptTokenCount":258}}`},
		{name: "batch", batch: true, body: `{"embeddings":[{"values":[0.5]}],"usageMetadata":{"promptTokenCount":258}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage, recorder := runGeminiEmbeddingHandler(t, nativeEmbeddingHandler, tc.batch, 7, tc.body)

			require.Equal(t, 258, usage.PromptTokens)
			require.Equal(t, 258, usage.TotalTokens)
			require.JSONEq(t, tc.body, recorder.Body.String())
		})
	}
}

func TestNativeGeminiEmbeddingHandlerFallsBackToEstimate(t *testing.T) {
	usage, recorder := runGeminiEmbeddingHandler(t, nativeEmbeddingHandler, false, 7, `{"embedding":{"values":[0.5]}}`)

	require.Equal(t, 7, usage.PromptTokens)
	require.JSONEq(t, `{"embedding":{"values":[0.5]}}`, recorder.Body.String())
}
