package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
)

const (
	BatchModelsModeAppend  = "append"
	BatchModelsModeRemove  = "remove"
	BatchModelsModeReplace = "replace"
)

// NOTE: Keep in sync with MODEL_FETCHABLE_TYPES in web/default/src/features/channels/constants.ts.
var batchEditFetchableTypes = []int{
	1, 4, 14, 17, 20, 23, 24, 25, 26, 27, 31, 34, 35, 40, 42, 43, 47, 48, 58, 59,
	61, 63, 64,
}

// NOTE: Keep in sync with BATCH_EDIT_FIELD_TYPES in web/default/src/features/channels/constants.ts.
var batchEditFieldTypes = map[string][]int{
	"force_format":              {1},
	"codex_disguise":            {1},
	"claude_code_disguise_mode": {14, 59},
	"claude_beta_query":         {14, 59},
	"auto_cache_control":        {1, 14, 58, 59},
	"upstream_model_update":     batchEditFetchableTypes,
}

// NOTE: A nil pointer field means "leave unchanged"; a non-nil pointer to a zero value is written as-is.
type ChannelBatchEditParams struct {
	ModelsMode         string
	Models             []string
	Group              *string
	Priority           *int64
	Weight             *uint
	ModelMapping       *string
	ParamOverride      *string
	HeaderOverride     *string
	TestModel          *string
	AutoBan            *int
	AutoBanOptimistic  *int
	AutoUnbanByBalance *int
	StatusCodeMapping  *string

	// NOTE: These fields live in the channel "setting" JSON column.
	Proxy                  *string
	SystemPrompt           *string
	SystemPromptOverride   *bool
	RetryOn429             *int
	ThinkingToContent      *bool
	PassThroughBodyEnabled *bool
	ForceFormat            *bool

	// NOTE: These fields live in the channel "settings" JSON column.
	CodexDisguise           *bool
	ClaudeCodeDisguiseMode  *int
	ClaudeBetaQuery         *bool
	AutoCacheControl        *bool
	UpstreamCheckEnabled    *bool
	UpstreamAutoSyncEnabled *bool
	IgnoredModelsMode       string
	IgnoredModels           []string
}

func (p *ChannelBatchEditParams) hasSettingChanges() bool {
	return p.Proxy != nil || p.SystemPrompt != nil || p.SystemPromptOverride != nil ||
		p.RetryOn429 != nil || p.ThinkingToContent != nil || p.PassThroughBodyEnabled != nil ||
		p.ForceFormat != nil
}

func (p *ChannelBatchEditParams) hasOtherSettingsChanges() bool {
	return p.CodexDisguise != nil || p.ClaudeCodeDisguiseMode != nil || p.ClaudeBetaQuery != nil ||
		p.AutoCacheControl != nil || p.UpstreamCheckEnabled != nil || p.UpstreamAutoSyncEnabled != nil ||
		p.IgnoredModelsMode != ""
}

func (p *ChannelBatchEditParams) hasChanges() bool {
	return p.ModelsMode != "" || p.Group != nil || p.Priority != nil || p.Weight != nil ||
		p.ModelMapping != nil || p.ParamOverride != nil || p.HeaderOverride != nil ||
		p.TestModel != nil || p.AutoBan != nil || p.AutoBanOptimistic != nil ||
		p.AutoUnbanByBalance != nil || p.StatusCodeMapping != nil ||
		p.hasSettingChanges() || p.hasOtherSettingsChanges()
}

func (p *ChannelBatchEditParams) restrictedFields() []string {
	fields := make([]string, 0, 6)
	if p.ForceFormat != nil {
		fields = append(fields, "force_format")
	}
	if p.CodexDisguise != nil {
		fields = append(fields, "codex_disguise")
	}
	if p.ClaudeCodeDisguiseMode != nil {
		fields = append(fields, "claude_code_disguise_mode")
	}
	if p.ClaudeBetaQuery != nil {
		fields = append(fields, "claude_beta_query")
	}
	if p.AutoCacheControl != nil {
		fields = append(fields, "auto_cache_control")
	}
	if p.UpstreamCheckEnabled != nil || p.UpstreamAutoSyncEnabled != nil || p.IgnoredModelsMode != "" {
		fields = append(fields, "upstream_model_update")
	}
	return fields
}

