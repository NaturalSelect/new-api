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

// StartTokenWindowQuotaFlushTask restores Redis/memory from the last DB snapshot
// (model.RestoreTokenWindowUsageFromDB), then starts a background goroutine that
// periodically snapshots every active token's rolling 5h/7d quota usage back into the
// tokens table. This closes the persistence gap in model/token_window_quota.go, whose
// counters otherwise only ever live in Redis/memory: without it, a Redis restart/eviction
// or process restart silently resets every token's usage to zero, letting the 5h/7d limit
// be bypassed. Only runs on the master node, matching every other singleton background task
// (see StartSubscriptionQuotaResetTask, StartTokenStatsBackfillTask).
//
// The restore runs synchronously, before this function returns, instead of inside the
// background goroutine: main() calls this before starting the HTTP server, so a synchronous
// restore guarantees every request is served against fully-seeded counters. Running it
// inside the goroutine let it race server startup instead, so early requests after a
// restart could read/bill against an empty window before the seed from DB landed.
func StartTokenWindowQuotaFlushTask() {
	tokenWindowQuotaFlushOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		ctx := context.Background()
		result, err := model.RestoreTokenWindowUsageFromDB()
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("token window quota restore failed: %v", err))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("token window quota restore: candidates=%d restored=%d skipped=%d",
				result.Candidates, result.Restored, len(result.Skips)))
			for _, skip := range result.Skips {
				if skip.Failed {
					logger.LogWarn(ctx, fmt.Sprintf("token window quota restore failed for token=%d window=%s: %s", skip.TokenId, skip.Window, skip.Reason))
				} else {
					logger.LogDebug(ctx, fmt.Sprintf("token window quota restore skipped token=%d window=%s: %s", skip.TokenId, skip.Window, skip.Reason))
				}
			}
		}

		gopool.Go(func() {
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
