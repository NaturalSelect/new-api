package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// PoeLog stores Poe API usage records synced from the Poe usage history endpoint.
// Each entry corresponds to one query charged on the Poe account.
type PoeLog struct {
	Id               int    `json:"id"`
	ChannelId        int    `json:"channel_id" gorm:"index;index:idx_poe_ch_creation,priority:1"`
	QueryId          string `json:"query_id" gorm:"uniqueIndex;type:varchar(64)"`
	BotName          string `json:"bot_name" gorm:"index;index:idx_poe_creation_bot,priority:2;default:''"`
	CreationTime     int64  `json:"creation_time" gorm:"index;index:idx_poe_ch_creation,priority:2;index:idx_poe_creation_bot,priority:1"` // microseconds (from Poe API)
	CostUsd          string `json:"cost_usd" gorm:"default:''"`
	CostPoints       int    `json:"cost_points" gorm:"default:0"`
	CostBreakdown    string `json:"cost_breakdown"`               // JSON string of cost_breakdown_in_points
	UsageType        string `json:"usage_type" gorm:"default:''"` // Chat, API, Canvas App
	ApiKeyName       string `json:"api_key_name" gorm:"default:''"`
	ChatName         string `json:"chat_name" gorm:"default:''"`
	CanvasTabName    string `json:"canvas_tab_name" gorm:"default:''"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	CacheTokens      int    `json:"cache_tokens" gorm:"default:0"`       // cache read (Cache discount)
	CacheWriteTokens int    `json:"cache_write_tokens" gorm:"default:0"` // cache write (Cache write)
	ChannelName      string `json:"channel_name" gorm:"-"`
	SyncedAt         int64  `json:"synced_at" gorm:"default:0"` // unix timestamp when this record was synced
}

// GetPoeLogLatestQueryId returns the query_id of the most recent PoeLog entry
// for a given channel, used as a pagination cursor for the next sync.
func GetPoeLogLatestQueryId(channelId int) (string, error) {
	var poeLog PoeLog
	err := DB.Where("channel_id = ?", channelId).
		Order("creation_time DESC").
		Limit(1).
		Find(&poeLog).Error
	if err != nil {
		return "", err
	}
	return poeLog.QueryId, nil
}

// BulkCreatePoeLogsIgnoreDuplicates inserts PoeLog entries, ignoring duplicates.
// Returns the number of rows actually inserted.
func BulkCreatePoeLogsIgnoreDuplicates(entries []*PoeLog) (int64, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	result := DB.
		Where("1=1").
		Omit("id").
		CreateInBatches(entries, 100)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// QueryPoeLogsParams holds filter parameters for querying PoeLog records.
type QueryPoeLogsParams struct {
	ChannelId      int
	BotName        string
	UsageType      string
	StartTimestamp int64
	EndTimestamp   int64
	PaidOnly       bool
	StartIdx       int
	Num            int
}

// GetPoeLogs returns a page of PoeLog records matching the given filters.
func GetPoeLogs(params QueryPoeLogsParams) ([]*PoeLog, int64, error) {
	tx := DB.Model(&PoeLog{})
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.BotName != "" {
		tx = tx.Where("bot_name = ?", strings.ToLower(params.BotName))
	}
	if params.UsageType != "" {
		tx = tx.Where("usage_type = ?", params.UsageType)
	}
	if params.PaidOnly {
		tx = tx.Where("cost_points != 0")
	}
	if params.StartTimestamp != 0 {
		// Convert unix seconds to microseconds for comparison
		tx = tx.Where("creation_time >= ?", params.StartTimestamp*1_000_000)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("creation_time <= ?", params.EndTimestamp*1_000_000)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*PoeLog
	if err := tx.Order("creation_time DESC").
		Limit(params.Num).
		Offset(params.StartIdx).
		Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// PoeLogStats holds aggregated statistics for PoeLog queries.
type PoeLogStats struct {
	TotalPoints           int64  `json:"total_points"`
	TotalUsd              string `json:"total_usd"`
	Count                 int64  `json:"count"`
	TotalPromptTokens     int64  `json:"total_prompt_tokens"`
	TotalCompletionTokens int64  `json:"total_completion_tokens"`
	TotalCacheTokens      int64  `json:"total_cache_tokens"`
	TotalCacheWriteTokens int64  `json:"total_cache_write_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
}

