package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertBatchEditChannelOfType(t *testing.T, name string, chType int, setting string, settings string) *Channel {
	t.Helper()
	channel := insertBatchEditChannel(t, name, "a", "default")
	updates := map[string]any{"type": chType, "setting": setting, "settings": settings}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error)
	return reloadChannel(t, channel.Id)
}

func decodeJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	obj := map[string]any{}
	require.NoError(t, common.UnmarshalJsonStr(raw, &obj))
	return obj
}

func boolPtr(v bool) *bool {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func insertBatchEditChannel(t *testing.T, name string, models string, group string) *Channel {
	t.Helper()
	priority := int64(5)
	weight := uint(3)
	channel := &Channel{
		Name:     name,
		Key:      "sk-" + name,
		Status:   1,
		Models:   models,
		Group:    group,
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))
	return channel
}

func reloadChannel(t *testing.T, id int) *Channel {
	t.Helper()
	var channel Channel
	require.NoError(t, DB.First(&channel, id).Error)
	return &channel
}

func countAbilities(t *testing.T, channelId int, group string, model string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&Ability{}).
		Where("channel_id = ? AND "+commonGroupCol+" = ? AND model = ?", channelId, group, model).
		Count(&count).Error)
	return count
}

func TestMergeChannelModels(t *testing.T) {
	assert.Equal(t, "a,b,c", mergeChannelModels("a,b", BatchModelsModeAppend, []string{"b", " c ", ""}))
	assert.Equal(t, "a,c", mergeChannelModels("a,b,c", BatchModelsModeRemove, []string{"b", "x"}))
	assert.Equal(t, "x,y", mergeChannelModels("a,b", BatchModelsModeReplace, []string{"x", "y", "x"}))
	assert.Equal(t, "", mergeChannelModels("a,b", BatchModelsModeRemove, []string{"a", "b"}))
}

func TestBatchEditChannels_ModelsAppendKeepsEachChannelsOwnModels(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "gpt-4o", "default")
	c2 := insertBatchEditChannel(t, "c2", "claude-3,gpt-4o", "default")

	n, err := BatchEditChannels([]int{c1.Id, c2.Id}, ChannelBatchEditParams{
		ModelsMode: BatchModelsModeAppend,
		Models:     []string{"gpt-4o", "gemini-pro"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	assert.Equal(t, "gpt-4o,gemini-pro", reloadChannel(t, c1.Id).Models)
	assert.Equal(t, "claude-3,gpt-4o,gemini-pro", reloadChannel(t, c2.Id).Models)
	assert.EqualValues(t, 1, countAbilities(t, c1.Id, "default", "gemini-pro"))
	assert.EqualValues(t, 1, countAbilities(t, c2.Id, "default", "gemini-pro"))
}

func TestBatchEditChannels_ModelsRemoveAndReplace(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a,b,c", "default")
	c2 := insertBatchEditChannel(t, "c2", "b,c,d", "default")

	_, err := BatchEditChannels([]int{c1.Id, c2.Id}, ChannelBatchEditParams{
		ModelsMode: BatchModelsModeRemove,
		Models:     []string{"b"},
	})
	require.NoError(t, err)
	assert.Equal(t, "a,c", reloadChannel(t, c1.Id).Models)
	assert.Equal(t, "c,d", reloadChannel(t, c2.Id).Models)
	assert.EqualValues(t, 0, countAbilities(t, c1.Id, "default", "b"))

	_, err = BatchEditChannels([]int{c1.Id, c2.Id}, ChannelBatchEditParams{
		ModelsMode: BatchModelsModeReplace,
		Models:     []string{"x", "y"},
	})
	require.NoError(t, err)
	assert.Equal(t, "x,y", reloadChannel(t, c1.Id).Models)
	assert.Equal(t, "x,y", reloadChannel(t, c2.Id).Models)
	assert.EqualValues(t, 0, countAbilities(t, c1.Id, "default", "a"))
	assert.EqualValues(t, 1, countAbilities(t, c2.Id, "default", "y"))
}

func TestBatchEditChannels_RemoveAllModelsRollsBackEverything(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a,b", "default")
	c2 := insertBatchEditChannel(t, "c2", "a", "default")

	_, err := BatchEditChannels([]int{c1.Id, c2.Id}, ChannelBatchEditParams{
		ModelsMode: BatchModelsModeRemove,
		Models:     []string{"a"},
		Group:      strPtr("vip"),
	})
	require.Error(t, err)

	assert.Equal(t, "a,b", reloadChannel(t, c1.Id).Models)
	assert.Equal(t, "default", reloadChannel(t, c1.Id).Group)
	assert.Equal(t, "a", reloadChannel(t, c2.Id).Models)
	assert.EqualValues(t, 1, countAbilities(t, c1.Id, "default", "a"))
}

func TestBatchEditChannels_GroupRebuildsAbilities(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a,b", "default")

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{Group: strPtr("vip,svip")})
	require.NoError(t, err)

	assert.Equal(t, "vip,svip", reloadChannel(t, c1.Id).Group)
	assert.EqualValues(t, 0, countAbilities(t, c1.Id, "default", "a"))
	assert.EqualValues(t, 1, countAbilities(t, c1.Id, "vip", "a"))
	assert.EqualValues(t, 1, countAbilities(t, c1.Id, "svip", "b"))
}

func TestBatchEditChannels_ZeroAndEmptyValuesAreWritten(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a", "default")
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", c1.Id).Updates(map[string]any{
		"param_override": `{"temperature":1}`,
		"test_model":     "a",
		"auto_ban":       1,
	}).Error)

	priority := int64(0)
	weight := uint(0)
	autoBan := 0
	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{
		Priority:      &priority,
		Weight:        &weight,
		ParamOverride: strPtr(""),
		TestModel:     strPtr(""),
		AutoBan:       &autoBan,
	})
	require.NoError(t, err)

	got := reloadChannel(t, c1.Id)
	require.NotNil(t, got.Priority)
	require.NotNil(t, got.Weight)
	require.NotNil(t, got.AutoBan)
	require.NotNil(t, got.ParamOverride)
	require.NotNil(t, got.TestModel)
	assert.EqualValues(t, 0, *got.Priority)
	assert.EqualValues(t, 0, *got.Weight)
	assert.EqualValues(t, 0, *got.AutoBan)
	assert.Equal(t, "", *got.ParamOverride)
	assert.Equal(t, "", *got.TestModel)
	assert.Equal(t, "a", got.Models)
}

