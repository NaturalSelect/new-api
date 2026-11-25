package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modelcatalog"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const testCatalogBody = `{"data":[
	{
		"id":"zz/zz-priced-ratio-model",
		"name":"ZZ: Ratio Model",
		"created":1795562304,
		"description":"a catalog model",
		"context_length":200000,
		"knowledge_cutoff":"2026-01-31",
		"hugging_face_id":"zz/ratio",
		"architecture":{"modality":"text+image->text","input_modalities":["text","image"],"output_modalities":["text"],"tokenizer":"Claude"},
		"top_provider":{"context_length":190000,"max_completion_tokens":64000},
		"supported_parameters":["tools","reasoning"]
	},
	{
		"id":"openai/gpt-4o",
		"name":"OpenAI: GPT-4o",
		"created":1715558400,
		"context_length":128000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"top_provider":{"max_completion_tokens":16384}
	}
]}`

func withModelCatalog(t *testing.T) {
	t.Helper()

	snap, err := modelcatalog.Parse([]byte(testCatalogBody))
	require.NoError(t, err)
	modelcatalog.Set(snap)
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		modelcatalog.Clear()
		model.InvalidatePricingCache()
	})
}

func TestListModelsIncludesCatalogMetadata(t *testing.T) {
	pricingSetupForListModels(t)
	withModelCatalog(t)

	_, byId := listModelsPayload(t, constant.ChannelTypeOpenAI, "")

	matched, ok := byId["zz-priced-ratio-model"]
	require.True(t, ok)
	require.Equal(t, 1795562304, matched.Created)
	require.Equal(t, "ZZ: Ratio Model", matched.Name)
	require.Equal(t, "a catalog model", matched.Description)
	require.Equal(t, 200000, matched.ContextLength)
	require.Equal(t, "2026-01-31", matched.KnowledgeCutoff)
	require.Equal(t, "zz/ratio", matched.HuggingFaceID)
	require.Equal(t, []string{"tools", "reasoning"}, matched.SupportedParameters)
	require.NotNil(t, matched.Architecture)
	require.Equal(t, "text+image->text", matched.Architecture.Modality)
	require.Equal(t, []string{"text", "image"}, matched.Architecture.InputModalities)
	require.Equal(t, []string{"text"}, matched.Architecture.OutputModalities)
	require.Equal(t, "Claude", matched.Architecture.Tokenizer)
	require.NotNil(t, matched.TopProvider)
	require.Equal(t, 190000, matched.TopProvider.ContextLength)
	require.Equal(t, 64000, matched.TopProvider.MaxCompletionTokens)
	require.NotNil(t, matched.Pricing, "existing pricing output is kept")

	unmatched, ok := byId["zz-priced-tiered-model"]
	require.True(t, ok)
	require.Equal(t, 1626777600, unmatched.Created)
	require.Zero(t, unmatched.ContextLength)
	require.Nil(t, unmatched.Architecture)
	require.Nil(t, unmatched.TopProvider)
	require.Empty(t, unmatched.Name)
}

func TestListModelsWithoutCatalogKeepsLegacyShape(t *testing.T) {
	pricingSetupForListModels(t)
	modelcatalog.Clear()

	recorder, byId := listModelsPayload(t, constant.ChannelTypeOpenAI, "")

	require.Equal(t, 1626777600, byId["zz-priced-ratio-model"].Created)
	require.NotContains(t, recorder.Body.String(), "context_length")
	require.NotContains(t, recorder.Body.String(), "architecture")
	require.NotContains(t, recorder.Body.String(), "top_provider")
}

func TestListModelsAnthropicFormatUsesCatalogCreatedAt(t *testing.T) {
	pricingSetupForListModels(t)
	withModelCatalog(t)

	recorder, _ := listModelsPayload(t, constant.ChannelTypeAnthropic, "")

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	createdAt := map[string]string{}
	for _, item := range payload.Data {
		createdAt[item.ID] = item.CreatedAt
	}
	require.Equal(t, "2026-11-24T23:18:24Z", createdAt["zz-priced-ratio-model"])
	require.Equal(t, "2021-07-20T10:40:00Z", createdAt["zz-priced-tiered-model"])
}

func TestListModelsGeminiFormatFillsTokenLimits(t *testing.T) {
	pricingSetupForListModels(t)
	withModelCatalog(t)

	recorder, _ := listModelsPayload(t, constant.ChannelTypeGemini, "")

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	byName := map[string]map[string]any{}
	for _, item := range payload.Models {
		byName[item["name"].(string)] = item
	}

	matched := byName["zz-priced-ratio-model"]
	require.EqualValues(t, 200000, matched["inputTokenLimit"])
	require.EqualValues(t, 64000, matched["outputTokenLimit"])
	require.Equal(t, "a catalog model", matched["description"])
	require.Equal(t, "zz-priced-ratio-model", matched["displayName"])

	unmatched := byName["zz-priced-tiered-model"]
	require.Nil(t, unmatched["inputTokenLimit"])
	require.Nil(t, unmatched["outputTokenLimit"])
	require.Nil(t, unmatched["description"])
}

func retrieveModelPayload(t *testing.T, modelID string, modelType int) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models/"+modelID, nil)
	ctx.Params = gin.Params{{Key: "model", Value: modelID}}
	RetrieveModel(ctx, modelType)
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder
}

func TestRetrieveModelIncludesCatalogMetadataWithoutMutatingStaticModels(t *testing.T) {
	_, known := openAIModelsMap["gpt-4o"]
	require.True(t, known, "test relies on gpt-4o being a built-in model")
	withModelCatalog(t)

	recorder := retrieveModelPayload(t, "gpt-4o", constant.ChannelTypeOpenAI)
	var got map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &got))
	require.EqualValues(t, 128000, got["context_length"])
	require.EqualValues(t, 1715558400, got["created"])
	require.Equal(t, "OpenAI: GPT-4o", got["name"])

	anthropic := retrieveModelPayload(t, "gpt-4o", constant.ChannelTypeAnthropic)
	require.Contains(t, anthropic.Body.String(), `"created_at":"2024-05-13T00:00:00Z"`)

	modelcatalog.Clear()
	recorder = retrieveModelPayload(t, "gpt-4o", constant.ChannelTypeOpenAI)
	require.NotContains(t, recorder.Body.String(), "context_length")
	require.Equal(t, 1626777600, openAIModelsMap["gpt-4o"].Created)
}

func TestGetPricingIncludesCatalogMetadata(t *testing.T) {
	pricingSetupForListModels(t)
	withModelCatalog(t)

	byName := pricingByModelName(model.GetPricing())

	matched, ok := byName["zz-priced-ratio-model"]
	require.True(t, ok)
	require.Equal(t, 200000, matched.ContextLength)
	require.Equal(t, 64000, matched.MaxOutputTokens)
	require.Equal(t, []string{"text", "image"}, matched.InputModalities)
	require.Equal(t, []string{"text"}, matched.OutputModalities)
	require.Equal(t, "2026-01-31", matched.KnowledgeCutoff)
	require.Equal(t, "2026-11-24", matched.ReleaseDate)
	require.Equal(t, []string{"function_calling", "tools", "reasoning", "vision"}, matched.Capabilities)
	require.Empty(t, matched.Description, "catalog must not overwrite admin-managed descriptions")

	unmatched, ok := byName["zz-priced-tiered-model"]
	require.True(t, ok)
	encoded, err := common.Marshal(unmatched)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "context_length")
	require.NotContains(t, string(encoded), "release_date")
	require.NotContains(t, string(encoded), "capabilities")
}

func TestGetPricingDropsCatalogMetadataWhenCatalogCleared(t *testing.T) {
	pricingSetupForListModels(t)
	withModelCatalog(t)
	require.NotZero(t, pricingByModelName(model.GetPricing())["zz-priced-ratio-model"].ContextLength)

	modelcatalog.Clear()
	model.InvalidatePricingCache()

	require.Zero(t, pricingByModelName(model.GetPricing())["zz-priced-ratio-model"].ContextLength)
}
