package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func TestBuildIntelligenceScores(t *testing.T) {
	const payload = `{
		"settings": [
			{"model": "m1", "effort": "high"},
			{"model": "m1", "effort": "low"},
			{"model": "m2", "effort": "high"},
			{"model": "m3", "effort": "high"}
		],
		"tasks": [
			{"id": "a", "weight": 0.5},
			{"id": "b", "weight": 0.25},
			{"id": "c", "weight": 0.25}
		],
		"round": {
			"completed_at": "2026-10-23T10:43:22+00:00",
			"results": {
				"m1|high": {
					"a": {"status": "scored", "reward": 1, "seconds": 60, "cost": {"usd": 0.2}},
					"b": {"status": "scored", "reward": 0, "seconds": 120, "cost": {"usd": null}},
					"c": {"status": "scored", "reward": 0.5, "seconds": 180, "cost": {"usd": 0.4}}
				},
				"m1|low": {
					"a": {"status": "scored", "reward": 0, "seconds": 30, "cost": {"usd": null}}
				},
				"m2|high": {
					"a": {"status": "error", "reward": 1, "seconds": 10, "cost": {"usd": 9}},
					"unlisted": {"status": "scored", "reward": 1, "seconds": 10, "cost": {"usd": 9}}
				}
			}
		}
	}`

	var resp intelligenceAPIResponse
	require.NoError(t, common.UnmarshalJsonStr(payload, &resp))

	scores := buildIntelligenceScores(resp)

	require.Len(t, scores, 2, "m2 has no scored listed task and m3 has no results, so both are omitted")

	full := scores[0]
	require.Equal(t, "m1", full.Model)
	require.Equal(t, "high", full.Effort)
	// weighted reward: 0.5*1 + 0.25*0 + 0.25*0.5 = 0.625 of total weight 1.0
	require.InDelta(t, 62.5, full.IQ, 1e-9)
	require.InDelta(t, 0.625, full.Passed, 1e-9)
	require.InDelta(t, 1.0, full.ValidTasks, 1e-9)
	// average over the 3 scored tasks: (60+120+180)/3 = 120s
	require.InDelta(t, 2.0, full.AverageMinutes, 1e-9)
	// average only over the 2 priced tasks: (0.2+0.4)/2
	require.InDelta(t, 0.3, full.AveragePriceUSD, 1e-9)
	require.Equal(t, "2026-10-23T10:43:22+00:00", full.LatestGradedAt)

	partial := scores[1]
	require.Equal(t, "low", partial.Effort)
	// a single scored task normalizes by the weight actually scored, not the full task set
	require.InDelta(t, 0.0, partial.IQ, 1e-9)
	require.InDelta(t, 0.5, partial.ValidTasks, 1e-9)
	require.Zero(t, partial.AveragePriceUSD, "no priced task means no price rather than a zero average of nothing")
}

func TestBuildIntelligenceScoresEmptyResponse(t *testing.T) {
	require.Empty(t, buildIntelligenceScores(intelligenceAPIResponse{}))
}