// GetPoeLogStats returns aggregated statistics for PoeLog records matching the given filters.
func GetPoeLogStats(channelId int, startTimestamp, endTimestamp int64, paidOnly bool) (PoeLogStats, error) {
	type result struct {
		TotalPoints           int64   `gorm:"column:total_points"`
		TotalCostUsd          float64 `gorm:"column:total_cost_usd"`
		Count                 int64   `gorm:"column:cnt"`
		TotalPromptTokens     int64   `gorm:"column:total_prompt_tokens"`
		TotalCompletionTokens int64   `gorm:"column:total_completion_tokens"`
		TotalCacheTokens      int64   `gorm:"column:total_cache_tokens"`
		TotalCacheWriteTokens int64   `gorm:"column:total_cache_write_tokens"`
	}

	tx := DB.Model(&PoeLog{})
	if channelId != 0 {
		tx = tx.Where("channel_id = ?", channelId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("creation_time >= ?", startTimestamp*1_000_000)
	}
	if endTimestamp != 0 {
		tx = tx.Where("creation_time <= ?", endTimestamp*1_000_000)
	}
	if paidOnly {
		tx = tx.Where("cost_points != 0")
	}

	var r result
	selectExpr := "SUM(cost_points) AS total_points, COUNT(*) AS cnt, " +
		"SUM(prompt_tokens) AS total_prompt_tokens, " +
		"SUM(completion_tokens) AS total_completion_tokens, " +
		"SUM(cache_tokens) AS total_cache_tokens, " +
		"SUM(cache_write_tokens) AS total_cache_write_tokens"
	if common.UsingPostgreSQL {
		selectExpr += ", SUM(cost_usd::numeric) AS total_cost_usd"
	} else {
		selectExpr += ", SUM(CAST(cost_usd AS DECIMAL(20,10))) AS total_cost_usd"
	}
	if err := tx.Select(selectExpr).Scan(&r).Error; err != nil {
		return PoeLogStats{}, err
	}

	return PoeLogStats{
		TotalPoints:           r.TotalPoints,
		TotalUsd:             fmt.Sprintf("%.6f", r.TotalCostUsd),
		Count:                 r.Count,
		TotalPromptTokens:     r.TotalPromptTokens,
		TotalCompletionTokens: r.TotalCompletionTokens,
		TotalCacheTokens:      r.TotalCacheTokens,
		TotalCacheWriteTokens: r.TotalCacheWriteTokens,
		TotalTokens:           r.TotalPromptTokens + r.TotalCompletionTokens,
	}, nil
}

// PoeLogSyncState stores the last successful sync time per channel for the periodic task.
type PoeLogSyncState struct {
	ChannelId    int   `json:"channel_id" gorm:"primaryKey"`
	LastSyncedAt int64 `json:"last_synced_at" gorm:"default:0"`
}

// UpsertPoeLogSyncState saves or updates the sync state for a channel.
func UpsertPoeLogSyncState(channelId int) error {
	state := PoeLogSyncState{
		ChannelId:    channelId,
		LastSyncedAt: time.Now().Unix(),
	}
	return DB.Save(&state).Error
}

// GetPoeLogSyncState returns the sync state for a channel.
// Returns an empty state (LastSyncedAt=0) if none exists yet.
func GetPoeLogSyncState(channelId int) (PoeLogSyncState, error) {
	var state PoeLogSyncState
	err := DB.Where("channel_id = ?", channelId).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PoeLogSyncState{ChannelId: channelId}, nil
	}
	return state, err
}

// LogPoeQuotaData writes PoeLog data into the quota_data cache used by the dashboard.
// This mirrors LogQuotaData but for Poe-sourced records, so dashboard reads from the
// pre-aggregated quota_data table instead of doing full table scans on poe_logs.
func LogPoeQuotaData(channelId int, modelName string, costPoints int, createdAt int64, tokenUsed int) {
	if !common.DataExportEnabled {
		return
	}
	LogQuotaData(0, "", modelName, costPoints, createdAt, tokenUsed)
}

