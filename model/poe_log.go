package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// PoeLog stores Poe API usage records synced from the Poe usage history endpoint.
// Each entry corresponds to one query charged on the Poe account.
type PoeLog struct {
	Id            int    `json:"id"`
	ChannelId     int    `json:"channel_id" gorm:"index"`
	QueryId       string `json:"query_id" gorm:"uniqueIndex;type:varchar(64)"`
	BotName       string `json:"bot_name" gorm:"index;default:''"`
	CreationTime  int64  `json:"creation_time" gorm:"index"` // microseconds (from Poe API)
	CostUsd       string `json:"cost_usd" gorm:"default:''"`
	CostPoints    int    `json:"cost_points" gorm:"default:0"`
	CostBreakdown string `json:"cost_breakdown"` // JSON string of cost_breakdown_in_points
	UsageType     string `json:"usage_type" gorm:"default:''"` // Chat, API, Canvas App
	ApiKeyName    string `json:"api_key_name" gorm:"default:''"`
	ChatName      string `json:"chat_name" gorm:"default:''"`
	CanvasTabName string `json:"canvas_tab_name" gorm:"default:''"`
	PromptTokens      int `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int `json:"completion_tokens" gorm:"default:0"`
	CacheTokens       int `json:"cache_tokens" gorm:"default:0"`       // cache read (Cache discount)
	CacheWriteTokens  int `json:"cache_write_tokens" gorm:"default:0"` // cache write (Cache write)
	SyncedAt      int64  `json:"synced_at" gorm:"default:0"` // unix timestamp when this record was synced
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
		tx = tx.Where("bot_name = ?", params.BotName)
	}
	if params.UsageType != "" {
		tx = tx.Where("usage_type = ?", params.UsageType)
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
	TotalPoints int64  `json:"total_points"`
	TotalUsd    string `json:"total_usd"`
	Count       int64  `json:"count"`
}

// GetPoeLogStats returns aggregated statistics for PoeLog records matching the given filters.
func GetPoeLogStats(channelId int, startTimestamp, endTimestamp int64) (PoeLogStats, error) {
	type result struct {
		TotalPoints int64 `gorm:"column:total_points"`
		Count       int64 `gorm:"column:cnt"`
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

	var r result
	if err := tx.Select("SUM(cost_points) AS total_points, COUNT(*) AS cnt").Scan(&r).Error; err != nil {
		return PoeLogStats{}, err
	}

	return PoeLogStats{
		TotalPoints: r.TotalPoints,
		Count:       r.Count,
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

// PoeLogQuotaData mirrors QuotaData for PoeLog-based dashboard aggregation.
type PoeLogQuotaData struct {
	ModelName string `json:"model_name" gorm:"column:model_name"`
	CreatedAt int64  `json:"created_at" gorm:"column:created_at"`
	Count     int64  `json:"count" gorm:"column:cnt"`
	Quota     int64  `json:"quota" gorm:"column:total_cost_points"`
	TokenUsed int64  `json:"token_used" gorm:"column:total_tokens"`
}

// GetPoeLogQuotaData aggregates PoeLog records into hourly buckets by bot_name,
// returning data compatible with the quota_data table format used by dashboard charts.
// cost_points is treated as quota, token_used is the sum of all token fields.
func GetPoeLogQuotaData(startTimestamp, endTimestamp int64, username string) ([]*PoeLogQuotaData, error) {
	if !common.DataExportEnabled {
		return nil, nil
	}

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
	var results []*PoeLogQuotaData
	err := tx.
		Select("bot_name AS model_name, "+
			hourBucket+" AS created_at, "+
			"COUNT(*) AS cnt, "+
			"SUM(cost_points) AS total_cost_points, "+
			"SUM(prompt_tokens + completion_tokens + cache_tokens + cache_write_tokens) AS total_tokens").
		Group("bot_name, "+hourBucket).
		Find(&results).Error
	return results, err
}

// getChannelIdsByUsername looks up channel IDs owned by a user through their tokens.
func getChannelIdsByUsername(username string) ([]int, error) {
	var channelIds []int
	err := DB.Model(&Channel{}).
		Joins("JOIN tokens ON tokens.channel_id = channels.id").
		Where("tokens.name = ?", username).
		Distinct("channels.id").
		Pluck("channels.id", &channelIds).Error
	if err != nil {
		return nil, err
	}
	return channelIds, nil
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
		Select("bot_name AS model_name, "+
			hourBucket+" AS created_at, "+
			"SUM(prompt_tokens) AS total_prompt_tokens, "+
			"SUM(completion_tokens) AS total_completion_tokens, "+
			"SUM(cache_tokens) AS total_cache_tokens, "+
			"SUM(cache_write_tokens) AS total_cache_write_tokens, "+
			"COUNT(*) AS cnt").
		Group("bot_name, "+hourBucket).
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
