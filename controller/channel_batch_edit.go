package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const maxStatusCodeMappingLength = 1024

type ChannelBatchEdit struct {
	Ids                []int    `json:"ids"`
	ModelsMode         string   `json:"models_mode"`
	Models             []string `json:"models"`
	Group              *string  `json:"group"`
	Priority           *int64   `json:"priority"`
	Weight             *uint    `json:"weight"`
	ModelMapping       *string  `json:"model_mapping"`
	ParamOverride      *string  `json:"param_override"`
	HeaderOverride     *string  `json:"header_override"`
	TestModel          *string  `json:"test_model"`
	AutoBan            *int     `json:"auto_ban"`
	AutoBanOptimistic  *int     `json:"auto_ban_optimistic"`
	AutoUnbanByBalance *int     `json:"auto_unban_by_balance"`
	StatusCodeMapping  *string  `json:"status_code_mapping"`

	Proxy                  *string `json:"proxy"`
	SystemPrompt           *string `json:"system_prompt"`
	SystemPromptOverride   *bool   `json:"system_prompt_override"`
	RetryOn429             *int    `json:"retry_on_429"`
	ThinkingToContent      *bool   `json:"thinking_to_content"`
	PassThroughBodyEnabled *bool   `json:"pass_through_body_enabled"`
	ForceFormat            *bool   `json:"force_format"`

	CodexDisguise                   *bool    `json:"codex_disguise"`
	ClaudeCodeDisguiseMode          *int     `json:"claude_code_disguise_mode"`
	ClaudeBetaQuery                 *bool    `json:"claude_beta_query"`
	AutoCacheControl                *bool    `json:"auto_cache_control"`
	UpstreamModelUpdateCheckEnabled *bool    `json:"upstream_model_update_check_enabled"`
	UpstreamModelUpdateAutoSync     *bool    `json:"upstream_model_update_auto_sync_enabled"`
	IgnoredModelsMode               string   `json:"upstream_model_update_ignored_models_mode"`
	IgnoredModels                   []string `json:"upstream_model_update_ignored_models"`
}

func normalizeOptionalJSON(value *string, message string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed != "" && !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("%s", message)
	}
	return common.GetPointer[string](trimmed), nil
}

// normalizeBatchEditModelNames trims, de-duplicates and length-checks model names.
func normalizeBatchEditModelNames(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if len(name) > 255 {
			return nil, fmt.Errorf("模型名称过长")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result, nil
}

func validateStatusCodeMapping(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxStatusCodeMappingLength {
		return fmt.Errorf("状态码映射长度不能超过 %d 个字符", maxStatusCodeMappingLength)
	}
	mapping := map[string]any{}
	if err := common.UnmarshalJsonStr(value, &mapping); err != nil {
		return fmt.Errorf("状态码映射必须是合法的 JSON 对象")
	}
	inRange := func(code int) bool { return code >= 100 && code <= 599 }
	for from, to := range mapping {
		fromCode, err := strconv.Atoi(from)
		if err != nil || !inRange(fromCode) {
			return fmt.Errorf("状态码映射的键必须是 100 到 599 的状态码: %s", from)
		}
		var toCode int
		switch v := to.(type) {
		case string:
			toCode, err = strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("状态码映射的值必须是 100 到 599 的状态码: %s", from)
			}
		case float64:
			if v != float64(int(v)) {
				return fmt.Errorf("状态码映射的值必须是 100 到 599 的状态码: %s", from)
			}
			toCode = int(v)
		default:
			return fmt.Errorf("状态码映射的值必须是 100 到 599 的状态码: %s", from)
		}
		if !inRange(toCode) {
			return fmt.Errorf("状态码映射的值必须是 100 到 599 的状态码: %s", from)
		}
	}
	return nil
}

func isBinaryFlag(value *int) bool {
	return value == nil || *value == 0 || *value == 1
}

func isValidBatchModelsMode(mode string) bool {
	switch mode {
	case "", model.BatchModelsModeAppend, model.BatchModelsModeRemove, model.BatchModelsModeReplace:
		return true
	}
	return false
}

