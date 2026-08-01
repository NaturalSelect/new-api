package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	intelligenceSourceURL    = "https://codexradar.com/data/intelligence-efficiency.json?refresh=1"
	intelligenceHTTPTimeout  = 30 * time.Second
	intelligenceMaxBodyBytes = 10 << 20 // 10MB
)

// IntelligenceScore is one model+effort combo's benchmark result, as reported
// by the external intelligence-efficiency API. The IQ score comes pre-computed
// from upstream; no local weighting/aggregation is performed.
type IntelligenceScore struct {
	Model             string  `json:"model"`
	Effort            string  `json:"effort"`
	IQ                float64 `json:"iq"`
	Passed            int     `json:"passed"`
	ValidTasks        int     `json:"valid_tasks"`
	AveragePriceUSD   float64 `json:"average_price_usd"`
	AverageMinutes    float64 `json:"average_minutes"`
	CombinedCostIndex float64 `json:"combined_cost_index"`
	LatestGradedAt    string  `json:"latest_graded_at"`
}

// intelligenceAPIResponse mirrors the subset of fields consumed from
// https://codexradar.com/data/intelligence-efficiency.json
type intelligenceAPIResponse struct {
	SourceUpdatedAt string              `json:"source_updated_at"`
	Points          []IntelligenceScore `json:"points"`
}

var (
	intelligenceCacheMutex     sync.RWMutex
	intelligenceCacheScores    []IntelligenceScore
	intelligenceCacheUpdatedAt int64

	intelligenceSyncOnce sync.Once
)

// GetIntelligenceScores returns the most recently fetched model intelligence
// scores and the unix timestamp of that fetch. Both are zero-valued until the
// first successful sync completes.
func GetIntelligenceScores() ([]IntelligenceScore, int64) {
	intelligenceCacheMutex.RLock()
	defer intelligenceCacheMutex.RUnlock()
	return intelligenceCacheScores, intelligenceCacheUpdatedAt
}

// StartIntelligenceSyncTask starts a background goroutine that periodically
// fetches model intelligence scores from the external benchmark API and keeps
// them in a process-local in-memory cache.
//
// NOTE: unlike most periodic tasks in this codebase, this deliberately runs on
// every node rather than master-only. The cache is per-process memory (not
// shared/persisted state), so each instance must independently populate its
// own copy for its own API responses to be correct.
func StartIntelligenceSyncTask() {
	intelligenceSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "Intelligence score sync task started")

			for {
				if !operation_setting.IsIntelligenceSyncEnabled() {
					time.Sleep(10 * time.Second)
					continue
				}

				runIntelligenceSyncOnce()

				interval := time.Duration(operation_setting.GetIntelligenceRefreshIntervalMinutes()) * time.Minute
				time.Sleep(interval)
			}
		})
	})
}

// runIntelligenceSyncOnce performs one fetch-parse-cache pass. Errors are
// logged and swallowed so a flaky/unavailable third-party API never affects
// the rest of the application.
func runIntelligenceSyncOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), intelligenceHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, intelligenceSourceURL, nil)
	if err != nil {
		logger.LogError(ctx, "intelligence sync: build request failed: "+err.Error())
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.LogError(ctx, "intelligence sync: request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, "intelligence sync: unexpected status code "+resp.Status)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, intelligenceMaxBodyBytes))
	if err != nil {
		logger.LogError(ctx, "intelligence sync: read body failed: "+err.Error())
		return
	}

	var parsed intelligenceAPIResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		logger.LogError(ctx, "intelligence sync: unmarshal failed: "+err.Error())
		return
	}

	intelligenceCacheMutex.Lock()
	intelligenceCacheScores = parsed.Points
	intelligenceCacheUpdatedAt = time.Now().Unix()
	intelligenceCacheMutex.Unlock()
}
