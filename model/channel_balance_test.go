package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelIsDisabledByBalance(t *testing.T) {
	tests := []struct {
		name     string
		info     map[string]interface{}
		expected bool
	}{
		{name: "missing", info: nil, expected: false},
		{name: "malformed", info: map[string]interface{}{"status_reason": 1}, expected: false},
		{name: "other reason", info: map[string]interface{}{"status_reason": "upstream error"}, expected: false},
		{name: "balance", info: map[string]interface{}{"status_reason": ChannelDisableReasonBalance}, expected: true},
		{name: "credit balance error", info: map[string]interface{}{"status_reason": "status_code=400, Your credit balance is too low"}, expected: true},
		{name: "payment required", info: map[string]interface{}{"status_reason": "status_code=402, Payment Required"}, expected: true},
		{name: "quota error", info: map[string]interface{}{"status_reason": "status_code=429, You exceeded your current quota"}, expected: true},
		{name: "insufficient quota error", info: map[string]interface{}{"status_reason": "status_code=429, insufficient_quota"}, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{}
			if tt.info != nil {
				channel.SetOtherInfo(tt.info)
			}

			require.Equal(t, tt.expected, channel.IsDisabledByBalance())
		})
	}
}
