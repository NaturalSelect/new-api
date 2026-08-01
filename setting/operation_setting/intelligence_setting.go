package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

type IntelligenceSetting struct {
	Enabled         bool `json:"enabled"`
	RefreshInterval int  `json:"refresh_interval"` // minutes
}

var intelligenceSetting = IntelligenceSetting{
	Enabled:         false,
	RefreshInterval: 60,
}

func init() {
	config.GlobalConfig.Register("intelligence_setting", &intelligenceSetting)
}

func GetIntelligenceSetting() *IntelligenceSetting {
	return &intelligenceSetting
}

func IsIntelligenceSyncEnabled() bool {
	return intelligenceSetting.Enabled
}

// GetIntelligenceRefreshIntervalMinutes returns the configured refresh interval,
// falling back to 60 minutes when unset and enforcing a 10 minute floor to avoid
// excessive requests to the external benchmark API.
func GetIntelligenceRefreshIntervalMinutes() int {
	interval := intelligenceSetting.RefreshInterval
	if interval <= 0 {
		return 60
	}
	if interval < 10 {
		return 10
	}
	return interval
}
