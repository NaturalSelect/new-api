package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const tokenWindowQuotaFlushTickInterval = 1 * time.Minute

var tokenWindowQuotaFlushOnce sync.Once

// StartTokenWindowQuotaFlushTask starts a background goroutine that periodically snapshots
// every active token's rolling 5h/7d quota usage into the tokens table, and once at startup
// restores Redis/memory from the last snapshot (model.RestoreTokenWindowUsageFromDB). This
// closes the persistence gap in model/token_window_quota.go, whose counters otherwise only
// ever live in Redis/memory: without it, a Redis restart/eviction or process restart
// silently resets every token's usage to zero, letting the 5h/7d limit be bypassed. Only
// runs on the master node, matching every other singleton background task (see
// StartSubscriptionQuotaResetTask, StartTokenStatsBackfillTask).
func StartTokenWindowQuotaFlushTask() {
	tokenWindowQuotaFlushOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ctx := context.Background()
			restored, err := model.RestoreTokenWindowUsageFromDB()
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("token window quota restore failed: %v", err))
			} else if restored > 0 {
				logger.LogInfo(ctx, fmt.Sprintf("token window quota restored from last snapshot: tokens=%d", restored))
			}

			logger.LogInfo(ctx, fmt.Sprintf("token window quota flush task started: tick=%s", tokenWindowQuotaFlushTickInterval))
			ticker := time.NewTicker(tokenWindowQuotaFlushTickInterval)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := model.FlushActiveTokenWindowUsage(); err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("token window quota flush failed: %v", err))
				}
			}
		})
	})
}
