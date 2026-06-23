package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := dates
	if operation_setting.IsPoeLogSyncEnabled() {
		poeData, poeErr := model.GetPoeLogQuotaData(startTimestamp, endTimestamp, username)
		if poeErr == nil && len(poeData) > 0 {
			result = mergeQuotaData(dates, poeData)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
	return
}

func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if endTimestamp-startTimestamp > 2592000 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := dates
	if operation_setting.IsPoeLogSyncEnabled() {
		poeData, poeErr := model.GetPoeLogQuotaData(startTimestamp, endTimestamp, "")
		if poeErr == nil && len(poeData) > 0 {
			result = mergeQuotaDataByUser(dates, poeData)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
	return
}

func GetTokenDistribution(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	poeLogEnabled := operation_setting.IsPoeLogSyncEnabled()

	var dates []*model.TokenDistributionData
	var err error
	if poeLogEnabled {
		dates, err = model.GetTokenDistributionWithPoe(startTimestamp, endTimestamp, username, false)
	} else {
		dates, err = model.GetTokenDistribution(startTimestamp, endTimestamp, username)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := dates
	if poeLogEnabled {
		poeData, poeErr := model.GetPoeLogTokenDistribution(startTimestamp, endTimestamp, username)
		if poeErr == nil && len(poeData) > 0 {
			result = mergeTokenDistribution(dates, poeData)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

func GetSelfTokenDistribution(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.GetString("username")
	poeLogEnabled := operation_setting.IsPoeLogSyncEnabled()

	var dates []*model.TokenDistributionData
	var err error
	if poeLogEnabled {
		dates, err = model.GetTokenDistributionWithPoe(startTimestamp, endTimestamp, username, false)
	} else {
		dates, err = model.GetTokenDistribution(startTimestamp, endTimestamp, username)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	result := dates
	if poeLogEnabled {
		poeData, poeErr := model.GetPoeLogTokenDistribution(startTimestamp, endTimestamp, username)
		if poeErr == nil && len(poeData) > 0 {
			result = mergeTokenDistribution(dates, poeData)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

func mergeTokenDistribution(logData []*model.TokenDistributionData, poeData []*model.PoeLogTokenDistributionData) []*model.TokenDistributionData {
	type key struct {
		CreatedAt int64
		ModelName string
	}
	merged := make(map[key]*model.TokenDistributionData)

	for _, item := range logData {
		k := key{CreatedAt: item.CreatedAt, ModelName: item.ModelName}
		merged[k] = item
	}
	for _, item := range poeData {
		k := key{CreatedAt: item.CreatedAt, ModelName: item.ModelName}
		if existing, ok := merged[k]; ok {
			existing.InputTokens += item.InputTokens
			existing.OutputTokens += item.OutputTokens
			existing.CacheReadTokens += item.CacheReadTokens
			existing.CacheWriteTokens += item.CacheWriteTokens
			existing.Count += item.Count
		} else {
			merged[k] = &model.TokenDistributionData{
				CreatedAt:        item.CreatedAt,
				ModelName:        item.ModelName,
				InputTokens:      item.InputTokens,
				OutputTokens:     item.OutputTokens,
				CacheReadTokens:  item.CacheReadTokens,
				CacheWriteTokens: item.CacheWriteTokens,
				Count:            item.Count,
			}
		}
	}

	result := make([]*model.TokenDistributionData, 0, len(merged))
	for _, item := range merged {
		result = append(result, item)
	}
	return result
}

func mergeQuotaData(logData []*model.QuotaData, poeData []*model.PoeLogQuotaData) []*model.QuotaData {
	type key struct {
		ModelName string
		CreatedAt int64
	}
	merged := make(map[key]*model.QuotaData)

	for _, item := range logData {
		k := key{ModelName: item.ModelName, CreatedAt: item.CreatedAt}
		merged[k] = item
	}
	for _, item := range poeData {
		k := key{ModelName: item.ModelName, CreatedAt: item.CreatedAt}
		if existing, ok := merged[k]; ok {
			existing.Count += int(item.Count)
			existing.Quota += int(item.Quota)
			existing.TokenUsed += int(item.TokenUsed)
		} else {
			merged[k] = &model.QuotaData{
				ModelName: item.ModelName,
				CreatedAt: item.CreatedAt,
				Count:     int(item.Count),
				Quota:     int(item.Quota),
				TokenUsed: int(item.TokenUsed),
			}
		}
	}

	result := make([]*model.QuotaData, 0, len(merged))
	for _, item := range merged {
		result = append(result, item)
	}
	return result
}

func mergeQuotaDataByUser(logData []*model.QuotaData, poeData []*model.PoeLogQuotaData) []*model.QuotaData {
	type key struct {
		Username  string
		CreatedAt int64
	}
	merged := make(map[key]*model.QuotaData)

	for _, item := range logData {
		k := key{Username: item.Username, CreatedAt: item.CreatedAt}
		merged[k] = item
	}
	for _, item := range poeData {
		k := key{Username: "", CreatedAt: item.CreatedAt}
		if existing, ok := merged[k]; ok {
			existing.Count += int(item.Count)
			existing.Quota += int(item.Quota)
			existing.TokenUsed += int(item.TokenUsed)
		} else {
			merged[k] = &model.QuotaData{
				Username:  "",
				ModelName: item.ModelName,
				CreatedAt: item.CreatedAt,
				Count:     int(item.Count),
				Quota:     int(item.Quota),
				TokenUsed: int(item.TokenUsed),
			}
		}
	}

	result := make([]*model.QuotaData, 0, len(merged))
	for _, item := range merged {
		result = append(result, item)
	}
	return result
}