func TestBatchEditChannels_RejectsEmptyRequests(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a", "default")

	_, err := BatchEditChannels(nil, ChannelBatchEditParams{Group: strPtr("vip")})
	require.Error(t, err)

	_, err = BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{})
	require.Error(t, err)

	_, err = BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{ModelsMode: "bogus", Models: []string{"x"}})
	require.Error(t, err)
}

func strPtr(s string) *string {
	return &s
}

func TestMergeModelList(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, mergeModelList([]string{"a"}, BatchModelsModeAppend, []string{"b", "a"}))
	assert.Equal(t, []string{"a"}, mergeModelList([]string{"a", "b"}, BatchModelsModeRemove, []string{"b"}))
	assert.Equal(t, []string{}, mergeModelList([]string{"a"}, BatchModelsModeRemove, []string{"a"}))
	assert.Equal(t, []string{}, mergeModelList([]string{"a"}, BatchModelsModeReplace, nil))
	assert.Equal(t, []string{"a"}, mergeModelList([]string{"a"}, "bogus", []string{"x"}))
}

func TestBatchEditChannels_SettingMergeKeepsUnrelatedKeys(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, `{"proxy":"old","system_prompt":"keep me","unknown":7}`, `{}`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{Proxy: strPtr("http://new")})
	require.NoError(t, err)

	got := decodeJSONObject(t, *reloadChannel(t, c1.Id).Setting)
	assert.Equal(t, "http://new", got["proxy"])
	assert.Equal(t, "keep me", got["system_prompt"])
	assert.EqualValues(t, 7, got["unknown"])
}

func TestBatchEditChannels_SettingZeroValuesAreWritten(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, `{"retry_on_429":5,"thinking_to_content":true}`, `{}`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{
		RetryOn429:        intPtr(0),
		ThinkingToContent: boolPtr(false),
		Proxy:             strPtr(""),
	})
	require.NoError(t, err)

	got := decodeJSONObject(t, *reloadChannel(t, c1.Id).Setting)
	assert.EqualValues(t, 0, got["retry_on_429"])
	assert.Equal(t, false, got["thinking_to_content"])
	assert.Equal(t, "", got["proxy"])
}

func TestBatchEditChannels_SettingsMergeKeepsRuntimeKeys(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, ``,
		`{"upstream_model_update_check_enabled":true,"upstream_model_update_last_check_time":1700000000,"allow_service_tier":true}`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{AutoCacheControl: boolPtr(true)})
	require.NoError(t, err)

	got := decodeJSONObject(t, reloadChannel(t, c1.Id).OtherSettings)
	assert.Equal(t, true, got["auto_cache_control"])
	assert.Equal(t, true, got["upstream_model_update_check_enabled"])
	assert.EqualValues(t, 1700000000, got["upstream_model_update_last_check_time"])
	assert.Equal(t, true, got["allow_service_tier"])
}

