package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func withEasterEggSetting(t *testing.T, enabled bool, modelName string) {
	t.Helper()
	orig := easterEggSetting
	t.Cleanup(func() { easterEggSetting = orig })
	easterEggSetting = EasterEggSetting{Enabled: enabled, ModelName: modelName}
}

func TestIsEasterEggModel(t *testing.T) {
	cases := []struct {
		name      string
		enabled   bool
		configed  string
		requested string
		want      bool
	}{
		{"disabled, any name does not match", false, "nailong", "nailong", false},
		{"enabled but empty configured name never matches", true, "", "nailong", false},
		{"enabled, exact match", true, "nailong", "nailong", true},
		{"enabled, case-insensitive match", true, "nailong", "NaiLong", true},
		{"enabled, requested name has surrounding spaces", true, "nailong", "  nailong  ", true},
		{"enabled, configured name has surrounding spaces", true, "  nailong  ", "nailong", true},
		{"enabled, unrelated model name does not match", true, "nailong", "gpt-4o", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEasterEggSetting(t, tc.enabled, tc.configed)
			require.Equal(t, tc.want, IsEasterEggModel(tc.requested))
		})
	}
}
