package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
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
	poeModelCatalogRefreshInterval = 30 * time.Minute
	poeModelCatalogRetryInterval   = time.Minute
	poeModelCatalogHTTPTimeout     = 30 * time.Second
	poeModelCatalogMaxBodyBytes    = 16 << 20
	poeModelCatalogMaxEndpoints    = 5

	poeResponsesEndpoint = "/v1/responses"
)

// NOTE: Poe image models name their aspect parameter differently; the first one a model declares wins.
var poeImageAspectParamNames = []string{"aspect_ratio", "aspect"}

var (
	poeModelCatalogSyncOnce sync.Once
	// poeModelCatalogCache maps lowercase Poe model id -> what Poe publishes about it.
	poeModelCatalogCache atomic.Pointer[map[string]PoeModelInfo]

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

// PoeModelInfo is the part of Poe's /v1/models entry that the adaptor acts on.
type PoeModelInfo struct {
	// Params maps a parameter name to its enum values. A parameter without a string
	// enum (boolean, number) is present with an empty slice.
	Params map[string][]string
	// Endpoints are the API paths Poe lists for the model, lowercased.
	Endpoints []string
}

type poeBotParam struct {
	Name   string `json:"name"`
	Schema struct {
		Enum []any `json:"enum"`
	} `json:"schema"`
}

type poeBotModel struct {
	Id                 string        `json:"id"`
	SupportedEndpoints []string      `json:"supported_endpoints"`
	Parameters         []poeBotParam `json:"parameters"`
}

type poeBotModelsResponse struct {
	Data []poeBotModel `json:"data"`
}

type poeModelsEndpoint struct {
	baseURL string
	proxy   string
}

// StartPoeModelCatalogSyncTask keeps a process-local cache of the per-model
// parameters and endpoints that Poe publishes on /v1/models. The Poe adaptor uses
// it to translate a generic reasoning_effort into the name each model really accepts
// and to serve Responses requests through Chat Completions for models without a
// Responses endpoint.
//
// NOTE: Runs on every node because the cache is per-process memory.
func StartPoeModelCatalogSyncTask() {
	poeModelCatalogSyncOnce.Do(func() {
		gopool.Go(func() {
			logger.LogInfo(context.Background(), "Poe model catalog sync task started")
			for {
				err := refreshPoeModelCatalog()
				switch {
				case err == nil:
					time.Sleep(poeModelCatalogRefreshInterval)
				case errors.Is(err, errNoPoeEndpoint):
					time.Sleep(poeModelCatalogRetryInterval)
				default:
					logger.LogWarn(context.Background(), "poe model catalog sync failed: "+err.Error())
					time.Sleep(poeModelCatalogRetryInterval)
				}
			}
		})
	})
}

func refreshPoeModelCatalog() error {
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
	endpoints := make([]poeModelsEndpoint, 0, poeModelCatalogMaxEndpoints)
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
		if len(endpoints) >= poeModelCatalogMaxEndpoints {
			break
		}
	}
	if len(endpoints) == 0 {
		return errNoPoeEndpoint
	}

	var lastErr error
	for _, endpoint := range endpoints {
		catalog, err := fetchPoeModelCatalog(endpoint)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", endpoint.baseURL, err)
			continue
		}
		SetPoeModelCatalog(catalog)
		return nil
	}
	return lastErr
}

func fetchPoeModelCatalog(endpoint poeModelsEndpoint) (map[string]PoeModelInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), poeModelCatalogHTTPTimeout)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, poeModelCatalogMaxBodyBytes))
	if err != nil {
		return nil, err
	}
	catalog, err := parsePoeModelCatalog(body)
	if err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, errors.New("model list has no entries")
	}
	return catalog, nil
}

func parsePoeModelCatalog(body []byte) (map[string]PoeModelInfo, error) {
	var parsed poeBotModelsResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal models: %w", err)
	}

	result := make(map[string]PoeModelInfo, len(parsed.Data))
	for _, m := range parsed.Data {
		id := strings.ToLower(strings.TrimSpace(m.Id))
		if id == "" {
			continue
		}
		info := PoeModelInfo{
			Params:    make(map[string][]string, len(m.Parameters)),
			Endpoints: make([]string, 0, len(m.SupportedEndpoints)),
		}
		for _, endpoint := range m.SupportedEndpoints {
			info.Endpoints = append(info.Endpoints, strings.ToLower(strings.TrimSpace(endpoint)))
		}
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
			info.Params[p.Name] = enum
		}
		result[id] = info
	}
	return result, nil
}

