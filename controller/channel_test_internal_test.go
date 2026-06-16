package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestParseChannelStatusTime(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		expected   int64
		expectedOK bool
	}{
		{name: "float64", value: float64(123), expected: 123, expectedOK: true},
		{name: "int", value: int(456), expected: 456, expectedOK: true},
		{name: "int64", value: int64(789), expected: 789, expectedOK: true},
		{name: "json number int", value: json.Number("987"), expected: 987, expectedOK: true},
		{name: "json number float", value: json.Number("654.9"), expected: 654, expectedOK: true},
		{name: "zero", value: 0, expectedOK: false},
		{name: "invalid", value: json.Number("invalid"), expectedOK: false},
		{name: "missing", value: nil, expectedOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, ok := parseChannelStatusTime(tt.value)

			require.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				require.Equal(t, tt.expected, actual)
			}
		})
	}
}

func TestOptimisticUnbanKeysNonMultiKeyRequiresElapsedAutoDisabledChannel(t *testing.T) {
	autoBanOptimistic := 1
	channel := &model.Channel{
		Status:            common.ChannelStatusAutoDisabled,
		AutoBanOptimistic: &autoBanOptimistic,
	}
	channel.SetOtherInfo(map[string]interface{}{"status_time": int64(100)})

	require.Empty(t, optimisticUnbanKeys(channel, 159, 1))
	require.Equal(t, []string{""}, optimisticUnbanKeys(channel, 160, 1))

	channel.Status = common.ChannelStatusEnabled
	require.Empty(t, optimisticUnbanKeys(channel, 1_000, 1))

	channel.Status = common.ChannelStatusManuallyDisabled
	require.Empty(t, optimisticUnbanKeys(channel, 1_000, 1))
}

func TestOptimisticUnbanKeysMultiKeySkipsManualInvalidAndFreshKeys(t *testing.T) {
	autoBanOptimistic := 1
	channel := &model.Channel{
		Key:               "key-0\nkey-1\nkey-2",
		Status:            common.ChannelStatusAutoDisabled,
		AutoBanOptimistic: &autoBanOptimistic,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
			MultiKeyStatusList: map[int]int{
				-1: common.ChannelStatusAutoDisabled,
				0:  common.ChannelStatusAutoDisabled,
				1:  common.ChannelStatusManuallyDisabled,
				2:  common.ChannelStatusAutoDisabled,
				3:  common.ChannelStatusAutoDisabled,
			},
			MultiKeyDisabledTime: map[int]int64{
				-1: 100,
				0:  100,
				1:  100,
				2:  150,
				3:  100,
			},
		},
	}

	require.Equal(t, []string{"key-0"}, optimisticUnbanKeys(channel, 160, 1))

	channel.Status = common.ChannelStatusEnabled
	require.Equal(t, []string{"key-0"}, optimisticUnbanKeys(channel, 160, 1))

	channel.Status = common.ChannelStatusManuallyDisabled
	require.Empty(t, optimisticUnbanKeys(channel, 160, 1))
}
