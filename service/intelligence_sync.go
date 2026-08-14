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
	intelligenceSourceURL    = "https://codexradar.com/api/intelligence-efficiency-metrics?refresh=1"
	intelligenceHTTPTimeout  = 30 * time.Second
	intelligenceMaxBodyBytes = 10 << 20 // 10MB
)

// IntelligenceScore is one model+effort combo's benchmark result, exposed to
// the frontend via GetIntelligenceScores. This is our own stable API
// contract; its JSON tags do not necessarily match the upstream response
// shape (see intelligencePoint), which is mapped into this type on fetch.
type IntelligenceScore struct {
	Model             string  `json:"model"`
	Effort            string  `json:"effort"`
	IQ                float64 `json:"iq"`
	Passed            float64 `json:"passed"`
	ValidTasks        float64 `json:"valid_tasks"`
	AveragePriceUSD   float64 `json:"average_price_usd"`
	AverageMinutes    float64 `json:"average_minutes"`
	CombinedCostIndex float64 `json:"combined_cost_index"`
	LatestGradedAt    string  `json:"latest_graded_at"`
}

// intelligencePoint mirrors one entry of the "points" array returned by
// https://codexradar.com/api/intelligence-efficiency-metrics. weighted_passed/
// weighted_total are the rolling-weighted counts backing iq; the sibling
// plain passed/total fields are raw non-weighted counts and are not used.
type intelligencePoint struct {
	Model             string  `json:"model"`
	Effort            string  `json:"effort"`
	IQ                float64 `json:"iq"`
	WeightedPassed    float64 `json:"weighted_passed"`
	WeightedTotal     float64 `json:"weighted_total"`
	AveragePriceUSD   float64 `json:"average_price_usd"`
	AverageMinutes    float64 `json:"average_minutes"`
	CombinedCostIndex float64 `json:"combined_cost_index"`
	SourceUpdatedAt   string  `json:"source_updated_at"`
}

// intelligenceAPIResponse mirrors the subset of fields consumed from
// https://codexradar.com/api/intelligence-efficiency-metrics
type intelligenceAPIResponse struct {
	SourceUpdatedAt string              `json:"source_updated_at"`
	Points          []intelligencePoint `json:"points"`
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

	scores := make([]IntelligenceScore, len(parsed.Points))
	for i, p := range parsed.Points {
		scores[i] = IntelligenceScore{
			Model:             p.Model,
			Effort:            p.Effort,
			IQ:                p.IQ,
			Passed:            p.WeightedPassed,
			ValidTasks:        p.WeightedTotal,
			AveragePriceUSD:   p.AveragePriceUSD,
			AverageMinutes:    p.AverageMinutes,
			CombinedCostIndex: p.CombinedCostIndex,
			LatestGradedAt:    p.SourceUpdatedAt,
		}
	}

	intelligenceCacheMutex.Lock()
	intelligenceCacheScores = scores
	intelligenceCacheUpdatedAt = time.Now().Unix()
	intelligenceCacheMutex.Unlock()
}
