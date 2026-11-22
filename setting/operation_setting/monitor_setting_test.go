package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestMonitorSettingAutoUnbanBalanceThresholdConfigRoundTrip(t *testing.T) {
	orig := monitorSetting
	t.Cleanup(func() { monitorSetting = orig })

	exported := config.GlobalConfig.ExportAllConfigs()
	require.Equal(t, "0", exported["monitor_setting.auto_unban_balance_threshold"])

	cfg := config.GlobalConfig.Get("monitor_setting")
	require.NotNil(t, cfg)

	err := config.UpdateConfigFromMap(cfg, map[string]string{
		"auto_unban_balance_threshold": "12.5",
	})
	require.NoError(t, err)
	require.Equal(t, 12.5, GetMonitorSetting().AutoUnbanBalanceThreshold)

	err = config.UpdateConfigFromMap(cfg, map[string]string{
		"auto_unban_balance_threshold": "0",
	})
	require.NoError(t, err)
	require.Equal(t, 0.0, GetMonitorSetting().AutoUnbanBalanceThreshold)
}
