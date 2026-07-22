package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// resetTokenStatsCacheWatermark clears the in-memory watermark option after the test,
// since common.OptionMap is process-global state that truncateTables' DB-only cleanup
// does not touch — without this, a watermark set by one test would leak into every
// later test's GetTokenStatsCacheWatermark() call (including the existing raw-scan
// tests in key_distribution_test.go / token_distribution_test.go, which all implicitly
// assume "no cache coverage").
func resetTokenStatsCacheWatermark(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, tokenStatsCacheWatermarkOptionKey)
		common.OptionMapRWMutex.Unlock()
	})
}

func TestBucketTimestampToDay(t *testing.T) {
	cases := []struct {
		name      string
		timestamp int64
		want      int64
	}{
		{"zero", 0, 0},
		{"negative", -100, 0},
		{"exact_day_boundary", 172800, 172800},
		{"mid_day", 172800 + 3661, 172800},
		{"just_before_next_day", 172800 + 86399, 172800},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, BucketTimestampToDay(tc.timestamp))
		})
	}
}

func TestGetTokenStatsCacheWatermark_DefaultsToToday(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	require.Equal(t, today, GetTokenStatsCacheWatermark())
}

func TestAdvanceTokenStatsCacheWatermark_PersistsAndReads(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	target := today - 5*86400
	require.NoError(t, AdvanceTokenStatsCacheWatermark(target))
	require.Equal(t, target, GetTokenStatsCacheWatermark())
}

// A future/garbage watermark value must never push the cache-covered window past
// today, since today's bucket is always still being written to.
func TestGetTokenStatsCacheWatermark_ClampsToToday(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	require.NoError(t, AdvanceTokenStatsCacheWatermark(today+10*86400))
	require.Equal(t, today, GetTokenStatsCacheWatermark())
}

// tokenStatsCacheZonesFor's day-boundary splitting is the trickiest logic in the
// cache read path, so it gets a direct table-driven test with hand-computed expected
// zones (relative to the real "today"/watermark) rather than relying solely on the
// indirect end-to-end coverage elsewhere in this file.
func TestTokenStatsCacheZonesFor(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	watermark := today - 10*86400
	require.NoError(t, AdvanceTokenStatsCacheWatermark(watermark))

	cases := []struct {
		name                                string
		start, end                          int64
		wantBeforeOk                        bool
		wantBeforeStart, wantBeforeEnd      int64
		wantCacheOk                         bool
		wantCacheFirstDay, wantCacheLastDay int64
		wantAfterOk                         bool
		wantAfterStart, wantAfterEnd        int64
	}{
		{
			name:  "unbounded_spans_all_three_zones",
			start: 0, end: 0,
			wantBeforeOk: true, wantBeforeStart: 0, wantBeforeEnd: watermark - 1,
			wantCacheOk: true, wantCacheFirstDay: watermark, wantCacheLastDay: today - 86400,
			wantAfterOk: true, wantAfterStart: today, wantAfterEnd: 0,
		},
		{
			name:  "fully_within_cache_window_day_aligned",
			start: watermark, end: today - 1,
			wantBeforeOk: false,
			wantCacheOk:  true, wantCacheFirstDay: watermark, wantCacheLastDay: today - 86400,
			wantAfterOk: false,
		},
		{
			name:  "entirely_before_watermark",
			start: 0, end: watermark - 1,
			wantBeforeOk: true, wantBeforeStart: 0, wantBeforeEnd: watermark - 1,
			wantCacheOk: false,
			wantAfterOk: false,
		},
		{
			name:  "entirely_in_today_or_future",
			start: today, end: 0,
			// No day is both cache-covered and non-today, so CacheOk is false and the
			// whole range collapses into a single raw scan via Before (avoiding a
			// redundant second scan through After for the same data).
			wantBeforeOk: true, wantBeforeStart: today, wantBeforeEnd: 0,
			wantCacheOk: false,
			wantAfterOk: false,
		},
		{
			name:  "partial_start_day_pushed_to_before",
			start: watermark + 43200, end: 0, // noon on the watermark's first day
			wantBeforeOk: true, wantBeforeStart: watermark + 43200, wantBeforeEnd: watermark + 86399,
			wantCacheOk: true, wantCacheFirstDay: watermark + 86400, wantCacheLastDay: today - 86400,
			wantAfterOk: true, wantAfterStart: today, wantAfterEnd: 0,
		},
		{
			name:  "partial_end_day_pushed_to_after",
			start: 0, end: today + 43200, // noon today, i.e. requested end is mid-day today
			wantBeforeOk: true, wantBeforeStart: 0, wantBeforeEnd: watermark - 1,
			wantCacheOk: true, wantCacheFirstDay: watermark, wantCacheLastDay: today - 86400,
			wantAfterOk: true, wantAfterStart: today, wantAfterEnd: today + 43200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := tokenStatsCacheZonesFor(tc.start, tc.end)
			require.Equal(t, tc.wantBeforeOk, z.BeforeOk, "BeforeOk")
			if tc.wantBeforeOk {
				require.Equal(t, tc.wantBeforeStart, z.BeforeStart, "BeforeStart")
				require.Equal(t, tc.wantBeforeEnd, z.BeforeEnd, "BeforeEnd")
			}
			require.Equal(t, tc.wantCacheOk, z.CacheOk, "CacheOk")
			if tc.wantCacheOk {
				require.Equal(t, tc.wantCacheFirstDay, z.CacheFirstDay, "CacheFirstDay")
				require.Equal(t, tc.wantCacheLastDay, z.CacheLastDay, "CacheLastDay")
			}
			require.Equal(t, tc.wantAfterOk, z.AfterOk, "AfterOk")
			if tc.wantAfterOk {
				require.Equal(t, tc.wantAfterStart, z.AfterStart, "AfterStart")
				require.Equal(t, tc.wantAfterEnd, z.AfterEnd, "AfterEnd")
			}
		})
	}
}

