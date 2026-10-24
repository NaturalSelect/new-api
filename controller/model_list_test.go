package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type listModelsResponse struct {
	Success bool               `json:"success"`
	Data    []dto.OpenAIModels `json:"data"`
	Object  string             `json:"object"`
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	initModelListColumnNames(t)

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func initModelListColumnNames(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func withTieredBillingConfig(t *testing.T, modes map[string]string, exprs map[string]string) {
	t.Helper()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		model.InvalidatePricingCache()
	})

	modeBytes, err := common.Marshal(modes)
	require.NoError(t, err)
	exprBytes, err := common.Marshal(exprs)
	require.NoError(t, err)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": string(modeBytes),
		"billing_setting.billing_expr": string(exprBytes),
	}))
	model.InvalidatePricingCache()
}

func withSelfUseModeDisabled(t *testing.T) {
	t.Helper()

	original := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = original
	})
}

func decodeListModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]struct{} {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)

	ids := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		ids[item.Id] = struct{}{}
	}
	return ids
}

func pricingByModelName(pricings []model.Pricing) map[string]model.Pricing {
	byName := make(map[string]model.Pricing, len(pricings))
	for _, pricing := range pricings {
		byName[pricing.ModelName] = pricing
	}
	return byName
}

func TestListModelsIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-tiered-visible-model":      "tiered_expr",
		"zz-tiered-empty-expr-model":   "tiered_expr",
		"zz-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-tiered-empty-expr-model": "   ",
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "model-list-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-tiered-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-empty-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-missing-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-unpriced-model", ChannelId: 1, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1001)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-tiered-visible-model")
	require.NotContains(t, ids, "zz-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-unpriced-model")

	pricingByName := pricingByModelName(model.GetPricing())
	visiblePricing, ok := pricingByName["zz-tiered-visible-model"]
	require.True(t, ok)
	require.Equal(t, "tiered_expr", visiblePricing.BillingMode)
	require.NotEmpty(t, visiblePricing.BillingExpr)

	emptyExprPricing, ok := pricingByName["zz-tiered-empty-expr-model"]
	require.True(t, ok)
	require.Empty(t, emptyExprPricing.BillingMode)
	require.Empty(t, emptyExprPricing.BillingExpr)

	missingExprPricing, ok := pricingByName["zz-tiered-missing-expr-model"]
	require.True(t, ok)
	require.Empty(t, missingExprPricing.BillingMode)
	require.Empty(t, missingExprPricing.BillingExpr)
}

func TestListModelsTokenLimitIncludesTieredBillingModel(t *testing.T) {
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-token-tiered-visible-model":      "tiered_expr",
		"zz-token-tiered-empty-expr-model":   "tiered_expr",
		"zz-token-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-token-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-token-tiered-empty-expr-model": "",
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"zz-token-tiered-visible-model":      true,
		"zz-token-tiered-empty-expr-model":   true,
		"zz-token-tiered-missing-expr-model": true,
		"zz-token-unpriced-model":            true,
	})

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-token-tiered-visible-model")
	require.NotContains(t, ids, "zz-token-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-token-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-token-unpriced-model")
}

func withRatioSettings(t *testing.T, modelRatio, completionRatio, groupRatio string) {
	t.Helper()

	type setting struct {
		current func() string
		update  func(string) error
		value   string
	}
	settings := []setting{
		{ratio_setting.ModelRatio2JSONString, ratio_setting.UpdateModelRatioByJSONString, modelRatio},
		{ratio_setting.CompletionRatio2JSONString, ratio_setting.UpdateCompletionRatioByJSONString, completionRatio},
		{ratio_setting.GroupRatio2JSONString, ratio_setting.UpdateGroupRatioByJSONString, groupRatio},
	}
	for _, s := range settings {
		original := s.current()
		update := s.update
		t.Cleanup(func() { require.NoError(t, update(original)) })
		require.NoError(t, s.update(s.value))
	}
}

