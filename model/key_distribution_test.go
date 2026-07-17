package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func createConsumeLog(t *testing.T, userId int, username string, tokenId int, tokenName string, modelName string, promptTokens int, completionTokens int, createdAt int64) {
	t.Helper()
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		TokenId:          tokenId,
		TokenName:        tokenName,
		ModelName:        modelName,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
	require.NoError(t, LOG_DB.Create(log).Error)
}

func createConsumeLogWithOther(t *testing.T, userId int, username string, tokenId int, tokenName string, modelName string, promptTokens int, completionTokens int, createdAt int64, other map[string]interface{}) {
	t.Helper()
	otherJson, err := common.Marshal(other)
	require.NoError(t, err)
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        createdAt,
		Type:             LogTypeConsume,
		TokenId:          tokenId,
		TokenName:        tokenName,
		ModelName:        modelName,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Other:            string(otherJson),
	}
	require.NoError(t, LOG_DB.Create(log).Error)
}

func findKeyDistributionItem(data []*KeyDistributionData, tokenId int, modelName string) *KeyDistributionData {
	for _, item := range data {
		if item.TokenId == tokenId && item.ModelName == modelName {
			return item
		}
	}
	return nil
}

// Same key + same model across multiple requests must accumulate into one row.
func TestGetKeyDistribution_SumsSameKeyAndModel(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 200, 80, 1010)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)

	item := result[0]
	require.Equal(t, 10, item.TokenId)
	require.Equal(t, "key-a", item.TokenName)
	require.Equal(t, "gpt-4", item.ModelName)
	require.Equal(t, 300, item.InputTokens)
	require.Equal(t, 130, item.OutputTokens)
	require.Equal(t, 430, item.TotalTokens)
	require.Equal(t, 2, item.Count)
}

// Different keys and/or different models must be aggregated into separate rows.
func TestGetKeyDistribution_GroupsByKeyAndModelSeparately(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4o", 10, 5, 1001) // same key, different model
	createConsumeLog(t, 1, "alice", 11, "key-b", "gpt-4", 20, 10, 1002) // different key, same model

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 3)

	require.NotNil(t, findKeyDistributionItem(result, 10, "gpt-4"))
	require.NotNil(t, findKeyDistributionItem(result, 10, "gpt-4o"))
	require.NotNil(t, findKeyDistributionItem(result, 11, "gpt-4"))
}

// Only logs within [start_timestamp, end_timestamp] are aggregated.
func TestGetKeyDistribution_TimeRangeFilter(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 999, 999, 5000) // outside range

	result, err := GetKeyDistribution(500, 2000, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 100, result[0].InputTokens)
	require.Equal(t, 50, result[0].OutputTokens)
	require.Equal(t, 1, result[0].Count)
}

// start_timestamp == 0 / end_timestamp == 0 must mean "no boundary", matching
// GetTokenDistribution's existing behavior (no default 30-day style span limit).
func TestGetKeyDistribution_ZeroTimestampsMeanNoBoundary(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 200, 80, 9999999999)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 2, result[0].Count)
}

// Non-consume logs (manage/error/etc.) must never be counted.
func TestGetKeyDistribution_OnlyConsumeLogsCounted(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		CreatedAt: 1001,
		Type:      LogTypeManage,
		TokenId:   10,
		TokenName: "key-a",
		ModelName: "gpt-4",
	}).Error)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 1, result[0].Count)
}

// Admin view: optional username narrows results to a single user, mirroring
// GetTokenDistribution's admin username filter.
func TestGetKeyDistribution_AdminUsernameFilter(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 2, "bob", 20, "key-b", "gpt-4", 999, 999, 1000)

	result, err := GetKeyDistribution(0, 0, "alice")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 10, result[0].TokenId)

	all, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
}

// Core permission check: self view must be strictly scoped by the server-side
// user_id, never by username. Two different accounts sharing the same username
// snapshot (e.g. a recreated account) must not leak each other's stats.
func TestGetSelfKeyDistribution_ForcesUserIdIsolation(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "shared-name", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 2, "shared-name", 20, "key-b", "gpt-4", 999, 999, 1000)

	result, err := GetSelfKeyDistribution(1, 0, 0)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 10, result[0].TokenId)
	require.Equal(t, 100, result[0].InputTokens)
	require.Equal(t, 50, result[0].OutputTokens)

	result2, err := GetSelfKeyDistribution(2, 0, 0)
	require.NoError(t, err)
	require.Len(t, result2, 1)
	require.Equal(t, 20, result2[0].TokenId)
}

