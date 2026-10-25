package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetTrafficCaptureRecord returns the decoded capture of one request. Root-only endpoint.
func GetTrafficCaptureRecord(c *gin.Context) {
	requestId := c.Param("request_id")
	if requestId == "" {
		common.ApiErrorMsg(c, "invalid request id")
		return
	}
	view, err := service.GetTrafficCaptureRecord(requestId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorMsg(c, "not captured or expired")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// GetTrafficCaptureStats reports storage usage against the configured retention limits.
func GetTrafficCaptureStats(c *gin.Context) {
	stats, err := service.GetTrafficCaptureStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, stats)
}

// PurgeTrafficCapture deletes every stored capture and returns how many rows were removed.
func PurgeTrafficCapture(c *gin.Context) {
	deleted, err := service.PurgeTrafficCaptures()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, deleted)
}
