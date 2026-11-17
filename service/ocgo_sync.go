package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	ocgoSyncInterval   = 180 * time.Second
	ocgoRequestTimeout = 15 * time.Second
	ocgoMaxConcurrency = 4
)

type OCGOUsageWindow struct {
	Status  string  `json:"status"`
	Percent float64 `json:"percent"`
	ResetAt string  `json:"reset_at"`
}

// OCGOUsageItem is the usage snapshot of one OpenCode Go API key. A multi-key channel yields one item per enabled key.
type OCGOUsageItem struct {
	ID        string           `json:"id"`
	ChannelID int              `json:"channel_id"`
	Name      string           `json:"name"`
	Rolling   *OCGOUsageWindow `json:"rolling,omitempty"`
	Weekly    *OCGOUsageWindow `json:"weekly,omitempty"`
	Monthly   *OCGOUsageWindow `json:"monthly,omitempty"`
	Error     string           `json:"error,omitempty"`
}

type ocgoTarget struct {
	id        string
	channelID int
	name      string
	baseURL   string
	key       string
	proxy     string
}

var (
	ocgoSyncMutex sync.Mutex

	ocgoCacheMutex     sync.RWMutex
	ocgoCacheUsage     = []OCGOUsageItem{}
	ocgoCacheUpdatedAt int64

	ocgoSyncOnce sync.Once
)

// GetOCGOUsage returns the most recent snapshot and the unix time it was taken, which is zero until the first sync finishes.
func GetOCGOUsage() ([]OCGOUsageItem, int64) {
	ocgoCacheMutex.RLock()
	defer ocgoCacheMutex.RUnlock()
	return ocgoCacheUsage, ocgoCacheUpdatedAt
}

// RefreshOCGOUsage runs one sync now. It fails only when the channels cannot be loaded, since a failing key is reported on its own item.
func RefreshOCGOUsage() ([]OCGOUsageItem, int64, error) {
	if err := runOCGOSyncOnce(); err != nil {
		return nil, 0, err
	}
	usage, updatedAt := GetOCGOUsage()
	return usage, updatedAt, nil
}

// StartOCGOSyncTask keeps the OpenCode Go usage snapshot fresh.
// NOTE: It runs on every node because the snapshot lives in process memory, like the CPA usage sync.
func StartOCGOSyncTask() {
	ocgoSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "OpenCode Go usage sync task started")
			for {
				if err := runOCGOSyncOnce(); err != nil {
					logger.LogWarn(context.Background(), "OpenCode Go usage sync: "+err.Error())
				}
				time.Sleep(ocgoSyncInterval)
			}
		})
	})
}

func buildOCGOTargets(channels []*model.Channel) []ocgoTarget {
	targets := make([]ocgoTarget, 0, len(channels))
	for _, ch := range channels {
		baseURL := ch.GetBaseURL()
		if baseURL == "" {
			baseURL = constant.ChannelBaseURLs[ch.Type]
		}
		proxy := ch.GetSetting().Proxy
		if !ch.ChannelInfo.IsMultiKey {
			key := strings.TrimSpace(ch.Key)
			if key == "" {
				continue
			}
			targets = append(targets, ocgoTarget{
				id:        strconv.Itoa(ch.Id),
				channelID: ch.Id,
				name:      ch.Name,
				baseURL:   baseURL,
				key:       key,
				proxy:     proxy,
			})
			continue
		}
		for idx, key := range ch.GetKeys() {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if status, ok := ch.ChannelInfo.MultiKeyStatusList[idx]; ok && status != common.ChannelStatusEnabled {
				continue
			}
			targets = append(targets, ocgoTarget{
				id:        fmt.Sprintf("%d-%d", ch.Id, idx),
				channelID: ch.Id,
				name:      fmt.Sprintf("%s #%d", ch.Name, idx+1),
				baseURL:   baseURL,
				key:       key,
				proxy:     proxy,
			})
		}
	}
	return targets
}

func toOCGOWindow(w *OpenCodeGoUsageWindow) *OCGOUsageWindow {
	if w == nil {
		return nil
	}
	return &OCGOUsageWindow{Status: w.Status, Percent: w.Percent, ResetAt: w.ResetsAt}
}

func fetchOCGOItem(target ocgoTarget) OCGOUsageItem {
	item := OCGOUsageItem{ID: target.id, ChannelID: target.channelID, Name: target.name}

	// NOTE: The proxy URL may carry credentials, so the error from building the client is not echoed back.
	client, err := NewProxyHttpClient(target.proxy)
	if err != nil {
		item.Error = "invalid proxy setting"
		return item
	}
	ctx, cancel := context.WithTimeout(context.Background(), ocgoRequestTimeout)
	defer cancel()

	statusCode, body, err := FetchOpenCodeGoUsage(ctx, client, target.baseURL, target.key)
	if err != nil {
		item.Error = "request failed: " + err.Error()
		return item
	}
	if statusCode != http.StatusOK {
		item.Error = fmt.Sprintf("upstream status %d", statusCode)
		return item
	}
	usage, err := ParseOpenCodeGoUsage(body)
	if err != nil {
		item.Error = err.Error()
		return item
	}
	item.Rolling = toOCGOWindow(usage.Rolling)
	item.Weekly = toOCGOWindow(usage.Weekly)
	item.Monthly = toOCGOWindow(usage.Monthly)
	return item
}

// runOCGOSyncOnce queries every enabled OpenCode Go key through its channel's own proxy and replaces the snapshot.
// The previous snapshot is kept when the channels cannot be loaded.
func runOCGOSyncOnce() error {
	ocgoSyncMutex.Lock()
	defer ocgoSyncMutex.Unlock()

	channels, err := model.GetEnabledChannelsByTypeWithKey(constant.ChannelTypeOpenCodeGo)
	if err != nil {
		return fmt.Errorf("load channels failed: %w", err)
	}
	targets := buildOCGOTargets(channels)

	items := make([]OCGOUsageItem, len(targets))
	sem := make(chan struct{}, ocgoMaxConcurrency)
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		sem <- struct{}{}
		gopool.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()
			items[i] = fetchOCGOItem(target)
		})
	}
	wg.Wait()

	ocgoCacheMutex.Lock()
	ocgoCacheUsage = items
	ocgoCacheUpdatedAt = time.Now().Unix()
	ocgoCacheMutex.Unlock()
	return nil
}
