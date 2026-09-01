package model

import (
	"github.com/QuantumNous/new-api/common"
)

// WarningLog stores requests that upstream providers rejected for violating their
// content/usage policy (e.g. "your prompt was flagged as potentially violating our
// usage policy"), keeping the complete original request body and prompt text so admins
// can locate exactly which user/content triggered the rejection. Nothing here is ever
// truncated — see migrateWarningLogColumnsToLongText in main.go for how the large text
// columns stay unbounded on MySQL too. See service.MatchContentPolicyWarning for the
// detection logic and setting/operation_setting.ContentPolicyWarningKeywords for the
// keyword list.
type WarningLog struct {
	Id                int    `json:"id"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
	UserId            int    `json:"user_id" gorm:"index"`
	Username          string `json:"username" gorm:"index;default:''"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	TokenName         string `json:"token_name" gorm:"default:''"`
	ChannelId         int    `json:"channel_id" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"default:''"`
	ChannelType       int    `json:"channel_type" gorm:"default:0"`
	ModelName         string `json:"model_name" gorm:"index;default:''"`
	Group             string `json:"group" gorm:"index;default:''"`
	Ip                string `json:"ip" gorm:"default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);default:''"`
	StatusCode        int    `json:"status_code" gorm:"default:0"`
	ErrorCode         string `json:"error_code" gorm:"type:varchar(64);default:''"`
	MatchedKeyword    string `json:"matched_keyword" gorm:"type:varchar(255);index;default:''"`
	ErrorMessage      string `json:"error_message" gorm:"type:text"`
	RequestPath       string `json:"request_path" gorm:"type:varchar(255);default:''"`
	// PromptText is the extracted prompt (same source as sensitive-word checking),
	// kept alongside RequestBody purely for quick review without parsing the raw JSON.
	PromptText  string `json:"prompt_text" gorm:"type:text"`
	RequestBody string `json:"request_body" gorm:"type:text"`
	BodySize    int64  `json:"body_size" gorm:"default:0"`
	Other       string `json:"other" gorm:"type:text"`
}

// RecordWarningLog persists a content-policy warning entry. Intended to be called from
// a goroutine (see controller.recordContentPolicyWarning) after all needed fields have
// already been read from the request context, since LOG_DB.Create blocks on I/O.
func RecordWarningLog(log *WarningLog) {
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysError("failed to record warning log: " + err.Error())
	}
}

// QueryWarningLogsParams holds filter parameters for querying WarningLog records.
type QueryWarningLogsParams struct {
	UserId         int
	Username       string
	TokenName      string
	ModelName      string
	ChannelId      int
	Group          string
	MatchedKeyword string
	RequestId      string
	StartTimestamp int64
	EndTimestamp   int64
	StartIdx       int
	Num            int
}

// GetWarningLogs returns a page of WarningLog records matching the given filters.
// RequestBody and PromptText are omitted from the result since they can be large —
// callers needing the full request must fetch a single record via GetWarningLogById.
func GetWarningLogs(params QueryWarningLogsParams) ([]*WarningLog, int64, error) {
	tx := LOG_DB.Model(&WarningLog{})
	if params.UserId != 0 {
		tx = tx.Where("user_id = ?", params.UserId)
	}
	if params.Username != "" {
		tx = tx.Where("username = ?", params.Username)
	}
	if params.TokenName != "" {
		tx = tx.Where("token_name = ?", params.TokenName)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", params.Group)
	}
	if params.MatchedKeyword != "" {
		tx = tx.Where("matched_keyword = ?", params.MatchedKeyword)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*WarningLog
	if err := tx.Omit("request_body", "prompt_text").
		Order("id desc").
		Limit(params.Num).
		Offset(params.StartIdx).
		Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetWarningLogById returns a single WarningLog with all fields, including the complete
// request body and prompt text, for detail inspection.
func GetWarningLogById(id int) (*WarningLog, error) {
	var log WarningLog
	if err := LOG_DB.Where("id = ?", id).First(&log).Error; err != nil {
		return nil, err
	}
	return &log, nil
}

// DeleteOldWarningLogs deletes WarningLog records created before targetTimestamp.
func DeleteOldWarningLogs(targetTimestamp int64) (int64, error) {
	result := LOG_DB.Where("created_at < ?", targetTimestamp).Delete(&WarningLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
