package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	poeLogSyncBatchSize = 200 // batch size for channel queries
	poeLogFetchLimit    = 100 // max entries per Poe API page (API maximum)
)

var (
	poeLogSyncOnce    sync.Once
	poeLogSyncRunning atomic.Bool
)

// poePointsHistoryItem is one entry from the Poe /usage/points_history response.
type poePointsHistoryItem struct {
	BotName       string            `json:"bot_name"`
	CreationTime  int64             `json:"creation_time"` // microseconds
	QueryId       string            `json:"query_id"`
	CostUsd       string            `json:"cost_usd"`
	CostPoints    int               `json:"cost_points"`
	CostBreakdown map[string]string `json:"cost_breakdown_in_points"`
	UsageType     string            `json:"usage_type"`
	ApiKeyName    string            `json:"api_key_name,omitempty"`
	ChatName      string            `json:"chat_name,omitempty"`
	CanvasTabName  string            `json:"canvas_tab_name,omitempty"`
}

// poePointsHistoryResponse is the full response from the Poe /usage/points_history endpoint.
type poePointsHistoryResponse struct {
	HasMore bool                   `json:"has_more"`
	Length  int                    `json:"length"`
	Data    []poePointsHistoryItem `json:"data"`
}

// StartPoeLogSyncTask starts a background goroutine that periodically syncs
// Poe usage history for all enabled Poe channels. Only runs on the master node
// and only when the poe_log_setting.enabled is true.
func StartPoeLogSyncTask() {
	poeLogSyncOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "Poe log sync task started")

			for {
				setting := operation_setting.GetPoeLogSetting()
				if !setting.Enabled {
					time.Sleep(10 * time.Second)
					continue
				}

				intervalSeconds := operation_setting.GetPoeLogSyncIntervalSeconds()
				interval := time.Duration(intervalSeconds) * time.Second

				runPoeLogSyncOnce()

				ticker := time.NewTicker(interval)
				<-ticker.C
				ticker.Stop()
			}
		})
	})
}

// runPoeLogSyncOnce performs one full sync pass across all Poe channels.
// When KeyDeduplicate is enabled, channels sharing the same API key are grouped,
// and only one representative channel fetches the history for that key.
// The results are then assigned to all channels that share the key.
// It is idempotent and safe to call concurrently (protected by atomic flag).
func runPoeLogSyncOnce() {
	if !poeLogSyncRunning.CompareAndSwap(false, true) {
		return
	}
	defer poeLogSyncRunning.Store(false)

	ctx := context.Background()
	setting := operation_setting.GetPoeLogSetting()
	if !setting.Enabled {
		return
	}

	offset := 0
	for {
		var channels []*model.Channel
		err := model.DB.
			Select("id", "name", "key", "status", "base_url", "channel_info").
			Where("type IN (?, ?) AND status = ?",
				constant.ChannelTypePoeOpenAI,
				constant.ChannelTypePoeAnthropic,
				common.ChannelStatusEnabled,
			).
			Order("id asc").
			Limit(poeLogSyncBatchSize).
			Offset(offset).
			Find(&channels).Error
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("poe log sync: query channels failed: %v", err))
			return
		}
		if len(channels) == 0 {
			break
		}
		offset += poeLogSyncBatchSize

		if setting.KeyDeduplicate {
			syncPoeChannelsWithKeyDedup(ctx, channels)
		} else {
			for _, ch := range channels {
				if ch == nil || strings.TrimSpace(ch.Key) == "" {
					continue
				}
				if err := syncPoeChannelLogs(ctx, ch); err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("poe log sync: channel_id=%d name=%s failed: %v", ch.Id, ch.Name, err))
				}
			}
		}
	}
}

// syncPoeChannelsWithKeyDedup groups channels by their normalized API key,
// fetches history once per unique key using the first channel in each group,
// then assigns the fetched entries to all channels sharing that key.
func syncPoeChannelsWithKeyDedup(ctx context.Context, channels []*model.Channel) {
	type keyGroup struct {
		key       string
		channels  []*model.Channel
		represent *model.Channel
	}

	groups := make(map[string]*keyGroup)
	var groupOrder []string

	for _, ch := range channels {
		if ch == nil {
			continue
		}
		normalizedKey := strings.TrimSpace(ch.Key)
		if normalizedKey == "" {
			continue
		}

		g, exists := groups[normalizedKey]
		if !exists {
			g = &keyGroup{
				key:       normalizedKey,
				represent: ch,
			}
			groups[normalizedKey] = g
			groupOrder = append(groupOrder, normalizedKey)
		}
		g.channels = append(g.channels, ch)
	}

	for _, keyStr := range groupOrder {
		g := groups[keyStr]
		rep := g.represent

		entries, err := fetchPoeLogEntries(ctx, rep)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("poe log sync (key dedup): key=***%s rep_channel_id=%d failed: %v", keySuffix(keyStr, 8), rep.Id, err))
			continue
		}

		if len(entries) == 0 {
			continue
		}

		for _, ch := range g.channels {
			var channelEntries []*model.PoeLog
			syncedAt := time.Now().Unix()
			for _, entry := range entries {
				channelEntry := *entry
				channelEntry.ChannelId = ch.Id
				channelEntry.SyncedAt = syncedAt
				channelEntries = append(channelEntries, &channelEntry)
			}

			inserted, err := model.BulkCreatePoeLogsIgnoreDuplicates(channelEntries)
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("poe log sync (key dedup): channel_id=%d name=%s bulk insert failed: %v", ch.Id, ch.Name, err))
				continue
			}
			if err := model.UpsertPoeLogSyncState(ch.Id); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("poe log sync (key dedup): update sync state for channel_id=%d failed: %v", ch.Id, err))
			}
			if inserted > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("poe log sync (key dedup): channel_id=%d name=%s synced %d new entries", ch.Id, ch.Name, inserted))
			}
		}
	}
}

