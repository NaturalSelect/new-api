package cpa_setting

var CPAUrl = ""
var CPAManagementKey = ""
var CPASyncInterval = 180

const MinCPASyncIntervalSeconds = 30

// EnableCPA reports whether the CPA integration is configured.
func EnableCPA() bool {
	return CPAUrl != ""
}

// GetCPASyncInterval returns the configured sync interval in seconds,
// enforcing a minimum to avoid excessive load on the CPA service.
func GetCPASyncInterval() int {
	if CPASyncInterval < MinCPASyncIntervalSeconds {
		return MinCPASyncIntervalSeconds
	}
	return CPASyncInterval
}
