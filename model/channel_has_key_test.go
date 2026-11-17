package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelHasKey(t *testing.T) {
	tests := []struct {
		name       string
		channelKey string
		input      string
		want       bool
	}{
		{"single key match", "sk-aaa", "sk-aaa", true},
		{"input is trimmed", "sk-aaa", "  sk-aaa\n", true},
		{"stored key is trimmed", "sk-aaa\n", "sk-aaa", true},
		{"single key mismatch", "sk-aaa", "sk-bbb", false},
		{"partial key does not match", "sk-aaa-long", "sk-aaa", false},
		{"multi-key newline separated", "sk-aaa\nsk-bbb\nsk-ccc", "sk-bbb", true},
		{"multi-key missing", "sk-aaa\nsk-bbb", "sk-ccc", false},
		{"multi-key crlf entry", "sk-aaa\r\nsk-bbb", "sk-aaa", true},
		{"json array element", `["{\"a\":1}","{\"b\":2}"]`, `"{\"b\":2}"`, true},
		{"multi-line json credential as whole key", "{\n  \"a\": 1\n}", "{\n  \"a\": 1\n}", true},
		{"empty input", "sk-aaa", "  ", false},
		{"empty channel key", "", "sk-aaa", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Key: tt.channelKey}
			assert.Equal(t, tt.want, channel.HasKey(tt.input))
		})
	}
}
