package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	openCodeGoUsagePath      = "/v1/usage"
	openCodeGoUsageMaxBytes  = 1 << 20
	openCodeGoUsageUserAgent = "new-api/usage"
)

type OpenCodeGoUsageWindow struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

type OpenCodeGoUsage struct {
	Rolling *OpenCodeGoUsageWindow `json:"rolling"`
	Weekly  *OpenCodeGoUsageWindow `json:"weekly"`
	Monthly *OpenCodeGoUsageWindow `json:"monthly"`
}

// FetchOpenCodeGoUsage reads the Go subscription usage windows of one API key and returns the upstream status code with the raw body.
func FetchOpenCodeGoUsage(ctx context.Context, client *http.Client, baseURL, key string) (int, []byte, error) {
	if client == nil {
		return 0, nil, errors.New("nil http client")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return 0, nil, errors.New("empty baseURL")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, nil, errors.New("empty key")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+openCodeGoUsagePath, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	// NOTE: The upstream asks clients to identify themselves instead of sending a generic HTTP library user agent.
	req.Header.Set("User-Agent", openCodeGoUsageUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, openCodeGoUsageMaxBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func ParseOpenCodeGoUsage(body []byte) (*OpenCodeGoUsage, error) {
	var parsed struct {
		Usage *OpenCodeGoUsage `json:"usage"`
	}
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}
	if parsed.Usage == nil {
		return nil, errors.New("usage is missing from the response")
	}
	return parsed.Usage, nil
}
