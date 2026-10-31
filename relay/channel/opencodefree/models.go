package opencodefree

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
)

// NOTE: The upstream model list is not restricted to free models, so it is filtered with the same whitelist the relay enforces.
func FetchModels(baseURL, proxyURL string) ([]string, error) {
	client, err := service.GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create http client failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header = FetchModelsHeaders()
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(result.Data))
	models := make([]string, 0, len(result.Data))
	for _, item := range result.Data {
		if IsAllowedModel(item.ID) && !seen[item.ID] {
			seen[item.ID] = true
			models = append(models, item.ID)
		}
	}
	return models, nil
}
