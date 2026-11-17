package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func ocgoUsageData(usage []service.OCGOUsageItem, updatedAt int64) gin.H {
	count, err := model.CountChannelsByType(constant.ChannelTypeOpenCodeGo)
	return gin.H{
		"usage":      usage,
		"updated_at": updatedAt,
		"configured": err == nil && count > 0,
	}
}

// GetOCGOUsage returns the cached OpenCode Go usage snapshot and when it was taken.
func GetOCGOUsage(c *gin.Context) {
	usage, updatedAt := service.GetOCGOUsage()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ocgoUsageData(usage, updatedAt),
	})
}

// RefreshOCGOUsage queries the upstream now and returns the new snapshot.
func RefreshOCGOUsage(c *gin.Context) {
	usage, updatedAt, err := service.RefreshOCGOUsage()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    ocgoUsageData(usage, updatedAt),
	})
}
