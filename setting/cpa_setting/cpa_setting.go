package cpa_setting

import "strings"

var CPAUrl = ""
var CPAManagementKey = ""
var CPASyncInterval = 180
var CPATypeOrder = "claude,codex"

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

// GetCPATypeOrder returns the configured credential type display order,
// parsed from the comma-separated CPATypeOrder setting. Types not listed
// here are left for the caller to sort after the listed ones.
func GetCPATypeOrder() []string {
	parts := strings.Split(CPATypeOrder, ",")
	order := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			order = append(order, trimmed)
		}
	}
	return order
}
