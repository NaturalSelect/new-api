package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modelcatalog"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	openRouterCatalogCheckInterval = time.Minute
	openRouterCatalogRetryDelay    = 5 * time.Minute
	openRouterCatalogHTTPTimeout   = 30 * time.Second
	openRouterCatalogMaxBodyBytes  = 16 << 20 // 16MB
)

var openRouterCatalogSyncOnce sync.Once

type openRouterCatalogSyncState struct {
	nextRefresh time.Time
	modelsURL   string
	proxy       string
}

// StartOpenRouterCatalogSyncTask starts a background goroutine that keeps the
// OpenRouter model catalog (context length, max output, modalities, knowledge
// cutoff, release date) fresh in a process-local in-memory cache.
//
// NOTE: like the intelligence sync, this deliberately runs on every node rather
// than master-only, because the cache is per-process memory.
func StartOpenRouterCatalogSyncTask() {
	openRouterCatalogSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "OpenRouter model catalog sync task started")

			var state openRouterCatalogSyncState
			for {
				runOpenRouterCatalogSyncStep(&state, operation_setting.GetOpenRouterCatalogSetting(), time.Now())
				time.Sleep(openRouterCatalogCheckInterval)
			}
		})
	})
}

// runOpenRouterCatalogSyncStep decides whether a refresh is due and performs it.
// Failures are logged and swallowed, and the previous catalog is kept, so an
// unreachable third-party API never affects the rest of the application.
func runOpenRouterCatalogSyncStep(state *openRouterCatalogSyncState, cfg operation_setting.OpenRouterCatalogSetting, now time.Time) {
	if !cfg.Enabled {
		if modelcatalog.Current() != nil {
			modelcatalog.Clear()
			model.InvalidatePricingCache()
		}
		*state = openRouterCatalogSyncState{}
		return
	}

	sourceChanged := cfg.ModelsURL != state.modelsURL || cfg.Proxy != state.proxy
	if !sourceChanged && now.Before(state.nextRefresh) {
		return
	}
	state.modelsURL = cfg.ModelsURL
	state.proxy = cfg.Proxy

	snap, err := refreshOpenRouterCatalog(cfg)
	if err != nil {
		logger.LogWarn(context.Background(), "openrouter catalog sync failed: "+err.Error())
		state.nextRefresh = now.Add(openRouterCatalogRetryDelay)
		return
	}

	modelcatalog.Set(snap)
	model.InvalidatePricingCache()
	state.nextRefresh = now.Add(time.Duration(cfg.RefreshInterval) * time.Minute)
	logger.LogInfo(context.Background(), fmt.Sprintf("openrouter catalog sync done: %d models", snap.Len()))
}

func refreshOpenRouterCatalog(cfg operation_setting.OpenRouterCatalogSetting) (*modelcatalog.Snapshot, error) {
	client, err := NewProxyHttpClient(cfg.Proxy)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), openRouterCatalogHTTPTimeout)
	defer cancel()
	return fetchOpenRouterCatalog(ctx, client, cfg.ModelsURL)
}

func fetchOpenRouterCatalog(ctx context.Context, client *http.Client, modelsURL string) (*modelcatalog.Snapshot, error) {
	parsed, err := url.Parse(modelsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid models url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid models url scheme %q", parsed.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, openRouterCatalogMaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	snap, err := modelcatalog.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	return snap, nil
}
