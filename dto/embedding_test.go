package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/require"
)

const embeddingTestPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func parseEmbeddingTestRequest(t *testing.T, body string) *EmbeddingRequest {
	t.Helper()
	var req EmbeddingRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	return &req
}

func TestEmbeddingParseMediaInputs(t *testing.T) {
	t.Run("string input", func(t *testing.T) {
		items, err := parseEmbeddingTestRequest(t, `{"model":"m","input":"hello"}`).ParseMediaInputs()
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, []MediaContent{{Type: ContentTypeText, Text: "hello"}}, items[0])
	})

	t.Run("each array element is its own embedding", func(t *testing.T) {
		body := `{"model":"m","input":["a",{"type":"image_url","image_url":{"url":"` + embeddingTestPNG + `"}}]}`
		items, err := parseEmbeddingTestRequest(t, body).ParseMediaInputs()
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.Equal(t, ContentTypeText, items[0][0].Type)
		require.Len(t, items[1], 1)
		require.Equal(t, ContentTypeImageURL, items[1][0].Type)
		require.Equal(t, embeddingTestPNG, items[1][0].GetImageMedia().Url)
	})

	t.Run("nested array is aggregated into one embedding", func(t *testing.T) {
		body := `{"model":"m","input":[["a dog",{"type":"image_url","image_url":{"url":"` + embeddingTestPNG + `"}},{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]]}`
		items, err := parseEmbeddingTestRequest(t, body).ParseMediaInputs()
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Len(t, items[0], 3)
		require.Equal(t, ContentTypeText, items[0][0].Type)
		require.Equal(t, ContentTypeImageURL, items[0][1].Type)
		require.Equal(t, ContentTypeInputAudio, items[0][2].Type)
	})

	t.Run("nil input", func(t *testing.T) {
		items, err := parseEmbeddingTestRequest(t, `{"model":"m"}`).ParseMediaInputs()
		require.NoError(t, err)
		require.Empty(t, items)
	})

	errorCases := []struct {
		name string
		body string
		want string
	}{
		{name: "token ids", body: `{"model":"m","input":[[1,2,3]]}`, want: "input[0]: part[0]: unsupported type"},
		{name: "number input", body: `{"model":"m","input":7}`, want: "unsupported input type"},
		{name: "unknown part type", body: `{"model":"m","input":[{"type":"image","image":"x"}]}`, want: `input[0]: unsupported content part type "image"`},
		{name: "image without url", body: `{"model":"m","input":[{"type":"image_url","image_url":{}}]}`, want: "input[0]: image_url content part has no data"},
		{name: "empty part list", body: `{"model":"m","input":[[]]}`, want: "input[0]: content part list is empty"},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEmbeddingTestRequest(t, tc.body).ParseMediaInputs()
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestEmbeddingTokenCountMetaIncludesMedia(t *testing.T) {
	body := `{"model":"m","input":["hello",["a dog",{"type":"image_url","image_url":{"url":"` + embeddingTestPNG + `"}},{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]]}`
	meta := parseEmbeddingTestRequest(t, body).GetTokenCountMeta()

	require.Equal(t, "hello\na dog", meta.CombineText)
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, types.FileTypeAudio, meta.Files[1].FileType)
}

func TestEmbeddingTokenCountMetaKeepsTextForUnparsableInput(t *testing.T) {
	meta := parseEmbeddingTestRequest(t, `{"model":"m","input":["hello",[1,2,3]]}`).GetTokenCountMeta()

	require.Equal(t, "hello", meta.CombineText)
	require.Empty(t, meta.Files)
}

func TestGeminiEmbeddingTokenCountMetaIncludesInlineData(t *testing.T) {
	const part = `{"inline_data":{"mime_type":"image/png","data":"AAAA"}}`

	var single GeminiEmbeddingRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"content":{"parts":[{"text":"a dog"},`+part+`]}}`, &single))
	meta := single.GetTokenCountMeta()
	require.Equal(t, "a dog", meta.CombineText)
	require.Len(t, meta.Files, 1)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)

	var batch GeminiBatchEmbeddingRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"requests":[{"content":{"parts":[{"text":"a dog"}]}},{"content":{"parts":[`+part+`]}}]}`, &batch))
	batchMeta := batch.GetTokenCountMeta()
	require.Equal(t, "a dog", batchMeta.CombineText)
	require.Len(t, batchMeta.Files, 1)
	require.Equal(t, types.FileTypeImage, batchMeta.Files[0].FileType)
}