// keySuffix returns the last n characters of the key for logging (avoid leaking full key).
func keySuffix(key string, n int) string {
	if len(key) <= n {
		return key
	}
	return key[len(key)-n:]
}

// fetchPoeLogEntries fetches all new usage entries from the Poe API for the given
// channel and returns them as PoeLog objects (with ChannelId=0, to be set by caller).
// This is used by the key-dedup path where entries are shared across channels.
func fetchPoeLogEntries(ctx context.Context, channel *model.Channel) ([]*model.PoeLog, error) {
	baseURL := strings.TrimSuffix(channel.GetBaseURL(), "/")
	if baseURL == "" {
		baseURL = "https://api.poe.com"
	}
	apiKey := strings.TrimSpace(channel.Key)

	// NOTE: Fetch the most recent stored query_id as the stop cursor.
	latestQueryId, err := model.GetPoeLogLatestQueryId(channel.Id)
	if err != nil {
		return nil, fmt.Errorf("get latest query_id: %w", err)
	}

	var collected []*model.PoeLog
	startingAfter := ""

outer:
	for {
		resp, err := fetchPoePointsHistory(ctx, baseURL, apiKey, startingAfter, poeLogFetchLimit, channel)
		if err != nil {
			return nil, fmt.Errorf("fetch points history (after=%s): %w", startingAfter, err)
		}
		if len(resp.Data) == 0 {
			break
		}

		for _, item := range resp.Data {
			if item.QueryId == latestQueryId {
				break outer
			}
			breakdownJSON := ""
			if len(item.CostBreakdown) > 0 {
				if b, err2 := common.Marshal(item.CostBreakdown); err2 == nil {
					breakdownJSON = string(b)
				}
			}
			collected = append(collected, &model.PoeLog{
				QueryId:       item.QueryId,
				BotName:       item.BotName,
				CreationTime:  item.CreationTime,
				CostUsd:       item.CostUsd,
				CostPoints:    item.CostPoints,
				CostBreakdown: breakdownJSON,
				UsageType:     item.UsageType,
				ApiKeyName:    item.ApiKeyName,
				ChatName:      item.ChatName,
				CanvasTabName: item.CanvasTabName,
			})
		}

		if !resp.HasMore {
			break
		}
		startingAfter = resp.Data[len(resp.Data)-1].QueryId
	}

	return collected, nil
}

// SyncPoeChannelLogsById syncs Poe usage logs for a single channel identified by ID.
// This is exposed for manual trigger via the admin API.
func SyncPoeChannelLogsById(channelId int) error {
	var ch model.Channel
	if err := model.DB.First(&ch, channelId).Error; err != nil {
		return fmt.Errorf("channel %d not found: %w", channelId, err)
	}
	if ch.Type != constant.ChannelTypePoeOpenAI && ch.Type != constant.ChannelTypePoeAnthropic {
		return fmt.Errorf("channel %d is not a Poe channel (type %d)", channelId, ch.Type)
	}
	return syncPoeChannelLogs(context.Background(), &ch)
}

// syncPoeChannelLogs fetches all new usage entries from the Poe API for the given
// channel and persists them into poe_logs. Used in the non-dedup path.
func syncPoeChannelLogs(ctx context.Context, channel *model.Channel) error {
	entries, err := fetchPoeLogEntries(ctx, channel)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("poe log sync: channel_id=%d name=%s no new entries", channel.Id, channel.Name))
		return nil
	}

	syncedAt := time.Now().Unix()
	for _, entry := range entries {
		entry.ChannelId = channel.Id
		entry.SyncedAt = syncedAt
	}

	inserted, err := model.BulkCreatePoeLogsIgnoreDuplicates(entries)
	if err != nil {
		return fmt.Errorf("bulk insert: %w", err)
	}

	if err := model.UpsertPoeLogSyncState(channel.Id); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("poe log sync: update sync state for channel_id=%d failed: %v", channel.Id, err))
	}

	logger.LogInfo(ctx, fmt.Sprintf("poe log sync: channel_id=%d name=%s synced %d new entries (fetched %d)", channel.Id, channel.Name, inserted, len(entries)))
	return nil
}

// fetchPoePointsHistory calls GET /usage/points_history on the Poe API.
func fetchPoePointsHistory(ctx context.Context, baseURL, apiKey, startingAfter string, limit int, channel *model.Channel) (*poePointsHistoryResponse, error) {
	url := fmt.Sprintf("%s/usage/points_history?limit=%d", baseURL, limit)
	if startingAfter != "" {
		url += "&starting_after=" + startingAfter
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	proxyURL := ""
	if channel != nil {
		proxyURL = channel.GetSetting().Proxy
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result poePointsHistoryResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &result, nil
}
