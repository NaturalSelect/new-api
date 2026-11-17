package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type ChannelKeyCheckRequest struct {
	Ids []int  `json:"ids"`
	Key string `json:"key"`
}

type ChannelKeyCheckResult struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

// CheckChannelKeyExists reports, for each selected channel, whether the given key is one of its keys.
// Only exact matches are reported and no key material is returned, so it cannot be used to read keys.
func CheckChannelKeyExists(c *gin.Context) {
	req := ChannelKeyCheckRequest{}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Ids) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "参数错误",
		})
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密钥不能为空",
		})
		return
	}

	channels, err := model.GetChannelsByIds(lo.Uniq(req.Ids))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	results := make([]ChannelKeyCheckResult, 0, len(channels))
	for _, channel := range channels {
		results = append(results, ChannelKeyCheckResult{
			Id:     channel.Id,
			Name:   channel.Name,
			Exists: channel.HasKey(req.Key),
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    results,
	})
}
