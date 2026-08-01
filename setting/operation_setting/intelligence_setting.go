package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

type IntelligenceSetting struct {
	Enabled         bool `json:"enabled"`
	RefreshInterval int  `json:"refresh_interval"` // minutes

	// AutoEffortEnabled controls whether a request's reasoning effort of
	// "auto" gets resolved to the highest-IQ effort level for the resolved
	// model. Off by default because it mutates downstream request content.
	AutoEffortEnabled bool `json:"auto_effort_enabled"`
	// DisabledAutoEfforts is a blacklist of effort levels excluded from auto
	// selection (e.g. "ultra" for cost reasons). Empty means none excluded.
	DisabledAutoEfforts []string `json:"disabled_auto_efforts"`
}

var intelligenceSetting = IntelligenceSetting{
	Enabled:             false,
	RefreshInterval:     60,
	AutoEffortEnabled:   false,
	DisabledAutoEfforts: nil,
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

// IsAutoEffortEnabled reports whether "auto" reasoning effort resolution is
// enabled.
func IsAutoEffortEnabled() bool {
	return intelligenceSetting.AutoEffortEnabled
}

// GetDisabledAutoEfforts returns a copy of the effort levels excluded from
// auto selection, so callers cannot mutate the underlying setting.
func GetDisabledAutoEfforts() []string {
	if len(intelligenceSetting.DisabledAutoEfforts) == 0 {
		return nil
	}
	disabled := make([]string, len(intelligenceSetting.DisabledAutoEfforts))
	copy(disabled, intelligenceSetting.DisabledAutoEfforts)
	return disabled
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
