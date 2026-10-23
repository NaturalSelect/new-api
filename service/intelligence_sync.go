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
	intelligenceSourceURL    = "https://codexradar.com/degradation-codex-latest.json"
	intelligenceHTTPTimeout  = 30 * time.Second
	intelligenceMaxBodyBytes = 10 << 20 // 10MB

	intelligenceScoredStatus = "scored"
	intelligenceIQScale      = 100
)

// IntelligenceScore is one model+effort combo's benchmark result, exposed to
// the frontend via GetIntelligenceScores. This is our own stable API
// contract; its JSON tags do not necessarily match the upstream response
// shape (see intelligenceAPIResponse), which is mapped into this type on fetch.
type IntelligenceScore struct {
	Model           string  `json:"model"`
	Effort          string  `json:"effort"`
	IQ              float64 `json:"iq"`
	Passed          float64 `json:"passed"`
	ValidTasks      float64 `json:"valid_tasks"`
	AveragePriceUSD float64 `json:"average_price_usd"`
	AverageMinutes  float64 `json:"average_minutes"`
	LatestGradedAt  string  `json:"latest_graded_at"`
}

type intelligenceSetting struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type intelligenceTask struct {
	ID     string  `json:"id"`
	Weight float64 `json:"weight"`
}

type intelligenceTaskResult struct {
	Status  string  `json:"status"`
	Reward  float64 `json:"reward"`
	Seconds float64 `json:"seconds"`
	Cost    struct {
		USD *float64 `json:"usd"`
	} `json:"cost"`
}

// NOTE: Mirrors the subset of https://codexradar.com/degradation-codex-latest.json
// we consume. round.results is keyed by "model|effort", then by task id.
type intelligenceAPIResponse struct {
	Settings []intelligenceSetting `json:"settings"`
	Tasks    []intelligenceTask    `json:"tasks"`
	Round    struct {
		CompletedAt string                                       `json:"completed_at"`
		Results     map[string]map[string]intelligenceTaskResult `json:"results"`
	} `json:"round"`
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

	scores := buildIntelligenceScores(parsed)
	if len(scores) == 0 {
		logger.LogError(ctx, "intelligence sync: response contained no scorable results, keeping previous cache")
		return
	}

	intelligenceCacheMutex.Lock()
	intelligenceCacheScores = scores
	intelligenceCacheUpdatedAt = time.Now().Unix()
	intelligenceCacheMutex.Unlock()
}

// buildIntelligenceScores folds the per-task rewards of each model+effort
// setting into one IntelligenceScore. Only scored tasks listed in resp.Tasks
// contribute; settings with no such task are omitted. Cost and duration
// averages cover only the tasks that reported them.
func buildIntelligenceScores(resp intelligenceAPIResponse) []IntelligenceScore {
	scores := make([]IntelligenceScore, 0, len(resp.Settings))
	for _, setting := range resp.Settings {
		results := resp.Round.Results[setting.Model+"|"+setting.Effort]

		var weightedPassed, weightedTotal, totalSeconds, totalUSD float64
		var scoredCount, pricedCount int
		// NOTE: iterate resp.Tasks (not the results map) so float summation order is deterministic.
		for _, task := range resp.Tasks {
			result, ok := results[task.ID]
			if !ok || result.Status != intelligenceScoredStatus {
				continue
			}
			weightedPassed += task.Weight * result.Reward
			weightedTotal += task.Weight
			totalSeconds += result.Seconds
			scoredCount++
			if result.Cost.USD != nil {
				totalUSD += *result.Cost.USD
				pricedCount++
			}
		}
		if scoredCount == 0 || weightedTotal <= 0 {
			continue
		}

		score := IntelligenceScore{
			Model:          setting.Model,
			Effort:         setting.Effort,
			IQ:             weightedPassed / weightedTotal * intelligenceIQScale,
			Passed:         weightedPassed,
			ValidTasks:     weightedTotal,
			AverageMinutes: totalSeconds / float64(scoredCount) / 60,
			LatestGradedAt: resp.Round.CompletedAt,
		}
		if pricedCount > 0 {
			score.AveragePriceUSD = totalUSD / float64(pricedCount)
		}
		scores = append(scores, score)
	}
	return scores
}
