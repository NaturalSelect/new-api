package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// TokenWindowQuotaLimit enforces the per-token rolling 5h/7d quota limits (0 = unlimited).
// It must run after TokenAuth() so token_id/token_quota_limit_5h/7d are already in context.
func TokenWindowQuotaLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit5h := common.GetContextKeyInt(c, constant.ContextKeyTokenQuotaLimit5h)
		limit7d := common.GetContextKeyInt(c, constant.ContextKeyTokenQuotaLimit7d)
		if limit5h <= 0 && limit7d <= 0 {
			c.Next()
			return
		}

		tokenId := c.GetInt("token_id")

		if limit5h > 0 {
			if blocked := checkTokenWindowQuota(c, tokenId, model.TokenQuotaWindow5h, limit5h); blocked {
				return
			}
		}
		if limit7d > 0 {
			if blocked := checkTokenWindowQuota(c, tokenId, model.TokenQuotaWindow7d, limit7d); blocked {
				return
			}
		}

		c.Next()
	}
}

// checkTokenWindowQuota returns true if the request was aborted because the window quota was exceeded.
// It also exposes the window's used/total quota via response headers so callers can track consumption.
func checkTokenWindowQuota(c *gin.Context, tokenId int, window model.TokenQuotaWindow, limit int) bool {
	usage, err := model.GetTokenWindowUsage(tokenId, window)
	if err != nil {
		common.SysLog("failed to get token window usage: " + err.Error())
		return false
	}

	setTokenWindowQuotaHeaders(c, window, usage.Used, limit)

	if usage.Used < int64(limit) {
		return false
	}

	resetAt := "N/A"
	if usage.ResetAt > 0 {
		resetAt = time.Unix(usage.ResetAt, 0).Format(time.RFC3339)
	}
	message := i18n.T(c, i18n.MsgTokenWindowQuotaExceeded, map[string]any{
		"Window":  window.Name,
		"Used":    usage.Used,
		"Limit":   limit,
		"ResetAt": resetAt,
	})
	abortWithOpenAiMessage(c, http.StatusTooManyRequests, message, types.ErrorCodeTokenWindowQuotaExceeded)
	return true
}

// setTokenWindowQuotaHeaders reports a token's rolling-window usage (e.g. "X-New-Api-Quota-5h-Used",
// "X-New-Api-Quota-5h-Limit") so clients can track consumption without polling the token API.
func setTokenWindowQuotaHeaders(c *gin.Context, window model.TokenQuotaWindow, used int64, limit int) {
	c.Header("X-New-Api-Quota-"+window.Name+"-Used", strconv.FormatInt(used, 10))
	c.Header("X-New-Api-Quota-"+window.Name+"-Limit", strconv.Itoa(limit))
}
