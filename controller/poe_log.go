package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// GetAllPoeLogs returns a paginated list of Poe log entries.
// Admin-only endpoint.
func GetAllPoeLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	botName := strings.ToLower(c.Query("bot_name"))
	usageType := c.Query("usage_type")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	paidOnly := c.Query("paid_only") == "true"

	logs, total, err := model.GetPoeLogs(model.QueryPoeLogsParams{
		ChannelId:      channelId,
		BotName:        botName,
		UsageType:      usageType,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		PaidOnly:       paidOnly,
		StartIdx:       pageInfo.GetStartIdx(),
		Num:            pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channelNames := model.GetChannelNamesByIds(lo.Map(logs, func(l *model.PoeLog, _ int) int {
		return l.ChannelId
	}))
	for _, l := range logs {
		l.ChannelName = channelNames[l.ChannelId]
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

// GetPoeLogStats returns aggregated statistics (total points, count) for Poe logs.
// Admin-only endpoint.
func GetPoeLogStats(c *gin.Context) {
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	paidOnly := c.Query("paid_only") == "true"

	stats, err := model.GetPoeLogStats(channelId, startTimestamp, endTimestamp, paidOnly)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// TriggerPoeLogSync triggers an immediate sync for a single Poe channel.
// Admin-only endpoint.
// POST /api/poe_log/sync  body: { "channel_id": 123 }
func TriggerPoeLogSync(c *gin.Context) {
	var req struct {
		ChannelId int `json:"channel_id" form:"channel_id"`
	}
	if err := c.ShouldBind(&req); err != nil || req.ChannelId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "channel_id is required",
		})
		return
	}

	if err := service.SyncPoeChannelLogsById(req.ChannelId); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "sync completed",
	})
}

// NOTE: ClearPoeLogs clears Poe log entries to allow re-syncing from scratch.
// NOTE: When channel_id is 0 or omitted, all Poe logs are cleared.
func ClearPoeLogs(c *gin.Context) {
	var req struct {
		ChannelId int `json:"channel_id" form:"channel_id"`
	}
	if err := c.ShouldBind(&req); err != nil {
		common.ApiError(c, fmt.Errorf("invalid request: %w", err))
		return
	}
	deleted, err := model.ClearPoeLogs(req.ChannelId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "cleared",
		"data": gin.H{
			"deleted": deleted,
		},
	})
}
