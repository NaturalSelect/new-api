package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func TestEstimateRequestTokenReservesTheTypeSafeInputBudget(t *testing.T) {
	const body = `{"model":"jev-latest","state":"s","questions":{"q":{"type":"noul"}}}`
	original := constant.CountToken
	t.Cleanup(func() { constant.CountToken = original })

	tests := []struct {
		name        string
		countToken  bool
		longContext bool
		want        int
	}{
		{name: "standard", countToken: true, want: dto.TypeSafeMaxInputTokens},
		{name: "standard without token counting", countToken: false, want: dto.TypeSafeMaxInputTokens},
		{name: "long context", countToken: true, longContext: true, want: dto.TypeSafeLongContextInputTokens},
		{name: "long context without token counting", countToken: false, longContext: true, want: dto.TypeSafeLongContextInputTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constant.CountToken = tt.countToken
			request, err := dto.NewTypeSafeRequest([]byte(body), false, tt.longContext)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: request}

			got, err := EstimateRequestToken(nil, request.GetTokenCountMeta(), info)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
