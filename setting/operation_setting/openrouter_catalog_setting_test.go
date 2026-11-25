package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"

	"github.com/stretchr/testify/require"
)

func withOpenRouterCatalogOptions(t *testing.T, options map[string]string) {
	t.Helper()

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(options))
}

func TestOpenRouterCatalogSettingDefaults(t *testing.T) {
	s := GetOpenRouterCatalogSetting()

	require.True(t, s.Enabled)
	require.Equal(t, 360, s.RefreshInterval)
	require.Equal(t, "https://openrouter.ai/api/v1/models", s.ModelsURL)
	require.Empty(t, s.Proxy)
}

func TestOpenRouterCatalogSettingIsExportedAsOptions(t *testing.T) {
	exported := config.GlobalConfig.ExportAllConfigs()

	require.Equal(t, "true", exported["openrouter_catalog_setting.enabled"])
	require.Equal(t, "360", exported["openrouter_catalog_setting.refresh_interval"])
	require.Equal(t, "https://openrouter.ai/api/v1/models", exported["openrouter_catalog_setting.models_url"])
	require.Contains(t, exported, "openrouter_catalog_setting.proxy")
}

func TestOpenRouterCatalogSettingNormalizesStoredValues(t *testing.T) {
	tests := []struct {
		name         string
		options      map[string]string
		wantEnabled  bool
		wantInterval int
		wantURL      string
		wantProxy    string
	}{
		{
			name: "interval below the floor is raised",
			options: map[string]string{
				"openrouter_catalog_setting.refresh_interval": "5",
			},
			wantEnabled:  true,
			wantInterval: MinOpenRouterCatalogRefreshInterval,
			wantURL:      DefaultOpenRouterCatalogModelsURL,
		},
		{
			name: "zero interval falls back to the default",
			options: map[string]string{
				"openrouter_catalog_setting.refresh_interval": "0",
			},
			wantEnabled:  true,
			wantInterval: DefaultOpenRouterCatalogRefreshInterval,
			wantURL:      DefaultOpenRouterCatalogModelsURL,
		},
		{
			name: "blank url falls back and proxy is trimmed",
			options: map[string]string{
				"openrouter_catalog_setting.models_url": "   ",
				"openrouter_catalog_setting.proxy":      " http://127.0.0.1:7890 ",
			},
			wantEnabled:  true,
			wantInterval: DefaultOpenRouterCatalogRefreshInterval,
			wantURL:      DefaultOpenRouterCatalogModelsURL,
			wantProxy:    "http://127.0.0.1:7890",
		},
		{
			name: "custom values are kept and enabled can be turned off",
			options: map[string]string{
				"openrouter_catalog_setting.enabled":          "false",
				"openrouter_catalog_setting.refresh_interval": "720",
				"openrouter_catalog_setting.models_url":       "https://mirror.example.com/models",
			},
			wantEnabled:  false,
			wantInterval: 720,
			wantURL:      "https://mirror.example.com/models",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withOpenRouterCatalogOptions(t, tt.options)

			s := GetOpenRouterCatalogSetting()

			require.Equal(t, tt.wantEnabled, s.Enabled)
			require.Equal(t, tt.wantInterval, s.RefreshInterval)
			require.Equal(t, tt.wantURL, s.ModelsURL)
			require.Equal(t, tt.wantProxy, s.Proxy)
		})
	}
}
