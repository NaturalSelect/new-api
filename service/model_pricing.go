package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
)

const (
	usdDecimalPlaces      = 12
	quotaPerUnitPrecision = 18
	// NOTE: Expression coefficients are $/1M tokens, so shifting by 6 digits yields USD per token.
	exprUnitShift = -6
)

// NOTE: Returns nil when the model has no billing config. Branch order mirrors relay/helper.ModelPriceHelper,
// which service cannot import.
func BuildOpenRouterPricing(modelName string, groupRatio float64) *dto.OpenRouterPricing {
	if common.QuotaPerUnit <= 0 {
		return nil
	}
	ratio := decimal.NewFromFloat(groupRatio)

	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		if expr, ok := billing_setting.GetBillingExpr(modelName); ok && strings.TrimSpace(expr) != "" {
			pricing, err := tieredExprPricing(expr, ratio)
			if err != nil {
				common.SysLog("build model list pricing for " + modelName + ": " + err.Error())
				return nil
			}
			return pricing
		}
	}
	if price, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return fixedPricePricing(price, ratio)
	}
	if modelRatio, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return ratioPricing(modelName, modelRatio, ratio)
	}
	return nil
}

func formatUSD(value decimal.Decimal) string {
	return value.Round(usdDecimalPlaces).String()
}

func fixedPricePricing(price float64, groupRatio decimal.Decimal) *dto.OpenRouterPricing {
	return &dto.OpenRouterPricing{
		Prompt:     "0",
		Completion: "0",
		Request:    formatUSD(decimal.NewFromFloat(price).Mul(groupRatio)),
	}
}

func ratioPricing(modelName string, modelRatio float64, groupRatio decimal.Decimal) *dto.OpenRouterPricing {
	unit := decimal.NewFromFloat(modelRatio).
		Mul(groupRatio).
		DivRound(decimal.NewFromFloat(common.QuotaPerUnit), quotaPerUnitPrecision)
	scaled := func(multiplier float64) string {
		return formatUSD(unit.Mul(decimal.NewFromFloat(multiplier)))
	}

	pricing := &dto.OpenRouterPricing{
		Prompt:     formatUSD(unit),
		Completion: scaled(ratio_setting.GetCompletionRatio(modelName)),
		Request:    "0",
	}
	if cacheRatio, ok := ratio_setting.GetCacheRatio(modelName); ok {
		pricing.InputCacheRead = scaled(cacheRatio)
	}
	if createCacheRatio, ok := ratio_setting.GetCreateCacheRatio(modelName); ok {
		pricing.InputCacheWrite = scaled(createCacheRatio)
	}
	if imageRatio, ok := ratio_setting.GetImageRatio(modelName); ok {
		pricing.ImageToken = scaled(imageRatio)
	}
	if ratio_setting.ContainsAudioRatio(modelName) {
		audioRatio := ratio_setting.GetAudioRatio(modelName)
		pricing.Audio = scaled(audioRatio)
		if ratio_setting.ContainsAudioCompletionRatio(modelName) {
			pricing.AudioOutput = scaled(audioRatio * ratio_setting.GetAudioCompletionRatio(modelName))
		}
	}
	return pricing
}

func tieredExprPricing(expr string, groupRatio decimal.Decimal) (*dto.OpenRouterPricing, error) {
	probed, err := billingexpr.ProbeUnitPrices(expr)
	if err != nil {
		return nil, err
	}
	usd := func(exprValue float64) string {
		return formatUSD(decimal.NewFromFloat(exprValue).Mul(groupRatio).Shift(exprUnitShift))
	}
	optional := func(variable string) string {
		if coefficient, ok := probed.Tokens[variable]; ok {
			return usd(coefficient)
		}
		return ""
	}

	return &dto.OpenRouterPricing{
		Prompt:            usd(probed.Tokens["p"]),
		Completion:        usd(probed.Tokens["c"]),
		Request:           usd(probed.Request),
		InputCacheRead:    optional("cr"),
		InputCacheWrite:   optional("cc"),
		InputCacheWrite1h: optional("cc1h"),
		ImageToken:        optional("img"),
		Audio:             optional("ai"),
		AudioOutput:       optional("ao"),
	}, nil
}