// Self view also respects the time range filter.
func TestGetSelfKeyDistribution_TimeRangeFilter(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 999, 999, 5000)

	result, err := GetSelfKeyDistribution(1, 500, 2000)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 100, result[0].InputTokens)
}

// Deleted keys (token_id no longer present in the tokens table) must still be
// returned, because the aggregation reads the logs snapshot and never JOINs tokens.
func TestGetKeyDistribution_DeletedKeyStillReturned(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 999, "deleted-key", "gpt-4", 100, 50, 1000)

	var tokenCount int64
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", 999).Count(&tokenCount).Error)
	require.Zero(t, tokenCount)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 999, result[0].TokenId)
	require.Equal(t, "deleted-key", result[0].TokenName)
}

// total_tokens must equal input+output, and default sort is by total_tokens desc.
func TestGetKeyDistribution_TotalTokensAndSortOrder(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 10, 10, 1000)   // total 20
	createConsumeLog(t, 1, "alice", 11, "key-b", "gpt-4", 100, 100, 1000) // total 200
	createConsumeLog(t, 1, "alice", 12, "key-c", "gpt-4", 50, 50, 1000)   // total 100

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 3)

	require.Equal(t, 11, result[0].TokenId) // 200
	require.Equal(t, 12, result[1].TokenId) // 100
	require.Equal(t, 10, result[2].TokenId) // 20
	for _, item := range result {
		require.Equal(t, item.InputTokens+item.OutputTokens, item.TotalTokens)
	}
}

// Ties on total_tokens must break deterministically (stable secondary sort),
// independent of database row order.
func TestGetKeyDistribution_StableSecondarySort(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 20, "key-b", "gpt-4", 50, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4o", 50, 50, 1000)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 50, 50, 1000)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 3)

	require.Equal(t, 10, result[0].TokenId)
	require.Equal(t, "gpt-4", result[0].ModelName)
	require.Equal(t, 10, result[1].TokenId)
	require.Equal(t, "gpt-4o", result[1].ModelName)
	require.Equal(t, 20, result[2].TokenId)
}

// Cache hit (cache_tokens) and cache write (cache_write_tokens) live in logs.other
// and must be parsed and summed per key+model, matching GetTokenDistribution.
func TestGetKeyDistribution_SumsCacheTokensFromOther(t *testing.T) {
	truncateTables(t)

	createConsumeLogWithOther(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000, map[string]interface{}{
		"cache_tokens":       20,
		"cache_write_tokens": 5,
	})
	createConsumeLogWithOther(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1010, map[string]interface{}{
		"cache_tokens":       30,
		"cache_write_tokens": 10,
	})

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)

	item := result[0]
	require.Equal(t, 50, item.CacheReadTokens)
	require.Equal(t, 15, item.CacheWriteTokens)
	// total_tokens stays input+output only; cache tokens are additional/informational
	// and must not change the existing sort-key semantics.
	require.Equal(t, item.InputTokens+item.OutputTokens, item.TotalTokens)
}

// Cache write falls back to cache_creation_tokens / cache_creation_tokens_5m+1h when
// cache_write_tokens is absent, mirroring getCacheWriteTokensFromOther's fallback chain.
func TestGetKeyDistribution_CacheWriteFallsBackToCreationTokens(t *testing.T) {
	truncateTables(t)

	createConsumeLogWithOther(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000, map[string]interface{}{
		"cache_creation_tokens_5m": 7,
		"cache_creation_tokens_1h": 3,
	})

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 10, result[0].CacheWriteTokens)
}

// Logs without an other payload (the createConsumeLog helper) must not error
// and simply contribute zero cache tokens.
func TestGetKeyDistribution_NoOtherMeansZeroCacheTokens(t *testing.T) {
	truncateTables(t)

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, 1000)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 0, result[0].CacheReadTokens)
	require.Equal(t, 0, result[0].CacheWriteTokens)
}

// Empty result must be an empty slice, not nil, so it serializes as `[]`.
func TestGetKeyDistribution_EmptyResultReturnsEmptySlice(t *testing.T) {
	truncateTables(t)

	result, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result, 0)

	selfResult, err := GetSelfKeyDistribution(1, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, selfResult)
	require.Len(t, selfResult, 0)
}
