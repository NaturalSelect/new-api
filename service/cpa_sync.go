package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/cpa_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	cpaUsagePath    = "/v0/management/auth-files/usage"
	cpaHTTPTimeout  = 15 * time.Second
	cpaMaxBodyBytes = 5 << 20 // 5MB
)

// CPAUsageWindow is one usage window (7-day or 5-hour) reported by CPA for a
// single auth credential. A nil *CPAUsageWindow on CPAUsageItem means CPA
// never received an upstream response header for that window, which is
// distinct from an explicit 0% usage.
type CPAUsageWindow struct {
	Percent int    `json:"percent"`
	ResetAt string `json:"reset_at"`
}

// CPAUsageItem is one auth credential's usage snapshot, as reported by
// GET {CPAUrl}/v0/management/auth-files/usage.
type CPAUsageItem struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Usage7D    *CPAUsageWindow `json:"usage_7d,omitempty"`
	Usage5H    *CPAUsageWindow `json:"usage_5h,omitempty"`
	ObservedAt string          `json:"observed_at"`
}

// cpaUsageAPIResponse mirrors the response body of the CPA usage endpoint.
type cpaUsageAPIResponse struct {
	Usage []CPAUsageItem `json:"usage"`
}

var (
	cpaCacheMutex     sync.RWMutex
	cpaCacheUsage     []CPAUsageItem
	cpaCacheUpdatedAt int64

	cpaSyncOnce sync.Once
)

// GetCPAUsage returns the most recently fetched CPA usage snapshot and the
// unix timestamp of that fetch. Both are zero-valued until the first
// successful sync completes.
func GetCPAUsage() ([]CPAUsageItem, int64) {
	cpaCacheMutex.RLock()
	defer cpaCacheMutex.RUnlock()
	return cpaCacheUsage, cpaCacheUpdatedAt
}

// RefreshCPAUsage forces one fetch-parse-cache pass and returns the resulting
// snapshot, or an error if the fetch failed (the existing cache is left
// untouched in that case).
func RefreshCPAUsage() ([]CPAUsageItem, int64, error) {
	if !cpa_setting.EnableCPA() {
		return nil, 0, fmt.Errorf("CPA service is not configured")
	}
	if err := runCPASyncOnce(); err != nil {
		return nil, 0, err
	}
	usage, updatedAt := GetCPAUsage()
	return usage, updatedAt, nil
}

// StartCPASyncTask starts a background goroutine that periodically fetches
// CPA auth-file usage snapshots and keeps them in a process-local in-memory
// cache.
//
// NOTE: like the intelligence sync task, this deliberately runs on every node
// rather than master-only — the cache is per-process memory, and CPA usage
// itself is also process-local on the CPA side, so there is no cross-instance
// state to reconcile.
func StartCPASyncTask() {
	cpaSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "CPA usage sync task started")

			for {
				if !cpa_setting.EnableCPA() {
					time.Sleep(10 * time.Second)
					continue
				}

				if err := runCPASyncOnce(); err != nil {
					logger.LogWarn(context.Background(), "CPA usage sync: "+err.Error())
				}

				interval := time.Duration(cpa_setting.GetCPASyncInterval()) * time.Second
				time.Sleep(interval)
			}
		})
	})
}

// runCPASyncOnce performs one fetch-parse-cache pass against the configured
// CPA service. On success it overwrites the in-memory cache; on failure the
// existing cache is left untouched and the error is returned to the caller.
func runCPASyncOnce() error {
	baseURL := strings.TrimRight(cpa_setting.CPAUrl, "/")
	if baseURL == "" {
		return fmt.Errorf("CPA URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cpaHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+cpaUsagePath, nil)
	if err != nil {
		return fmt.Errorf("build request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cpa_setting.CPAManagementKey)

	client := &http.Client{Timeout: cpaHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, cpaMaxBodyBytes))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cpaMaxBodyBytes))
	if err != nil {
		return fmt.Errorf("read body failed: %w", err)
	}

	var parsed cpaUsageAPIResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}

	cpaCacheMutex.Lock()
	cpaCacheUsage = parsed.Usage
	cpaCacheUpdatedAt = time.Now().Unix()
	cpaCacheMutex.Unlock()

	return nil
}
