package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	billingTestUserQuota = 8000
	billingTestUserUsed  = 2000
)

func setupBillingTest(t *testing.T) *gorm.DB {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.User{}, &model.TokenWindowBucket{}))

	originalDisplay := common.DisplayTokenStatEnabled
	originalBatch := common.BatchUpdateEnabled
	common.DisplayTokenStatEnabled = true
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.DisplayTokenStatEnabled = originalDisplay
		common.BatchUpdateEnabled = originalBatch
	})

	user := &model.User{Username: "billing_user", Quota: billingTestUserQuota, UsedQuota: billingTestUserUsed}
	require.NoError(t, db.Create(user).Error)
	return db
}

func seedBillingToken(t *testing.T, db *gorm.DB, key string, mutate func(token *model.Token)) *model.Token {
	t.Helper()

	token := seedToken(t, db, 1, key, key)
	token.UnlimitedQuota = false
	token.RemainQuota = 0
	mutate(token)
	require.NoError(t, db.Save(token).Error)
	t.Cleanup(func() {
		_ = model.ResetTokenWindowUsage(token.Id, model.TokenQuotaWindows())
	})
	return token
}

func fetchBilling(t *testing.T, token *model.Token) (OpenAISubscriptionResponse, OpenAIUsageResponse) {
	t.Helper()

	subCtx, subRecorder := newAuthenticatedContext(t, "GET", "/v1/dashboard/billing/subscription", nil, token.UserId)
	subCtx.Set("token_id", token.Id)
	GetSubscription(subCtx)
	var subscription OpenAISubscriptionResponse
	require.NoError(t, common.Unmarshal(subRecorder.Body.Bytes(), &subscription))

	usageCtx, usageRecorder := newAuthenticatedContext(t, "GET", "/v1/dashboard/billing/usage", nil, token.UserId)
	usageCtx.Set("token_id", token.Id)
	GetUsage(usageCtx)
	var usage OpenAIUsageResponse
	require.NoError(t, common.Unmarshal(usageRecorder.Body.Bytes(), &usage))
	return subscription, usage
}

func billingUSD(quota int) float64 {
	return float64(quota) / common.QuotaPerUnit
}

func TestBillingReturnsTokenRemainWhenTokenHasQuota(t *testing.T) {
	db := setupBillingTest(t)
	token := seedBillingToken(t, db, "quota-key", func(token *model.Token) {
		token.RemainQuota = 300
		token.UsedQuota = 700
	})

	subscription, usage := fetchBilling(t, token)

	require.InDelta(t, billingUSD(1000), subscription.HardLimitUSD, 1e-9)
	require.InDelta(t, billingUSD(700)*100, usage.TotalUsage, 1e-9)
	require.InDelta(t, 300.0/common.QuotaPerUnit, subscription.HardLimitUSD-usage.TotalUsage/100, 1e-9)
}

func TestBillingReturnsRollingWindowRemainWhenTokenIsUnlimited(t *testing.T) {
	db := setupBillingTest(t)
	token := seedBillingToken(t, db, "window-key", func(token *model.Token) {
		token.UnlimitedQuota = true
		token.QuotaLimit5h = 1000
	})
	require.NoError(t, model.DecreaseTokenQuota(token.Id, token.Key, 400))

	subscription, usage := fetchBilling(t, token)

	require.InDelta(t, billingUSD(1000), subscription.HardLimitUSD, 1e-9)
	require.InDelta(t, billingUSD(400)*100, usage.TotalUsage, 1e-9)
	require.InDelta(t, billingUSD(600), subscription.HardLimitUSD-usage.TotalUsage/100, 1e-9)
}

func TestBillingPicksTightestOfQuotaAndWindows(t *testing.T) {
	db := setupBillingTest(t)
	token := seedBillingToken(t, db, "tight-key", func(token *model.Token) {
		token.RemainQuota = 5000
		token.QuotaLimit5h = 1000
		token.QuotaLimit7d = 3000
	})
	require.NoError(t, model.DecreaseTokenQuota(token.Id, token.Key, 400))

	subscription, usage := fetchBilling(t, token)

	// NOTE: The 5h window leaves 600, less than the 7d window (2600) and the key quota (4600).
	require.InDelta(t, billingUSD(600), subscription.HardLimitUSD-usage.TotalUsage/100, 1e-9)
}

func TestBillingClampsExhaustedWindowToZeroRemaining(t *testing.T) {
	db := setupBillingTest(t)
	token := seedBillingToken(t, db, "exhausted-key", func(token *model.Token) {
		token.UnlimitedQuota = true
		token.QuotaLimit5h = 500
	})
	require.NoError(t, model.DecreaseTokenQuota(token.Id, token.Key, 800))

	subscription, usage := fetchBilling(t, token)

	require.InDelta(t, billingUSD(800), subscription.HardLimitUSD, 1e-9)
	require.InDelta(t, billingUSD(800)*100, usage.TotalUsage, 1e-9)
	require.InDelta(t, 0.0, subscription.HardLimitUSD-usage.TotalUsage/100, 1e-9)
}

func TestBillingFallsBackToAccountBalanceWhenTokenHasNoLimits(t *testing.T) {
	db := setupBillingTest(t)
	token := seedBillingToken(t, db, "free-key", func(token *model.Token) {
		token.UnlimitedQuota = true
	})

	subscription, usage := fetchBilling(t, token)

	require.InDelta(t, billingUSD(billingTestUserQuota+billingTestUserUsed), subscription.HardLimitUSD, 1e-9)
	require.InDelta(t, billingUSD(billingTestUserUsed)*100, usage.TotalUsage, 1e-9)
}

func TestBillingIgnoresTokenLimitsWhenTokenStatDisabled(t *testing.T) {
	db := setupBillingTest(t)
	common.DisplayTokenStatEnabled = false
	token := seedBillingToken(t, db, "disabled-key", func(token *model.Token) {
		token.RemainQuota = 300
		token.UsedQuota = 700
	})

	subscription, usage := fetchBilling(t, token)

	require.InDelta(t, billingUSD(billingTestUserQuota+billingTestUserUsed), subscription.HardLimitUSD, 1e-9)
	require.InDelta(t, billingUSD(billingTestUserUsed)*100, usage.TotalUsage, 1e-9)
}