func findTokenStatsCacheRow(t *testing.T, day int64, userId int, tokenId int, tokenName string, modelName string) *TokenStatsCache {
	t.Helper()
	var row TokenStatsCache
	err := LOG_DB.Where("day = ? AND user_id = ? AND token_id = ? AND token_name = ? AND model_name = ?",
		day, userId, tokenId, tokenName, modelName).First(&row).Error
	if err != nil {
		return nil
	}
	return &row
}

// upsertTokenStatsCacheForLog must skip non-consume log types entirely (no row
// created), since the cache only ever tracks billed usage.
func TestUpsertTokenStatsCacheForLog_IgnoresNonConsumeLogs(t *testing.T) {
	truncateTables(t)

	log := &Log{UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeManage, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4"}
	upsertTokenStatsCacheForLog(log)

	require.Nil(t, findTokenStatsCacheRow(t, BucketTimestampToDay(1000), 1, 10, "key-a", "gpt-4"))
}

// OpenAI semantic: prompt_tokens already includes cache_read, so InputTokens must be
// stored unchanged (cache_read must not be re-added), matching scanKeyDistribution's
// normalization exactly.
func TestUpsertTokenStatsCacheForLog_OpenAISemantic(t *testing.T) {
	truncateTables(t)

	log := &Log{
		UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeConsume,
		TokenId: 10, TokenName: "key-a", ModelName: "gpt-4",
		PromptTokens: 100, CompletionTokens: 50,
		Other: common.MapToJsonStr(map[string]interface{}{
			"cache_tokens":       20,
			"cache_write_tokens": 5,
		}),
	}
	upsertTokenStatsCacheForLog(log)

	row := findTokenStatsCacheRow(t, BucketTimestampToDay(1000), 1, 10, "key-a", "gpt-4")
	require.NotNil(t, row)
	require.EqualValues(t, 100, row.InputTokens)
	require.EqualValues(t, 50, row.OutputTokens)
	require.EqualValues(t, 20, row.CacheReadTokens)
	require.EqualValues(t, 5, row.CacheWriteTokens)
	require.EqualValues(t, 1, row.Count)
	require.Equal(t, "alice", row.Username)
}

// Claude/Anthropic semantic: prompt_tokens is text-only, so cache_read must be added
// back into InputTokens, matching scanKeyDistribution/scanTokenDistribution exactly.
func TestUpsertTokenStatsCacheForLog_AnthropicSemantic(t *testing.T) {
	truncateTables(t)

	log := &Log{
		UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeConsume,
		TokenId: 10, TokenName: "key-a", ModelName: "claude-3-5-sonnet",
		PromptTokens: 100, CompletionTokens: 50,
		Other: common.MapToJsonStr(map[string]interface{}{
			"cache_tokens":   30,
			"usage_semantic": "anthropic",
		}),
	}
	upsertTokenStatsCacheForLog(log)

	row := findTokenStatsCacheRow(t, BucketTimestampToDay(1000), 1, 10, "key-a", "claude-3-5-sonnet")
	require.NotNil(t, row)
	require.EqualValues(t, 130, row.InputTokens) // 100 text-only + 30 cache_read
	require.EqualValues(t, 30, row.CacheReadTokens)
}

// Two writes for the same (day, user, token, model) key must accumulate additively
// rather than the second overwriting the first — this is what lets a live billing
// write and a backfill of the same day compose safely.
func TestUpsertTokenStatsCacheForLog_AccumulatesAcrossCalls(t *testing.T) {
	truncateTables(t)

	log1 := &Log{UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeConsume, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4", PromptTokens: 100, CompletionTokens: 50}
	log2 := &Log{UserId: 1, Username: "alice", CreatedAt: 1010, Type: LogTypeConsume, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4", PromptTokens: 200, CompletionTokens: 80}
	upsertTokenStatsCacheForLog(log1)
	upsertTokenStatsCacheForLog(log2)

	row := findTokenStatsCacheRow(t, BucketTimestampToDay(1000), 1, 10, "key-a", "gpt-4")
	require.NotNil(t, row)
	require.EqualValues(t, 300, row.InputTokens)
	require.EqualValues(t, 130, row.OutputTokens)
	require.EqualValues(t, 2, row.Count)
}

// Different users/tokens/models on the same day must remain separate rows, not
// collapsed into one — the cache's grain must stay fine enough to reconstruct both
// Token and Key Distribution's grouping dimensions.
func TestUpsertTokenStatsCacheForLog_SeparatesByDimensions(t *testing.T) {
	truncateTables(t)

	upsertTokenStatsCacheForLog(&Log{UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeConsume, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4", PromptTokens: 100})
	upsertTokenStatsCacheForLog(&Log{UserId: 2, Username: "bob", CreatedAt: 1000, Type: LogTypeConsume, TokenId: 20, TokenName: "key-b", ModelName: "gpt-4", PromptTokens: 200})
	upsertTokenStatsCacheForLog(&Log{UserId: 1, Username: "alice", CreatedAt: 1000, Type: LogTypeConsume, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4o", PromptTokens: 300})

	var count int64
	require.NoError(t, LOG_DB.Model(&TokenStatsCache{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
}

// BackfillTokenStatsCacheDay's output must match a full raw scan of the same day
// exactly, for both Token and Key Distribution's aggregation dimensions — the cache is
// a derived speed-up, never a different source of truth.
func TestBackfillTokenStatsCacheDay_MatchesRawScan(t *testing.T) {
	truncateTables(t)

	const day = 10 * 86400
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, day+100)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 200, 80, day+3700)
	createConsumeLogWithOther(t, 2, "bob", 20, "key-b", "claude-3-5-sonnet", 100, 50, day+7300, map[string]interface{}{
		"cache_tokens":   30,
		"usage_semantic": "anthropic",
	})
	// Outside the target day: must not be included.
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 999, 999, day+86400+10)

	wantTokenDist, err := scanTokenDistribution(day, day+86399, "")
	require.NoError(t, err)
	wantKeyDist, err := scanKeyDistribution(day, day+86399, keyDistributionFilter{})
	require.NoError(t, err)

	scanned, err := BackfillTokenStatsCacheDay(day)
	require.NoError(t, err)
	require.EqualValues(t, 3, scanned)

	gotTokenDist, err := queryTokenStatsCacheForTokenDistribution(day, day, "")
	require.NoError(t, err)
	gotKeyDist, err := queryTokenStatsCacheForKeyDistribution(day, day, keyDistributionFilter{})
	require.NoError(t, err)

	// Token Distribution: cache is day-bucketed while the raw scan is hour-bucketed
	// (an intentional, documented trade-off — see TokenStatsCache's doc comment), so
	// compare per-model totals rather than per-bucket rows.
	wantByModel := map[string]int{}
	for _, item := range wantTokenDist {
		wantByModel[item.ModelName] += item.InputTokens
	}
	gotByModel := map[string]int{}
	for _, item := range gotTokenDist {
		gotByModel[item.ModelName] += item.InputTokens
	}
	require.Equal(t, wantByModel, gotByModel)

	// Key Distribution has no time dimension in its output, so the cache must match
	// the raw scan row-for-row exactly (no granularity loss).
	sortKeyDistributionData(wantKeyDist)
	sortKeyDistributionData(gotKeyDist)
	require.Equal(t, len(wantKeyDist), len(gotKeyDist))
	for i := range wantKeyDist {
		require.Equal(t, wantKeyDist[i].TokenId, gotKeyDist[i].TokenId)
		require.Equal(t, wantKeyDist[i].TokenName, gotKeyDist[i].TokenName)
		require.Equal(t, wantKeyDist[i].ModelName, gotKeyDist[i].ModelName)
		require.Equal(t, wantKeyDist[i].InputTokens, gotKeyDist[i].InputTokens)
		require.Equal(t, wantKeyDist[i].OutputTokens, gotKeyDist[i].OutputTokens)
		require.Equal(t, wantKeyDist[i].CacheReadTokens, gotKeyDist[i].CacheReadTokens)
		require.Equal(t, wantKeyDist[i].TotalTokens, gotKeyDist[i].TotalTokens)
	}
}

// Re-running the backfill for the same day (e.g. after a crash, or a manual retry)
// must recompute the same authoritative total, not add onto the previous attempt.
func TestBackfillTokenStatsCacheDay_Idempotent(t *testing.T) {
	truncateTables(t)

	const day = 20 * 86400
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, day+100)

	_, err := BackfillTokenStatsCacheDay(day)
	require.NoError(t, err)
	_, err = BackfillTokenStatsCacheDay(day)
	require.NoError(t, err)

	row := findTokenStatsCacheRow(t, day, 1, 10, "key-a", "gpt-4")
	require.NotNil(t, row)
	require.EqualValues(t, 100, row.InputTokens)
	require.EqualValues(t, 1, row.Count)
}

// A live increment landing on a day that has already been backfilled must add onto
// the backfilled total rather than being lost or double-counted — this is the
// concurrency guarantee a backdated Poe log sync write depends on.
func TestBackfillTokenStatsCacheDay_ComposesWithConcurrentLiveWrite(t *testing.T) {
	truncateTables(t)

	const day = 30 * 86400
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, day+100)

	_, err := BackfillTokenStatsCacheDay(day)
	require.NoError(t, err)

	// Simulate a backdated live write for the same day arriving after backfill.
	upsertTokenStatsCacheForLog(&Log{UserId: 1, Username: "alice", CreatedAt: day + 200, Type: LogTypeConsume, TokenId: 10, TokenName: "key-a", ModelName: "gpt-4", PromptTokens: 20, CompletionTokens: 10})

	row := findTokenStatsCacheRow(t, day, 1, 10, "key-a", "gpt-4")
	require.NotNil(t, row)
	require.EqualValues(t, 120, row.InputTokens)
	require.EqualValues(t, 60, row.OutputTokens)
	require.EqualValues(t, 2, row.Count)
}

// End-to-end: once the watermark covers a historical day, GetTokenDistribution must
// still return the same per-model totals as before caching — the cache must only
// speed up the read, never change what it returns.
func TestGetTokenDistribution_MergesCacheAndRawWithoutChangingTotals(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	pastDay := today - 3*86400

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, pastDay+100)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 200, 80, pastDay+3700)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 50, 20, common.GetTimestamp())

	wantByModel := map[string]int{}
	before, err := GetTokenDistribution(0, 0, "")
	require.NoError(t, err)
	for _, item := range before {
		wantByModel[item.ModelName] += item.InputTokens
	}

	_, err = BackfillTokenStatsCacheDay(pastDay)
	require.NoError(t, err)
	require.NoError(t, AdvanceTokenStatsCacheWatermark(pastDay))

	after, err := GetTokenDistribution(0, 0, "")
	require.NoError(t, err)
	gotByModel := map[string]int{}
	for _, item := range after {
		gotByModel[item.ModelName] += item.InputTokens
	}
	require.Equal(t, wantByModel, gotByModel)
}

// Same end-to-end guarantee for Key Distribution, which — unlike Token Distribution —
// has no time bucket in its output, so results must match exactly, row for row.
func TestGetKeyDistribution_MergesCacheAndRawWithoutChangingResults(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	pastDay := today - 3*86400

	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, pastDay+100)
	createConsumeLog(t, 2, "bob", 20, "key-b", "gpt-4o", 200, 80, pastDay+3700)
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 50, 20, common.GetTimestamp())

	before, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)

	_, err = BackfillTokenStatsCacheDay(pastDay)
	require.NoError(t, err)
	require.NoError(t, AdvanceTokenStatsCacheWatermark(pastDay))

	after, err := GetKeyDistribution(0, 0, "")
	require.NoError(t, err)

	sortKeyDistributionData(before)
	sortKeyDistributionData(after)
	require.Equal(t, len(before), len(after))
	for i := range before {
		require.Equal(t, before[i].TokenId, after[i].TokenId)
		require.Equal(t, before[i].ModelName, after[i].ModelName)
		require.Equal(t, before[i].InputTokens, after[i].InputTokens)
		require.Equal(t, before[i].OutputTokens, after[i].OutputTokens)
		require.Equal(t, before[i].TotalTokens, after[i].TotalTokens)
		require.Equal(t, before[i].Count, after[i].Count)
	}
}