func TestBatchEditChannels_ClaudeDisguiseModeWritesLegacyFlag(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 14, ``, `{"claude_code_disguise":true,"claude_code_disguise_mode":7}`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{ClaudeCodeDisguiseMode: intPtr(0)})
	require.NoError(t, err)
	got := decodeJSONObject(t, reloadChannel(t, c1.Id).OtherSettings)
	assert.EqualValues(t, 0, got["claude_code_disguise_mode"])
	assert.Equal(t, false, got["claude_code_disguise"])

	_, err = BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{ClaudeCodeDisguiseMode: intPtr(5)})
	require.NoError(t, err)
	got = decodeJSONObject(t, reloadChannel(t, c1.Id).OtherSettings)
	assert.EqualValues(t, 5, got["claude_code_disguise_mode"])
	assert.Equal(t, true, got["claude_code_disguise"])
}

func TestBatchEditChannels_IgnoredModelsAppendRemoveReplace(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, ``, `{"upstream_model_update_check_enabled":true,"upstream_model_update_ignored_models":["m1","m2"]}`)

	ignored := func() []any {
		return decodeJSONObject(t, reloadChannel(t, c1.Id).OtherSettings)["upstream_model_update_ignored_models"].([]any)
	}

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{IgnoredModelsMode: BatchModelsModeAppend, IgnoredModels: []string{"m2", "m3"}})
	require.NoError(t, err)
	assert.Equal(t, []any{"m1", "m2", "m3"}, ignored())

	_, err = BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{IgnoredModelsMode: BatchModelsModeRemove, IgnoredModels: []string{"m1"}})
	require.NoError(t, err)
	assert.Equal(t, []any{"m2", "m3"}, ignored())

	_, err = BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{IgnoredModelsMode: BatchModelsModeReplace})
	require.NoError(t, err)
	assert.Equal(t, []any{}, ignored())
}

func TestBatchEditChannels_DisablingUpstreamCheckClearsDependentState(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, ``,
		`{"upstream_model_update_check_enabled":true,"upstream_model_update_auto_sync_enabled":true,"upstream_model_update_last_detected_models":["x"]}`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{UpstreamCheckEnabled: boolPtr(false)})
	require.NoError(t, err)

	got := decodeJSONObject(t, reloadChannel(t, c1.Id).OtherSettings)
	assert.Equal(t, false, got["upstream_model_update_check_enabled"])
	assert.Equal(t, false, got["upstream_model_update_auto_sync_enabled"])
	assert.Equal(t, []any{}, got["upstream_model_update_last_detected_models"])
}

func TestBatchEditChannels_ColumnFieldsIncludingZeroValues(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannel(t, "c1", "a", "default")
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", c1.Id).Updates(map[string]any{
		"auto_ban_optimistic":   1,
		"auto_unban_by_balance": 1,
		"status_code_mapping":   `{"400":"500"}`,
	}).Error)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{
		AutoBanOptimistic:  intPtr(0),
		AutoUnbanByBalance: intPtr(0),
		StatusCodeMapping:  strPtr(""),
	})
	require.NoError(t, err)

	got := reloadChannel(t, c1.Id)
	require.NotNil(t, got.AutoBanOptimistic)
	require.NotNil(t, got.AutoUnbanByBalance)
	require.NotNil(t, got.StatusCodeMapping)
	assert.EqualValues(t, 0, *got.AutoBanOptimistic)
	assert.EqualValues(t, 0, *got.AutoUnbanByBalance)
	assert.Equal(t, "", *got.StatusCodeMapping)
}

func TestBatchEditChannels_UnsupportedTypeRollsBackWholeBatch(t *testing.T) {
	truncateTables(t)
	openai := insertBatchEditChannelOfType(t, "openai", 1, `{"proxy":"keep"}`, `{}`)
	claude := insertBatchEditChannelOfType(t, "claude", 14, `{"proxy":"keep"}`, `{}`)

	_, err := BatchEditChannels([]int{openai.Id, claude.Id}, ChannelBatchEditParams{
		Proxy:         strPtr("http://changed"),
		CodexDisguise: boolPtr(true),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support")

	for _, id := range []int{openai.Id, claude.Id} {
		got := decodeJSONObject(t, *reloadChannel(t, id).Setting)
		assert.Equal(t, "keep", got["proxy"])
		assert.NotContains(t, decodeJSONObject(t, reloadChannel(t, id).OtherSettings), "codex_disguise")
	}
}

func TestBatchEditChannels_InvalidExistingJSONIsReplaced(t *testing.T) {
	truncateTables(t)
	c1 := insertBatchEditChannelOfType(t, "c1", 1, `not json`, `{broken`)

	_, err := BatchEditChannels([]int{c1.Id}, ChannelBatchEditParams{
		Proxy:            strPtr("http://p"),
		AutoCacheControl: boolPtr(true),
	})
	require.NoError(t, err)

	got := reloadChannel(t, c1.Id)
	assert.Equal(t, "http://p", decodeJSONObject(t, *got.Setting)["proxy"])
	assert.Equal(t, true, decodeJSONObject(t, got.OtherSettings)["auto_cache_control"])
}