func TestGeminiEmbeddingRequestAcceptsSnakeCaseFields(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "snake case",
			body: `{"content":{"parts":[{"text":"x"}]},"task_type":"RETRIEVAL_QUERY","output_dimensionality":768}`,
			want: `{"content":{"parts":[{"text":"x"}]},"taskType":"RETRIEVAL_QUERY","outputDimensionality":768}`,
		},
		{
			name: "camel case",
			body: `{"content":{"parts":[{"text":"x"}]},"taskType":"CLASSIFICATION","outputDimensionality":256}`,
			want: `{"content":{"parts":[{"text":"x"}]},"taskType":"CLASSIFICATION","outputDimensionality":256}`,
		},
		{
			name: "explicit zero is forwarded",
			body: `{"content":{"parts":[{"text":"x"}]},"output_dimensionality":0}`,
			want: `{"content":{"parts":[{"text":"x"}]},"outputDimensionality":0}`,
		},
		{
			name: "absent is omitted",
			body: `{"content":{"parts":[{"text":"x"}]}}`,
			want: `{"content":{"parts":[{"text":"x"}]}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req GeminiEmbeddingRequest
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &req))
			out, err := common.Marshal(req)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(out))
		})
	}
}

func TestGeminiPartAcceptsSnakeCaseFileData(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "snake case", body: `{"file_data":{"mime_type":"image/png","file_uri":"https://generativelanguage.googleapis.com/v1beta/files/abc"}}`},
		{name: "camel case", body: `{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/abc"}}`},
		{name: "snake part with camel fields", body: `{"file_data":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/abc"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var part GeminiPart
			require.NoError(t, common.UnmarshalJsonStr(tc.body, &part))
			require.NotNil(t, part.FileData)
			require.Equal(t, "image/png", part.FileData.MimeType)
			require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/files/abc", part.FileData.FileUri)

			out, err := common.Marshal(part)
			require.NoError(t, err)
			require.JSONEq(t, `{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/abc"}}`, string(out))
		})
	}
}

func TestGeminiEmbeddingResponseParsesUsageMetadata(t *testing.T) {
	const usage = `"usageMetadata":{"promptTokenCount":300,"promptTokenDetails":[{"modality":"AUDIO","tokenCount":100},{"modality":"TEXT","tokenCount":200}]}`

	var single GeminiEmbeddingResponse
	require.NoError(t, common.UnmarshalJsonStr(`{"embedding":{"values":[0.1]},`+usage+`}`, &single))
	require.NotNil(t, single.UsageMetadata)
	require.Equal(t, 300, single.UsageMetadata.PromptTokenCount)

	var batch GeminiBatchEmbeddingResponse
	require.NoError(t, common.UnmarshalJsonStr(`{"embeddings":[{"values":[0.1]}],`+usage+`}`, &batch))
	require.NotNil(t, batch.UsageMetadata)
	require.Equal(t, 300, batch.UsageMetadata.ToUsageMetadata().PromptTokenCount)
	require.Equal(t, []GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 100}, {Modality: "TEXT", TokenCount: 200}}, batch.UsageMetadata.ToUsageMetadata().PromptTokensDetails)

	var plural GeminiBatchEmbeddingResponse
	require.NoError(t, common.UnmarshalJsonStr(`{"embeddings":[],"usageMetadata":{"promptTokenCount":5,"promptTokensDetails":[{"modality":"TEXT","tokenCount":5}]}}`, &plural))
	require.Equal(t, []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 5}}, plural.UsageMetadata.ToUsageMetadata().PromptTokensDetails)

	var none GeminiBatchEmbeddingResponse
	require.NoError(t, common.UnmarshalJsonStr(`{"embeddings":[]}`, &none))
	require.Nil(t, none.UsageMetadata)
}
