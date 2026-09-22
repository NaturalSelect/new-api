package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSimplifyOpenRouterModelID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
		ok       bool
	}{
		{
			name:     "claude dot version rewritten to dash",
			id:       "anthropic/claude-opus-4.5",
			expected: "claude-opus-4-5",
			ok:       true,
		},
		{
			name:     "claude dot version in the middle rewritten to dash",
			id:       "anthropic/claude-3.7-sonnet",
			expected: "claude-3-7-sonnet",
			ok:       true,
		},
		{
			name:     "claude id without dot version is unchanged",
			id:       "anthropic/claude-sonnet-4",
			expected: "claude-sonnet-4",
			ok:       true,
		},
		{
			name:     "claude id already dash-separated is unchanged",
			id:       "anthropic/claude-3-haiku",
			expected: "claude-3-haiku",
			ok:       true,
		},
		{
			name:     "claude variant entries are discarded",
			id:       "anthropic/claude-opus-4.5:thinking",
			expected: "",
			ok:       false,
		},
		{
			name:     "non-claude dot version is left untouched",
			id:       "qwen/qwen3.8-max-0902",
			expected: "qwen3.8-max-0902",
			ok:       true,
		},
		{
			name:     "id without provider prefix is left untouched",
			id:       "claude-opus-4.1",
			expected: "claude-opus-4-1",
			ok:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := simplifyOpenRouterModelID(tt.id)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.expected, got)
		})
	}
}
