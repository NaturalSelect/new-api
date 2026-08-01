package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/cpa_setting"

	"github.com/gin-gonic/gin"
)

// GetCPAUsage returns the locally cached CPA auth-file usage snapshot fetched
// periodically from the configured CPA service, along with the unix
// timestamp of the last successful sync (0 if none has completed yet) and
// whether the CPA integration is configured at all.
func GetCPAUsage(c *gin.Context) {
	usage, updatedAt := service.GetCPAUsage()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"usage":      usage,
			"updated_at": updatedAt,
			"configured": cpa_setting.EnableCPA(),
		},
	})
}

// RefreshCPAUsage forces an immediate fetch from the configured CPA service
// and returns the resulting usage snapshot.
func RefreshCPAUsage(c *gin.Context) {
	usage, updatedAt, err := service.RefreshCPAUsage()
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
		"data": gin.H{
			"usage":      usage,
			"updated_at": updatedAt,
			"configured": true,
		},
	})
}