// SetPoeModelCatalog replaces the whole cache.
func SetPoeModelCatalog(catalog map[string]PoeModelInfo) {
	poeModelCatalogCache.Store(&catalog)
}

func lookupPoeModel(modelName string) (PoeModelInfo, bool) {
	cache := poeModelCatalogCache.Load()
	if cache == nil {
		return PoeModelInfo{}, false
	}
	info, found := (*cache)[strings.ToLower(strings.TrimSpace(modelName))]
	return info, found
}

// ShouldPoeResponsesUseChat reports whether a Responses request for the model has to
// be served through Chat Completions because Poe does not list /v1/responses for it.
// Models missing from the catalog keep the endpoint the client asked for.
func ShouldPoeResponsesUseChat(modelName string) bool {
	info, found := lookupPoeModel(modelName)
	if !found {
		return false
	}
	for _, endpoint := range info.Endpoints {
		if endpoint == poeResponsesEndpoint {
			return false
		}
	}
	return true
}

// ResolvePoeReasoningParam returns the parameter name a Poe model accepts for
// reasoning depth together with the effort value clamped into that parameter's
// enum. ok is false when the model is unknown or declares no such parameter, in
// which case the caller should leave the request untouched.
func ResolvePoeReasoningParam(modelName, effort string) (name, value string, ok bool) {
	info, found := lookupPoeModel(modelName)
	if !found {
		return "", "", false
	}
	for _, candidate := range poeReasoningParamNames {
		if enum, declared := info.Params[candidate]; declared {
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

// PoeModelDeclaresParam reports whether Poe lists the parameter for the model.
func PoeModelDeclaresParam(modelName, param string) bool {
	info, found := lookupPoeModel(modelName)
	if !found {
		return false
	}
	_, declared := info.Params[param]
	return declared
}

// ResolvePoeImageParams maps an OpenAI image size and quality onto the parameters the
// Poe model declares, keyed by parameter name. A value the model has no matching
// parameter or enum entry for is dropped, as is everything for an unknown model.
func ResolvePoeImageParams(modelName, size, quality string) map[string]string {
	info, found := lookupPoeModel(modelName)
	if !found {
		return nil
	}
	params := make(map[string]string)
	if name, value, ok := resolvePoeImageSizeParam(info, size); ok {
		params[name] = value
	}
	quality = strings.TrimSpace(quality)
	if enum, declared := info.Params["quality"]; declared && quality != "" {
		for _, v := range enum {
			// NOTE: Enums are stored lowercased, so the client's own spelling is what gets sent.
			if v == strings.ToLower(quality) {
				params["quality"] = quality
				break
			}
		}
	}
	return params
}

func resolvePoeImageSizeParam(info PoeModelInfo, size string) (name, value string, ok bool) {
	target, ok := parseDimensionRatio(size, "x")
	if !ok {
		return "", "", false
	}
	if enum, declared := info.Params["size"]; declared {
		if value, ok := closestDimensionRatio(enum, "x", target); ok {
			return "size", value, true
		}
	}
	for _, candidate := range poeImageAspectParamNames {
		if enum, declared := info.Params[candidate]; declared {
			if value, ok := closestDimensionRatio(enum, ":", target); ok {
				return candidate, value, true
			}
		}
	}
	return "", "", false
}

// parseDimensionRatio turns "1024x1536" or "16:9" into width/height.
func parseDimensionRatio(value, separator string) (float64, bool) {
	left, right, found := strings.Cut(strings.TrimSpace(value), separator)
	if !found {
		return 0, false
	}
	width, err := strconv.ParseFloat(left, 64)
	if err != nil || width <= 0 {
		return 0, false
	}
	height, err := strconv.ParseFloat(right, 64)
	if err != nil || height <= 0 {
		return 0, false
	}
	return width / height, true
}

// closestDimensionRatio picks the enum entry whose ratio is nearest to target, skipping
// entries such as "auto" that carry no ratio.
func closestDimensionRatio(enum []string, separator string, target float64) (string, bool) {
	best, bestDist, found := "", 0.0, false
	for _, v := range enum {
		ratio, ok := parseDimensionRatio(v, separator)
		if !ok {
			continue
		}
		dist := math.Abs(math.Log(ratio / target))
		if !found || dist < bestDist {
			best, bestDist, found = v, dist, true
		}
	}
	return best, found
}
