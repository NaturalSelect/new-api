package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultOpenRouterCatalogModelsURL       = "https://openrouter.ai/api/v1/models"
	DefaultOpenRouterCatalogRefreshInterval = 360
	MinOpenRouterCatalogRefreshInterval     = 30
)

type OpenRouterCatalogSetting struct {
	Enabled         bool   `json:"enabled"`
	RefreshInterval int    `json:"refresh_interval"` // minutes
	ModelsURL       string `json:"models_url"`
	Proxy           string `json:"proxy"`
}

var openRouterCatalogSetting = OpenRouterCatalogSetting{
	Enabled:         true,
	RefreshInterval: DefaultOpenRouterCatalogRefreshInterval,
	ModelsURL:       DefaultOpenRouterCatalogModelsURL,
	Proxy:           "",
}

func init() {
	config.GlobalConfig.Register("openrouter_catalog_setting", &openRouterCatalogSetting)
}

// GetOpenRouterCatalogSetting returns a copy with empty values replaced by defaults and
// the refresh interval raised to its floor.
func GetOpenRouterCatalogSetting() OpenRouterCatalogSetting {
	s := openRouterCatalogSetting
	s.ModelsURL = strings.TrimSpace(s.ModelsURL)
	if s.ModelsURL == "" {
		s.ModelsURL = DefaultOpenRouterCatalogModelsURL
	}
	s.Proxy = strings.TrimSpace(s.Proxy)
	if s.RefreshInterval <= 0 {
		s.RefreshInterval = DefaultOpenRouterCatalogRefreshInterval
	}
	if s.RefreshInterval < MinOpenRouterCatalogRefreshInterval {
		s.RefreshInterval = MinOpenRouterCatalogRefreshInterval
	}
	return s
}
