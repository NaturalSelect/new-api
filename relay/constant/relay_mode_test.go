package constant

import "testing"

func TestPath2RelayModeGeminiDecision(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{path: "/v1beta/decisions", want: RelayModeGeminiDecision},
		{path: "/v1beta/models/gemini-2.0-flash:generateContent", want: RelayModeGemini},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := Path2RelayMode(tt.path); got != tt.want {
				t.Fatalf("Path2RelayMode(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}