// buildChannelBatchEditParams validates req and converts it into model parameters.
func buildChannelBatchEditParams(req *ChannelBatchEdit) (model.ChannelBatchEditParams, error) {
	params := model.ChannelBatchEditParams{
		ModelsMode:              req.ModelsMode,
		Priority:                req.Priority,
		Weight:                  req.Weight,
		AutoBan:                 req.AutoBan,
		AutoBanOptimistic:       req.AutoBanOptimistic,
		AutoUnbanByBalance:      req.AutoUnbanByBalance,
		Proxy:                   req.Proxy,
		SystemPrompt:            req.SystemPrompt,
		SystemPromptOverride:    req.SystemPromptOverride,
		RetryOn429:              req.RetryOn429,
		ThinkingToContent:       req.ThinkingToContent,
		PassThroughBodyEnabled:  req.PassThroughBodyEnabled,
		ForceFormat:             req.ForceFormat,
		CodexDisguise:           req.CodexDisguise,
		ClaudeCodeDisguiseMode:  req.ClaudeCodeDisguiseMode,
		ClaudeBetaQuery:         req.ClaudeBetaQuery,
		AutoCacheControl:        req.AutoCacheControl,
		UpstreamCheckEnabled:    req.UpstreamModelUpdateCheckEnabled,
		UpstreamAutoSyncEnabled: req.UpstreamModelUpdateAutoSync,
		IgnoredModelsMode:       req.IgnoredModelsMode,
	}

	if !isValidBatchModelsMode(req.ModelsMode) {
		return params, fmt.Errorf("不支持的模型修改方式")
	}
	if req.ModelsMode != "" {
		names, err := normalizeBatchEditModelNames(req.Models)
		if err != nil {
			return params, err
		}
		if len(names) == 0 {
			return params, fmt.Errorf("模型列表不能为空")
		}
		params.Models = names
	}

	if !isValidBatchModelsMode(req.IgnoredModelsMode) {
		return params, fmt.Errorf("不支持的忽略模型修改方式")
	}
	if req.IgnoredModelsMode != "" {
		names, err := normalizeBatchEditModelNames(req.IgnoredModels)
		if err != nil {
			return params, err
		}
		if len(names) == 0 && req.IgnoredModelsMode != model.BatchModelsModeReplace {
			return params, fmt.Errorf("忽略模型列表不能为空")
		}
		params.IgnoredModels = names
	}

	if req.Group != nil {
		group := strings.TrimSpace(*req.Group)
		if group == "" {
			return params, fmt.Errorf("分组不能为空")
		}
		params.Group = &group
	}
	if req.TestModel != nil {
		testModel := strings.TrimSpace(*req.TestModel)
		params.TestModel = &testModel
	}
	if req.Proxy != nil {
		proxy := strings.TrimSpace(*req.Proxy)
		params.Proxy = &proxy
	}

	if !isBinaryFlag(req.AutoBan) || !isBinaryFlag(req.AutoBanOptimistic) || !isBinaryFlag(req.AutoUnbanByBalance) {
		return params, fmt.Errorf("自动禁用相关开关只能是 0 或 1")
	}
	if req.RetryOn429 != nil && (*req.RetryOn429 < 0 || *req.RetryOn429 > dto.MaxRetryOn429) {
		return params, fmt.Errorf("429 重试次数必须在 0 到 %d 之间", dto.MaxRetryOn429)
	}
	if req.ClaudeCodeDisguiseMode != nil && (*req.ClaudeCodeDisguiseMode < 0 || *req.ClaudeCodeDisguiseMode > dto.ClaudeDisguiseFull) {
		return params, fmt.Errorf("Claude Code 伪装模式必须在 0 到 %d 之间", dto.ClaudeDisguiseFull)
	}

	if req.StatusCodeMapping != nil {
		trimmed := strings.TrimSpace(*req.StatusCodeMapping)
		if err := validateStatusCodeMapping(trimmed); err != nil {
			return params, err
		}
		params.StatusCodeMapping = &trimmed
	}

	var err error
	if params.ModelMapping, err = normalizeOptionalJSON(req.ModelMapping, "模型映射必须是合法的 JSON 格式"); err != nil {
		return params, err
	}
	if params.ParamOverride, err = normalizeOptionalJSON(req.ParamOverride, "参数覆盖必须是合法的 JSON 格式"); err != nil {
		return params, err
	}
	if params.HeaderOverride, err = normalizeOptionalJSON(req.HeaderOverride, "请求头覆盖必须是合法的 JSON 格式"); err != nil {
		return params, err
	}
	return params, nil
}

func BatchEditChannels(c *gin.Context) {
	req := ChannelBatchEdit{}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}

	params, err := buildChannelBatchEditParams(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	updated, err := model.BatchEditChannels(req.Ids, params)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    updated,
	})
}
