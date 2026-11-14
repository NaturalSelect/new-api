package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
