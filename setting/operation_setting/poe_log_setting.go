package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type PoeLogSetting struct {
	Enabled        bool `json:"enabled"`
	SyncInterval   int  `json:"sync_interval"`   // seconds
	KeyDeduplicate bool `json:"key_deduplicate"` // deduplicate API keys across channels
}

var poeLogSetting = PoeLogSetting{
	Enabled:        false,
	SyncInterval:   300,
	KeyDeduplicate: true,
}

func init() {
	config.GlobalConfig.Register("poe_log_setting", &poeLogSetting)
}

func GetPoeLogSetting() *PoeLogSetting {
	return &poeLogSetting
}

func IsPoeLogSyncEnabled() bool {
	return poeLogSetting.Enabled
}

func GetPoeLogSyncIntervalSeconds() int {
	if poeLogSetting.SyncInterval <= 0 {
		return 60
	}
	return poeLogSetting.SyncInterval
}

func IsPoeLogKeyDeduplicate() bool {
	return poeLogSetting.KeyDeduplicate
}
