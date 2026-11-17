package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	poeModelParamsRefreshInterval = 30 * time.Minute
	poeModelParamsRetryInterval   = time.Minute
	poeModelParamsHTTPTimeout     = 30 * time.Second
	poeModelParamsMaxBodyBytes    = 16 << 20
	poeModelParamsMaxEndpoints    = 5
)

var (
	poeModelParamsSyncOnce sync.Once
	// poeModelParamsCache maps lowercase Poe model id -> parameter name -> enum values.
	// A parameter without an enum (boolean, number) is present with an empty slice.
	poeModelParamsCache atomic.Pointer[map[string]map[string][]string]

	errNoPoeEndpoint = errors.New("no enabled Poe OpenAI channel")

	// NOTE: Ordered by preference; the first name a model declares wins.
	poeReasoningParamNames = []string{"reasoning_effort", "thinking_level"}

	poeEffortRank = map[string]int{
		"none":    0,
		"minimal": 1,
		"low":     2,
		"medium":  3,
		"high":    4,
		"xhigh":   5,
		"max":     6,
	}
)

type poeBotParam struct {
	Name   string `json:"name"`
	Schema struct {
		Enum []any `json:"enum"`
	} `json:"schema"`
}

type poeBotModel struct {
	Id         string        `json:"id"`
	Parameters []poeBotParam `json:"parameters"`
}

type poeBotModelsResponse struct {
	Data []poeBotModel `json:"data"`
}

type poeModelsEndpoint struct {
	baseURL string
	proxy   string
}

// StartPoeModelParamsSyncTask keeps a process-local cache of the per-model
// parameter definitions that Poe publishes on /v1/models, so the Poe adaptor can
// translate a generic reasoning_effort into the name each model really accepts.
//
// NOTE: Runs on every node because the cache is per-process memory.
func StartPoeModelParamsSyncTask() {
	poeModelParamsSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "Poe model params sync task started")
			for {
				err := refreshPoeModelParams()
				switch {
				case err == nil:
					time.Sleep(poeModelParamsRefreshInterval)
				case errors.Is(err, errNoPoeEndpoint):
					time.Sleep(poeModelParamsRetryInterval)
				default:
					logger.LogWarn(context.Background(), "poe model params sync failed: "+err.Error())
					time.Sleep(poeModelParamsRetryInterval)
				}
			}
		})
	})
}

func refreshPoeModelParams() error {
	var channels []*model.Channel
	err := model.DB.
		Select("id", "type", "base_url", "setting").
		Where("type = ? AND status = ?", constant.ChannelTypePoeOpenAI, common.ChannelStatusEnabled).
		Order("id asc").
		Find(&channels).Error
	if err != nil {
		return fmt.Errorf("query channels: %w", err)
	}

	seen := make(map[poeModelsEndpoint]struct{})
	endpoints := make([]poeModelsEndpoint, 0, poeModelParamsMaxEndpoints)
	for _, ch := range channels {
		baseURL := strings.TrimSuffix(ch.GetBaseURL(), "/")
		if baseURL == "" {
			baseURL = strings.TrimSuffix(constant.ChannelBaseURLs[constant.ChannelTypePoeOpenAI], "/")
		}
		endpoint := poeModelsEndpoint{baseURL: baseURL, proxy: ch.GetSetting().Proxy}
		if _, ok := seen[endpoint]; ok {
			continue
		}
		seen[endpoint] = struct{}{}
		endpoints = append(endpoints, endpoint)
		if len(endpoints) >= poeModelParamsMaxEndpoints {
			break
		}
	}
	if len(endpoints) == 0 {
		return errNoPoeEndpoint
	}

	var lastErr error
	for _, endpoint := range endpoints {
		params, err := fetchPoeModelParams(endpoint)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", endpoint.baseURL, err)
			continue
		}
		SetPoeModelParams(params)
		return nil
	}
	return lastErr
}

func fetchPoeModelParams(endpoint poeModelsEndpoint) (map[string]map[string][]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), poeModelParamsHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client, err := NewProxyHttpClient(endpoint.proxy)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, poeModelParamsMaxBodyBytes))
	if err != nil {
		return nil, err
	}
	params, err := parsePoeModelParams(body)
	if err != nil {
		return nil, err
	}
	if len(params) == 0 {
		return nil, errors.New("model list has no entries")
	}
	return params, nil
}

func parsePoeModelParams(body []byte) (map[string]map[string][]string, error) {
	var parsed poeBotModelsResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal models: %w", err)
	}

	result := make(map[string]map[string][]string, len(parsed.Data))
	for _, m := range parsed.Data {
		id := strings.ToLower(strings.TrimSpace(m.Id))
		if id == "" {
			continue
		}
		params := make(map[string][]string, len(m.Parameters))
		for _, p := range m.Parameters {
			if p.Name == "" {
				continue
			}
			enum := make([]string, 0, len(p.Schema.Enum))
			for _, v := range p.Schema.Enum {
				if s, ok := v.(string); ok {
					enum = append(enum, strings.ToLower(s))
				}
			}
			params[p.Name] = enum
		}
		result[id] = params
	}
	return result, nil
}

// SetPoeModelParams replaces the whole cache.
func SetPoeModelParams(params map[string]map[string][]string) {
	poeModelParamsCache.Store(&params)
}

// ResolvePoeReasoningParam returns the parameter name a Poe model accepts for
// reasoning depth together with the effort value clamped into that parameter's
// enum. ok is false when the model is unknown or declares no such parameter, in
// which case the caller should leave the request untouched.
func ResolvePoeReasoningParam(modelName, effort string) (name, value string, ok bool) {
	cache := poeModelParamsCache.Load()
	if cache == nil {
		return "", "", false
	}
	params, found := (*cache)[strings.ToLower(strings.TrimSpace(modelName))]
	if !found {
		return "", "", false
	}
	for _, candidate := range poeReasoningParamNames {
		if enum, declared := params[candidate]; declared {
			return candidate, clampPoeEffort(effort, enum), true
		}
	}
	return "", "", false
}

func clampPoeEffort(effort string, enum []string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if len(enum) == 0 {
		return effort
	}
	for _, v := range enum {
		if v == effort {
			return effort
		}
	}
	rank, known := poeEffortRank[effort]
	if !known {
		return effort
	}

	best := ""
	bestDist := 0
	for _, v := range enum {
		r, ok := poeEffortRank[v]
		if !ok {
			continue
		}
		dist := r - rank
		if dist < 0 {
			dist = -dist
		}
		// NOTE: On a tie prefer the higher level so a request for reasoning is never
		// silently weakened to "none".
		if best == "" || dist < bestDist || (dist == bestDist && r > poeEffortRank[best]) {
			best = v
			bestDist = dist
		}
	}
	if best == "" {
		return effort
	}
	return best
}