// A request whose start/end falls mid-day on a cache-covered boundary day must not
// use the cache for that partial day — the day-granularity cache row cannot answer a
// sub-day question, so it must fall back to the raw scan for that one day instead of
// silently over- or under-counting.
func TestGetKeyDistribution_PartialBoundaryDayFallsBackToRawScan(t *testing.T) {
	truncateTables(t)
	resetTokenStatsCacheWatermark(t)

	today := BucketTimestampToDay(common.GetTimestamp())
	pastDay := today - 5*86400

	// Two entries on the same day: one before noon, one after.
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 100, 50, pastDay+3600)  // 01:00
	createConsumeLog(t, 1, "alice", 10, "key-a", "gpt-4", 200, 80, pastDay+50000) // ~13:53

	_, err := BackfillTokenStatsCacheDay(pastDay)
	require.NoError(t, err)
	require.NoError(t, AdvanceTokenStatsCacheWatermark(pastDay))

	// Query starting at noon on pastDay: only the second entry should be included.
	result, err := GetKeyDistribution(pastDay+43200, 0, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 200, result[0].InputTokens)
	require.Equal(t, 80, result[0].OutputTokens)
	require.Equal(t, 1, result[0].Count)
}

// RecordTaskBillingLog must update the cache for consume entries (so task billing —
// e.g. Midjourney/Suno differential settlement — is reflected in Token/Key
// Distribution) but must not do so for refund entries, which are not consumption.
func TestRecordTaskBillingLog_UpdatesTokenStatsCacheOnlyForConsume(t *testing.T) {
	truncateTables(t)

	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId: 1, LogType: LogTypeConsume, ModelName: "midjourney", Quota: 100, TokenId: 10,
	})
	RecordTaskBillingLog(RecordTaskBillingLogParams{
		UserId: 1, LogType: LogTypeRefund, ModelName: "midjourney", Quota: 50, TokenId: 10,
	})

	var count int64
	require.NoError(t, LOG_DB.Model(&TokenStatsCache{}).Count(&count).Error)
	require.EqualValues(t, 1, count)

	var row TokenStatsCache
	require.NoError(t, LOG_DB.Where("user_id = ? AND token_id = ?", 1, 10).First(&row).Error)
	require.EqualValues(t, 1, row.Count)
}

// RecordPoeConsumeLog must update the cache, including for backdated entries (Poe log
// sync commonly writes historical rows) — otherwise Poe usage would be silently
// undercounted once its day falls under the cache watermark.
func TestRecordPoeConsumeLog_UpdatesTokenStatsCacheForBackdatedEntry(t *testing.T) {
	truncateTables(t)

	backdated := BucketTimestampToDay(common.GetTimestamp()) - 10*86400
	logId := RecordPoeConsumeLog(RecordPoeConsumeLogParams{
		ModelName: "poe-bot", Quota: 100, PromptTokens: 100, CompletionTokens: 50, CreatedAt: backdated + 100,
	})
	require.NotZero(t, logId)

	var count int64
	require.NoError(t, LOG_DB.Model(&TokenStatsCache{}).Where("day = ? AND model_name = ?", BucketTimestampToDay(backdated+100), "poe-bot").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
