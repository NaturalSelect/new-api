package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetAllWarningLogs returns a paginated list of content-policy warning log entries.
// The list omits the large request_body/prompt_text fields — use GetWarningLog for the
// full record. Admin-only endpoint.
func GetAllWarningLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	logs, total, err := model.GetWarningLogs(model.QueryWarningLogsParams{
		UserId:         userId,
		Username:       c.Query("username"),
		TokenName:      c.Query("token_name"),
		ModelName:      c.Query("model_name"),
		ChannelId:      channelId,
		Group:          c.Query("group"),
		MatchedKeyword: c.Query("matched_keyword"),
		RequestId:      c.Query("request_id"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		StartIdx:       pageInfo.GetStartIdx(),
		Num:            pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

// GetWarningLog returns a single warning log entry with the complete request body and
// prompt text. Admin-only endpoint.
func GetWarningLog(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid id")
		return
	}
	warningLog, err := model.GetWarningLogById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, warningLog)
}

// DeleteOldWarningLogs deletes warning log entries created before the given timestamp.
// Admin-only endpoint.
func DeleteOldWarningLogs(c *gin.Context) {
	targetTimestamp, err := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if err != nil || targetTimestamp <= 0 {
		common.ApiErrorMsg(c, "target_timestamp is required")
		return
	}
	count, err := model.DeleteOldWarningLogs(targetTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}
