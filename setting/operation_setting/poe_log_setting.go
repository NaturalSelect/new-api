package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type PoeLogSetting struct {
	Enabled           bool     `json:"enabled"`
	SyncInterval      int      `json:"sync_interval"`        // seconds
	KeyDeduplicate    bool     `json:"key_deduplicate"`      // deduplicate API keys across channels
	FreeModels        []string `json:"free_models"`          // manually configured free model names
	SyncToConsumeLog  bool     `json:"sync_to_consume_log"`  // sync PoeLog entries to consume log
}

var poeLogSetting = PoeLogSetting{
	Enabled:          false,
	SyncInterval:     300,
	KeyDeduplicate:   true,
	SyncToConsumeLog: true,
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

func IsPoeLogSyncToConsumeLogEnabled() bool {
	return poeLogSetting.SyncToConsumeLog
}

// NOTE: GetPoeFreeModels returns configured free model names as a lowercase lookup set.
func GetPoeFreeModels() map[string]bool {
	if len(poeLogSetting.FreeModels) == 0 {
		return nil
	}
	set := make(map[string]bool, len(poeLogSetting.FreeModels))
	for _, m := range poeLogSetting.FreeModels {
		m = strings.ToLower(strings.TrimSpace(m))
		if m != "" {
			set[m] = true
		}
	}
	return set
}
