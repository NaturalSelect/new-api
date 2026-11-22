package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeMediaMessageRedactedThinkingRoundTrip(t *testing.T) {
	raw := `{"type":"redacted_thinking","data":"EmwKAhgB..."}`
	var msg ClaudeMediaMessage
	require.NoError(t, common.UnmarshalJsonStr(raw, &msg))
	require.Equal(t, "redacted_thinking", msg.Type)
	require.Equal(t, "EmwKAhgB...", msg.Data)

	marshaled, err := common.Marshal(msg)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(marshaled))
}