func splitChannelModels(models string) []string {
	parts := strings.Split(models, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

// NOTE: Unknown modes return current unchanged; blanks and duplicates are dropped.
func mergeModelList(current []string, mode string, incoming []string) []string {
	incoming = splitChannelModels(strings.Join(incoming, ","))
	current = splitChannelModels(strings.Join(current, ","))
	switch mode {
	case BatchModelsModeReplace:
		return incoming
	case BatchModelsModeAppend:
		return splitChannelModels(strings.Join(append(current, incoming...), ","))
	case BatchModelsModeRemove:
		removed := make(map[string]struct{}, len(incoming))
		for _, name := range incoming {
			removed[name] = struct{}{}
		}
		kept := make([]string, 0, len(current))
		for _, name := range current {
			if _, ok := removed[name]; !ok {
				kept = append(kept, name)
			}
		}
		return kept
	}
	return current
}

func mergeChannelModels(current string, mode string, models []string) string {
	switch mode {
	case BatchModelsModeAppend, BatchModelsModeRemove, BatchModelsModeReplace:
		return strings.Join(mergeModelList(splitChannelModels(current), mode, models), ",")
	}
	return current
}

// NOTE: Invalid JSON is treated as empty, matching how GetSetting and GetOtherSettings recover.
func loadJSONObject(raw string) map[string]any {
	obj := map[string]any{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return obj
	}
	if err := common.UnmarshalJsonStr(raw, &obj); err != nil || obj == nil {
		return map[string]any{}
	}
	return obj
}

func applySettingPatch(obj map[string]any, p *ChannelBatchEditParams) {
	if p.Proxy != nil {
		obj["proxy"] = *p.Proxy
	}
	if p.SystemPrompt != nil {
		obj["system_prompt"] = *p.SystemPrompt
	}
	if p.SystemPromptOverride != nil {
		obj["system_prompt_override"] = *p.SystemPromptOverride
	}
	if p.RetryOn429 != nil {
		obj["retry_on_429"] = *p.RetryOn429
	}
	if p.ThinkingToContent != nil {
		obj["thinking_to_content"] = *p.ThinkingToContent
	}
	if p.PassThroughBodyEnabled != nil {
		obj["pass_through_body_enabled"] = *p.PassThroughBodyEnabled
	}
	if p.ForceFormat != nil {
		obj["force_format"] = *p.ForceFormat
	}
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func applyOtherSettingsPatch(obj map[string]any, p *ChannelBatchEditParams) {
	if p.CodexDisguise != nil {
		obj["codex_disguise"] = *p.CodexDisguise
	}
	if p.ClaudeCodeDisguiseMode != nil {
		obj["claude_code_disguise_mode"] = *p.ClaudeCodeDisguiseMode
		obj["claude_code_disguise"] = *p.ClaudeCodeDisguiseMode > 0
	}
	if p.ClaudeBetaQuery != nil {
		obj["claude_beta_query"] = *p.ClaudeBetaQuery
	}
	if p.AutoCacheControl != nil {
		obj["auto_cache_control"] = *p.AutoCacheControl
	}

	touchedUpstream := false
	if p.UpstreamCheckEnabled != nil {
		obj["upstream_model_update_check_enabled"] = *p.UpstreamCheckEnabled
		touchedUpstream = true
	}
	if p.UpstreamAutoSyncEnabled != nil {
		obj["upstream_model_update_auto_sync_enabled"] = *p.UpstreamAutoSyncEnabled
		touchedUpstream = true
	}
	if p.IgnoredModelsMode != "" {
		current := stringSliceFromAny(obj["upstream_model_update_ignored_models"])
		obj["upstream_model_update_ignored_models"] = mergeModelList(current, p.IgnoredModelsMode, p.IgnoredModels)
		touchedUpstream = true
	}
	if touchedUpstream {
		enabled, _ := obj["upstream_model_update_check_enabled"].(bool)
		if !enabled {
			obj["upstream_model_update_auto_sync_enabled"] = false
			obj["upstream_model_update_last_detected_models"] = []string{}
		}
	}
}

func marshalJSONObject(obj map[string]any) (string, error) {
	data, err := common.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// NOTE: Any failure rolls back every channel in the batch.
func BatchEditChannels(ids []int, params ChannelBatchEditParams) (int, error) {
	ids = lo.Uniq(ids)
	if len(ids) == 0 {
		return 0, errors.New("no channels selected")
	}
	if !params.hasChanges() {
		return 0, errors.New("no fields to update")
	}
	switch params.ModelsMode {
	case "", BatchModelsModeAppend, BatchModelsModeRemove, BatchModelsModeReplace:
	default:
		return 0, fmt.Errorf("invalid models mode: %s", params.ModelsMode)
	}
	switch params.IgnoredModelsMode {
	case "", BatchModelsModeAppend, BatchModelsModeRemove, BatchModelsModeReplace:
	default:
		return 0, fmt.Errorf("invalid ignored models mode: %s", params.IgnoredModelsMode)
	}

	baseUpdates := make(map[string]any)
	if params.Group != nil {
		baseUpdates["group"] = *params.Group
	}
	if params.Priority != nil {
		baseUpdates["priority"] = *params.Priority
	}
	if params.Weight != nil {
		baseUpdates["weight"] = *params.Weight
	}
	if params.ModelMapping != nil {
		baseUpdates["model_mapping"] = *params.ModelMapping
	}
	if params.ParamOverride != nil {
		baseUpdates["param_override"] = *params.ParamOverride
	}
	if params.HeaderOverride != nil {
		baseUpdates["header_override"] = *params.HeaderOverride
	}
	if params.TestModel != nil {
		baseUpdates["test_model"] = *params.TestModel
	}
	if params.AutoBan != nil {
		baseUpdates["auto_ban"] = *params.AutoBan
	}
	if params.AutoBanOptimistic != nil {
		baseUpdates["auto_ban_optimistic"] = *params.AutoBanOptimistic
	}
	if params.AutoUnbanByBalance != nil {
		baseUpdates["auto_unban_by_balance"] = *params.AutoUnbanByBalance
	}
	if params.StatusCodeMapping != nil {
		baseUpdates["status_code_mapping"] = *params.StatusCodeMapping
	}
	needAbilities := params.ModelsMode != "" || params.Group != nil || params.Priority != nil || params.Weight != nil
	restricted := params.restrictedFields()

	tx := DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	updated := 0
	for _, chunk := range lo.Chunk(ids, 200) {
		var channels []*Channel
		if err := tx.Where("id in (?)", chunk).Find(&channels).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		for _, channel := range channels {
			for _, field := range restricted {
				if !lo.Contains(batchEditFieldTypes[field], channel.Type) {
					tx.Rollback()
					return 0, fmt.Errorf("channel %d (type %d) does not support %s", channel.Id, channel.Type, field)
				}
			}

			updates := make(map[string]any, len(baseUpdates)+3)
			for k, v := range baseUpdates {
				updates[k] = v
			}
			if params.ModelsMode != "" {
				merged := mergeChannelModels(channel.Models, params.ModelsMode, params.Models)
				if merged == "" {
					tx.Rollback()
					return 0, fmt.Errorf("channel %d would have no models after the change", channel.Id)
				}
				updates["models"] = merged
				channel.Models = merged
			}
			if params.hasSettingChanges() {
				raw := ""
				if channel.Setting != nil {
					raw = *channel.Setting
				}
				obj := loadJSONObject(raw)
				applySettingPatch(obj, &params)
				encoded, err := marshalJSONObject(obj)
				if err != nil {
					tx.Rollback()
					return 0, err
				}
				updates["setting"] = encoded
			}
			if params.hasOtherSettingsChanges() {
				obj := loadJSONObject(channel.OtherSettings)
				applyOtherSettingsPatch(obj, &params)
				encoded, err := marshalJSONObject(obj)
				if err != nil {
					tx.Rollback()
					return 0, err
				}
				updates["settings"] = encoded
			}
			if params.Group != nil {
				channel.Group = *params.Group
			}
			if params.Priority != nil {
				channel.Priority = params.Priority
			}
			if params.Weight != nil {
				channel.Weight = params.Weight
			}
			if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
				tx.Rollback()
				return 0, err
			}
			if needAbilities {
				if err := channel.UpdateAbilities(tx); err != nil {
					tx.Rollback()
					return 0, err
				}
			}
			updated++
		}
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return updated, nil
}
