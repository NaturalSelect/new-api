package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type billingQuota struct {
	total       int
	used        int
	expiredTime int64
}

type quotaConstraint struct {
	used      int
	remaining int
}

// resolveBillingQuota reports the key's remaining allowance when the key has a quota cap or a
// rolling window cap, and the account balance otherwise.
func resolveBillingQuota(c *gin.Context) (billingQuota, error) {
	userId := c.GetInt("id")
	if !common.DisplayTokenStatEnabled {
		return resolveAccountBillingQuota(userId)
	}
	token, err := model.GetTokenById(c.GetInt("token_id"))
	if err != nil {
		return billingQuota{}, err
	}
	quota, limited, err := resolveTokenBillingQuota(token)
	if err != nil {
		return billingQuota{}, err
	}
	if !limited {
		if quota, err = resolveAccountBillingQuota(userId); err != nil {
			return billingQuota{}, err
		}
	}
	quota.expiredTime = max(token.ExpiredTime, 0)
	return quota, nil
}

func resolveAccountBillingQuota(userId int) (billingQuota, error) {
	remain, err := model.GetUserQuota(userId, false)
	if err != nil {
		return billingQuota{}, err
	}
	used, err := model.GetUserUsedQuota(userId)
	if err != nil {
		return billingQuota{}, err
	}
	return billingQuota{total: remain + used, used: used}, nil
}

// resolveTokenBillingQuota picks the tightest of the key's own caps. limited is false when the
// key has neither a quota cap nor a rolling window cap.
func resolveTokenBillingQuota(token *model.Token) (quota billingQuota, limited bool, err error) {
	constraints := make([]quotaConstraint, 0, 3)
	if !token.UnlimitedQuota {
		constraints = append(constraints, quotaConstraint{used: token.UsedQuota, remaining: token.RemainQuota})
	}
	windowLimits := []struct {
		window model.TokenQuotaWindow
		limit  int
	}{
		{model.TokenQuotaWindow5h, token.QuotaLimit5h},
		{model.TokenQuotaWindow7d, token.QuotaLimit7d},
	}
	for _, item := range windowLimits {
		if item.limit <= 0 {
			continue
		}
		usage, err := model.GetTokenWindowUsage(token.Id, item.window)
		if err != nil {
			return billingQuota{}, false, err
		}
		used := int(usage.Used)
		constraints = append(constraints, quotaConstraint{used: used, remaining: item.limit - used})
	}
	if len(constraints) == 0 {
		return billingQuota{}, false, nil
	}
	tightest := constraints[0]
	for _, constraint := range constraints[1:] {
		if constraint.remaining < tightest.remaining {
			tightest = constraint
		}
	}
	// NOTE: Clients compute balance as hard limit minus usage, so total is rebuilt as used plus remaining.
	return billingQuota{total: tightest.used + max(tightest.remaining, 0), used: tightest.used}, true, nil
}

func GetSubscription(c *gin.Context) {
	quota, err := resolveBillingQuota(c)
	if err != nil {
		openAIError := types.OpenAIError{
			Message: err.Error(),
			Type:    "upstream_error",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	amount := float64(quota.total)
	// OpenAI 兼容接口中的 *_USD 字段含义保持“额度单位”对应值：
	// 我们将其解释为以“站点展示类型”为准：
	// - USD: 直接除以 QuotaPerUnit
	// - CNY: 先转 USD 再乘汇率
	// - TOKENS: 直接使用 tokens 数量
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		amount = amount / common.QuotaPerUnit * operation_setting.USDExchangeRate
	case operation_setting.QuotaDisplayTypeTokens:
		// amount 保持 tokens 数值
	default:
		amount = amount / common.QuotaPerUnit
	}
	subscription := OpenAISubscriptionResponse{
		Object:             "billing_subscription",
		HasPaymentMethod:   true,
		SoftLimitUSD:       amount,
		HardLimitUSD:       amount,
		SystemHardLimitUSD: amount,
		AccessUntil:        quota.expiredTime,
	}
	c.JSON(200, subscription)
	return
}

func GetUsage(c *gin.Context) {
	quota, err := resolveBillingQuota(c)
	if err != nil {
		openAIError := types.OpenAIError{
			Message: err.Error(),
			Type:    "new_api_error",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
		return
	}
	amount := float64(quota.used)
	switch operation_setting.GetQuotaDisplayType() {
	case operation_setting.QuotaDisplayTypeCNY:
		amount = amount / common.QuotaPerUnit * operation_setting.USDExchangeRate
	case operation_setting.QuotaDisplayTypeTokens:
		// tokens 保持原值
	default:
		amount = amount / common.QuotaPerUnit
	}
	usage := OpenAIUsageResponse{
		Object:     "list",
		TotalUsage: amount * 100,
	}
	c.JSON(200, usage)
	return
}
