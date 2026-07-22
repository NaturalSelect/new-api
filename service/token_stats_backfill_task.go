package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	tokenStatsBackfillTickInterval   = 5 * time.Second
	tokenStatsBackfillMaxHistoryDays = 365
)

var (
	tokenStatsBackfillOnce    sync.Once
	tokenStatsBackfillRunning atomic.Bool
)

// StartTokenStatsBackfillTask starts a background goroutine that incrementally
// backfills TokenStatsCache for historical days, one day per tick, moving the
// watermark (model.GetTokenStatsCacheWatermark) further into the past every time a
// day is durably cached. It stops advancing once the watermark reaches
// tokenStatsBackfillMaxHistoryDays before today — cache reads never depend on the
// backfill having finished; every read still falls back to a raw logs scan for any day
// not yet covered (see the zone split in getTokenDistributionAggregated/
// getKeyDistributionAggregated), so this task only ever affects how fast those reads
// are, never their correctness. Only runs on the master node, matching every other
// singleton background task (see StartSubscriptionQuotaResetTask, StartPoeLogSyncTask).
func StartTokenStatsBackfillTask() {
	tokenStatsBackfillOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("token stats cache backfill task started: tick=%s, max_history_days=%d", tokenStatsBackfillTickInterval, tokenStatsBackfillMaxHistoryDays))
			ticker := time.NewTicker(tokenStatsBackfillTickInterval)
			defer ticker.Stop()

			runTokenStatsBackfillOnce()
			for range ticker.C {
				runTokenStatsBackfillOnce()
			}
		})
	})
}

// runTokenStatsBackfillOnce backfills at most one additional historical day per call:
// the day immediately before the current watermark. It is deliberately one-day-at-a-time
// (rather than draining the whole backlog in one call) so a slow/large logs table
// spreads its load across many ticks instead of blocking on one huge scan.
func runTokenStatsBackfillOnce() {
	if !tokenStatsBackfillRunning.CompareAndSwap(false, true) {
		return
	}
	defer tokenStatsBackfillRunning.Store(false)

	ctx := context.Background()
	today := model.BucketTimestampToDay(common.GetTimestamp())
	watermark := model.GetTokenStatsCacheWatermark()
	oldestAllowed := today - tokenStatsBackfillMaxHistoryDays*86400
	if watermark <= oldestAllowed {
		return
	}

	targetDay := watermark - 86400
	scanned, err := model.BackfillTokenStatsCacheDay(targetDay)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("token stats cache backfill failed for day %d: %v", targetDay, err))
		return
	}
	if err := model.AdvanceTokenStatsCacheWatermark(targetDay); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("token stats cache watermark advance failed for day %d: %v", targetDay, err))
		return
	}
	if common.DebugEnabled {
		logger.LogDebug(ctx, "token stats cache backfilled day=%d logs_scanned=%d watermark=%d", targetDay, scanned, targetDay)
	}
}
