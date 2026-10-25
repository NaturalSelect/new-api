package traffic_capture_setting

import (
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RecordModeAll    = "all"
	RecordModeFailed = "failed"
)

type TrafficCaptureSetting struct {
	Enabled         bool   `json:"enabled"`
	CaptureUpstream bool   `json:"capture_upstream"`
	RecordMode      string `json:"record_mode"`
	UserIds         []int  `json:"user_ids"`
	TtlMinutes      int    `json:"ttl_minutes"`
	RetentionSizeMB int    `json:"retention_size_mb"`
	MaxBodyMB       int    `json:"max_body_mb"`
}

var trafficCaptureSetting = TrafficCaptureSetting{
	Enabled:         false,
	CaptureUpstream: true,
	RecordMode:      RecordModeAll,
	UserIds:         []int{},
	TtlMinutes:      1440,
	RetentionSizeMB: 2048,
	MaxBodyMB:       2,
}

func init() {
	config.GlobalConfig.Register("traffic_capture_setting", &trafficCaptureSetting)
}

func GetSetting() TrafficCaptureSetting {
	return trafficCaptureSetting
}

// GetTTL never returns less than one minute, so a bad value cannot disable expiry.
func GetTTL() time.Duration {
	return time.Duration(atLeastOne(trafficCaptureSetting.TtlMinutes)) * time.Minute
}

// GetRetentionSizeBytes never returns less than one MB, so a bad value cannot make the cap unbounded.
func GetRetentionSizeBytes() int64 {
	return int64(atLeastOne(trafficCaptureSetting.RetentionSizeMB)) << 20
}

func GetMaxBodyBytes() int64 {
	return int64(atLeastOne(trafficCaptureSetting.MaxBodyMB)) << 20
}

func atLeastOne(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