var poeChannelIdsCache struct {
	sync.RWMutex
	m map[string][]int
}

func init() {
	poeChannelIdsCache.m = make(map[string][]int)
}

// getChannelIdsByUsername looks up channel IDs owned by a user through their tokens.
// Results are cached to avoid repeated JOINs on every dashboard request.
func getChannelIdsByUsername(username string) ([]int, error) {
	poeChannelIdsCache.RLock()
	if ids, ok := poeChannelIdsCache.m[username]; ok {
		poeChannelIdsCache.RUnlock()
		return ids, nil
	}
	poeChannelIdsCache.RUnlock()

	var channelIds []int
	err := DB.Model(&Channel{}).
		Joins("JOIN tokens ON tokens.channel_id = channels.id").
		Where("tokens.name = ?", username).
		Distinct("channels.id").
		Pluck("channels.id", &channelIds).Error
	if err != nil {
		return nil, err
	}

	poeChannelIdsCache.Lock()
	poeChannelIdsCache.m[username] = channelIds
	poeChannelIdsCache.Unlock()

	return channelIds, nil
}

func InvalidatePoeChannelIdsCache() {
	poeChannelIdsCache.Lock()
	poeChannelIdsCache.m = make(map[string][]int)
	poeChannelIdsCache.Unlock()
}

// PoeLogTokenDistributionData mirrors TokenDistributionData for PoeLog aggregation.
type PoeLogTokenDistributionData struct {
	CreatedAt        int64  `json:"created_at" gorm:"column:created_at"`
	ModelName        string `json:"model_name" gorm:"column:model_name"`
	InputTokens      int64  `json:"input_tokens" gorm:"column:total_prompt_tokens"`
	OutputTokens     int64  `json:"output_tokens" gorm:"column:total_completion_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens" gorm:"column:total_cache_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens" gorm:"column:total_cache_write_tokens"`
	Count            int    `json:"count" gorm:"column:cnt"`
}

// GetPoeLogTokenDistribution aggregates PoeLog records into hourly buckets by bot_name,
// returning data compatible with the token distribution format used by dashboard charts.
func GetPoeLogTokenDistribution(startTimestamp, endTimestamp int64, username string) ([]*PoeLogTokenDistributionData, error) {
	tx := DB.Model(&PoeLog{})
	if startTimestamp != 0 {
		tx = tx.Where("creation_time >= ?", startTimestamp*1_000_000)
	}
	if endTimestamp != 0 {
		tx = tx.Where("creation_time <= ?", endTimestamp*1_000_000)
	}
	if username != "" {
		channelIds, err := getChannelIdsByUsername(username)
		if err != nil {
			return nil, err
		}
		if len(channelIds) == 0 {
			return nil, nil
		}
		tx = tx.Where("channel_id IN ?", channelIds)
	}

	hourBucket := fmt.Sprintf("(%s / 1000000 / 3600 * 3600)", poeCreationTimeCol())
	var results []*PoeLogTokenDistributionData
	err := tx.
		Select("bot_name AS model_name, " +
			hourBucket + " AS created_at, " +
			"SUM(prompt_tokens) AS total_prompt_tokens, " +
			"SUM(completion_tokens) AS total_completion_tokens, " +
			"SUM(cache_tokens) AS total_cache_tokens, " +
			"SUM(cache_write_tokens) AS total_cache_write_tokens, " +
			"COUNT(*) AS cnt").
		Group("bot_name, " + hourBucket).
		Find(&results).Error
	return results, err
}

// poeCreationTimeCol returns the quoted column name for creation_time,
// which is needed for PostgreSQL-compatible integer division in GROUP BY.
func poeCreationTimeCol() string {
	if common.UsingPostgreSQL {
		return `"creation_time"`
	}
	return "`creation_time`"
}

func MigratePoeLogBotNameLower() error {
	return DB.Model(&PoeLog{}).
		Where("bot_name != LOWER(bot_name)").
		Update("bot_name", DB.Raw("LOWER(bot_name)")).
		Error
}
