package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func withRatioSettings(t *testing.T, modelRatio, completionRatio, cacheRatio, modelPrice string) {
	t.Helper()

	type setting struct {
		current func() string
		update  func(string) error
	}
	settings := []struct {
		setting
		value string
	}{
		{setting{ratio_setting.ModelRatio2JSONString, ratio_setting.UpdateModelRatioByJSONString}, modelRatio},
		{setting{ratio_setting.CompletionRatio2JSONString, ratio_setting.UpdateCompletionRatioByJSONString}, completionRatio},
		{setting{ratio_setting.CacheRatio2JSONString, ratio_setting.UpdateCacheRatioByJSONString}, cacheRatio},
		{setting{ratio_setting.ModelPrice2JSONString, ratio_setting.UpdateModelPriceByJSONString}, modelPrice},
	}
	for _, s := range settings {
		original := s.current()
		update := s.update
		t.Cleanup(func() { require.NoError(t, update(original)) })
		require.NoError(t, s.update(s.value))
	}
}

func withPricingEnvironment(t *testing.T) {
	t.Helper()

	originalQuotaPerUnit := common.QuotaPerUnit
	originalSelfUse := operation_setting.SelfUseModeEnabled
	common.QuotaPerUnit = 500000
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.SelfUseModeEnabled = originalSelfUse
	})
}

func withTieredExprConfig(t *testing.T, exprs map[string]string) {
	t.Helper()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })

	modes := make(map[string]string, len(exprs))
	for name := range exprs {
		modes[name] = "tiered_expr"
	}
	modeBytes, err := common.Marshal(modes)
	require.NoError(t, err)
	exprBytes, err := common.Marshal(exprs)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeBytes),
		"billing_setting.billing_expr": string(exprBytes),
	}))
}

func TestBuildOpenRouterPricing_Ratio(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t,
		`{"zz-ratio-model":1.25}`,
		`{"zz-ratio-model":8}`,
		`{"zz-ratio-model":0.1}`,
		`{}`,
	)

	pricing := BuildOpenRouterPricing("zz-ratio-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0.0000025", pricing.Prompt)
	require.Equal(t, "0.00002", pricing.Completion)
	require.Equal(t, "0.00000025", pricing.InputCacheRead)
	require.Equal(t, "0", pricing.Request)
	require.Empty(t, pricing.InputCacheWrite)
	require.Empty(t, pricing.InputCacheWrite1h)
	require.Empty(t, pricing.ImageToken)
	require.Empty(t, pricing.Audio)
	require.Empty(t, pricing.AudioOutput)
}

func TestBuildOpenRouterPricing_RatioAppliesGroupRatio(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{"zz-ratio-model":1.25}`, `{"zz-ratio-model":8}`, `{}`, `{}`)

	pricing := BuildOpenRouterPricing("zz-ratio-model", 0.5)
	require.NotNil(t, pricing)
	require.Equal(t, "0.00000125", pricing.Prompt)
	require.Equal(t, "0.00001", pricing.Completion)
}

func TestBuildOpenRouterPricing_FixedPrice(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{}`, `{}`, `{}`, `{"zz-fixed-model":0.04}`)

	pricing := BuildOpenRouterPricing("zz-fixed-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0", pricing.Prompt)
	require.Equal(t, "0", pricing.Completion)
	require.Equal(t, "0.04", pricing.Request)

	pricing = BuildOpenRouterPricing("zz-fixed-model", 2)
	require.NotNil(t, pricing)
	require.Equal(t, "0.08", pricing.Request)
}

func TestBuildOpenRouterPricing_TieredExpr(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{}`, `{}`, `{}`, `{}`)
	withTieredExprConfig(t, map[string]string{
		"zz-tiered-model": `tier("base", p * 3 + c * 15 + cc1h * 6)`,
	})

	pricing := BuildOpenRouterPricing("zz-tiered-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0.000003", pricing.Prompt)
	require.Equal(t, "0.000015", pricing.Completion)
	require.Equal(t, "0.000006", pricing.InputCacheWrite1h)
	require.Equal(t, "0", pricing.Request)
	require.Empty(t, pricing.InputCacheRead)
	require.Empty(t, pricing.InputCacheWrite)

	pricing = BuildOpenRouterPricing("zz-tiered-model", 2)
	require.NotNil(t, pricing)
	require.Equal(t, "0.000006", pricing.Prompt)
}

func TestBuildOpenRouterPricing_TieredExprConstantTermBecomesRequestPrice(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{}`, `{}`, `{}`, `{}`)
	withTieredExprConfig(t, map[string]string{
		"zz-tiered-model": `tier("base", p * 1 + 0.5)`,
	})

	pricing := BuildOpenRouterPricing("zz-tiered-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0.0000005", pricing.Request)
	require.Equal(t, "0.000001", pricing.Prompt)
	require.Equal(t, "0", pricing.Completion)
}

func TestBuildOpenRouterPricing_TieredExprTakesPrecedence(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{"zz-both-model":10}`, `{}`, `{}`, `{"zz-both-model":1}`)
	withTieredExprConfig(t, map[string]string{
		"zz-both-model": `tier("base", p * 3 + c * 15)`,
	})

	pricing := BuildOpenRouterPricing("zz-both-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0.000003", pricing.Prompt)
	require.Equal(t, "0", pricing.Request)
}

func TestBuildOpenRouterPricing_TieredModeWithEmptyExprFallsBack(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{"zz-empty-expr-model":1.25}`, `{}`, `{}`, `{}`)
	withTieredExprConfig(t, map[string]string{"zz-empty-expr-model": "   "})

	pricing := BuildOpenRouterPricing("zz-empty-expr-model", 1)
	require.NotNil(t, pricing)
	require.Equal(t, "0.0000025", pricing.Prompt)
}

func TestBuildOpenRouterPricing_Unconfigured(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{}`, `{}`, `{}`, `{}`)

	require.Nil(t, BuildOpenRouterPricing("zz-unconfigured-model", 1))
}

func TestBuildOpenRouterPricing_NonPositiveQuotaPerUnit(t *testing.T) {
	withPricingEnvironment(t)
	withRatioSettings(t, `{"zz-ratio-model":1.25}`, `{}`, `{}`, `{}`)
	common.QuotaPerUnit = 0

	require.Nil(t, BuildOpenRouterPricing("zz-ratio-model", 1))
}