func pricingSetupForListModels(t *testing.T) {
	t.Helper()

	withSelfUseModeDisabled(t)
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	withRatioSettings(t,
		`{"zz-priced-ratio-model":1.25}`,
		`{"zz-priced-ratio-model":8}`,
		`{"default":1,"vip":0.5}`,
	)
	withTieredBillingConfig(t, map[string]string{
		"zz-priced-tiered-model": "tiered_expr",
	}, map[string]string{
		"zz-priced-tiered-model": `tier("base", p * 3 + c * 15)`,
	})

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1002,
		Username: "model-pricing-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-priced-ratio-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-priced-tiered-model", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "zz-priced-ratio-model", ChannelId: 1, Enabled: true},
		{Group: "vip", Model: "zz-priced-tiered-model", ChannelId: 1, Enabled: true},
	}).Error)
	model.InvalidatePricingCache()
}

func listModelsPayload(t *testing.T, modelType int, tokenGroup string) (*httptest.ResponseRecorder, map[string]dto.OpenAIModels) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1002)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	if tokenGroup != "" {
		common.SetContextKey(ctx, constant.ContextKeyTokenGroup, tokenGroup)
	}

	ListModels(ctx, modelType)

	byId := map[string]dto.OpenAIModels{}
	if modelType == constant.ChannelTypeOpenAI {
		require.Equal(t, http.StatusOK, recorder.Code)
		var payload listModelsResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		for _, item := range payload.Data {
			byId[item.Id] = item
		}
	}
	return recorder, byId
}

func TestListModelsIncludesOpenRouterPricing(t *testing.T) {
	pricingSetupForListModels(t)

	_, byId := listModelsPayload(t, constant.ChannelTypeOpenAI, "")

	ratioModel, ok := byId["zz-priced-ratio-model"]
	require.True(t, ok)
	require.NotNil(t, ratioModel.Pricing)
	require.Equal(t, "0.0000025", ratioModel.Pricing.Prompt)
	require.Equal(t, "0.00002", ratioModel.Pricing.Completion)
	require.Equal(t, "0", ratioModel.Pricing.Request)

	tieredModel, ok := byId["zz-priced-tiered-model"]
	require.True(t, ok)
	require.NotNil(t, tieredModel.Pricing)
	require.Equal(t, "0.000003", tieredModel.Pricing.Prompt)
	require.Equal(t, "0.000015", tieredModel.Pricing.Completion)
}

func TestListModelsOpenRouterPricingUsesTokenGroupRatio(t *testing.T) {
	pricingSetupForListModels(t)

	_, byId := listModelsPayload(t, constant.ChannelTypeOpenAI, "vip")

	ratioModel, ok := byId["zz-priced-ratio-model"]
	require.True(t, ok)
	require.NotNil(t, ratioModel.Pricing)
	require.Equal(t, "0.00000125", ratioModel.Pricing.Prompt)

	tieredModel, ok := byId["zz-priced-tiered-model"]
	require.True(t, ok)
	require.NotNil(t, tieredModel.Pricing)
	require.Equal(t, "0.0000015", tieredModel.Pricing.Prompt)
}

func TestListModelsOpenRouterPricingAutoGroupUsesFirstEnabledGroup(t *testing.T) {
	pricingSetupForListModels(t)

	groups := modelListGroups{
		userGroup:   "default",
		tokenGroup:  "auto",
		ownerGroups: []string{"vip", "default"},
	}
	require.Equal(t, "vip", pricingGroupForModel(groups, "zz-priced-ratio-model"))

	groups.ownerGroups = []string{"missing", "default"}
	require.Equal(t, "default", pricingGroupForModel(groups, "zz-priced-ratio-model"))

	groups.ownerGroups = []string{"missing", "other"}
	require.Equal(t, "missing", pricingGroupForModel(groups, "zz-priced-ratio-model"))

	groups.ownerGroups = nil
	require.Equal(t, "default", pricingGroupForModel(groups, "zz-priced-ratio-model"))
}

func TestListModelsAnthropicFormatOmitsPricing(t *testing.T) {
	pricingSetupForListModels(t)

	recorder, _ := listModelsPayload(t, constant.ChannelTypeAnthropic, "")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "pricing")
}

func TestListModelsGeminiFormatOmitsPricing(t *testing.T) {
	pricingSetupForListModels(t)

	recorder, _ := listModelsPayload(t, constant.ChannelTypeGemini, "")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "pricing")
}
