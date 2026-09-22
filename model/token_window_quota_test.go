package model

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClock lets tests control tokenWindowNow deterministically.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newFakeClock installs a fake clock as tokenWindowNow and resets the shared
// in-memory window store so tests don't leak state into one another.
func newFakeClock(t *testing.T, start time.Time) *fakeClock {
	t.Helper()
	c := &fakeClock{now: start}

	original := tokenWindowNow
	tokenWindowNow = c.Now

	resetTokenWindowMemory()
	t.Cleanup(func() {
		tokenWindowNow = original
		resetTokenWindowMemory()
	})

	return c
}

func resetTokenWindowMemory() {
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()
	tokenWindowMemory.buckets = make(map[int]map[string]map[int64]int64)
}

func TestBucketStart(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	require.EqualValues(t, base.Unix(), bucketStart(base, 300))
	require.EqualValues(t, base.Unix(), bucketStart(base.Add(299*time.Second), 300))
	require.EqualValues(t, base.Unix()+300, bucketStart(base.Add(300*time.Second), 300))
}

func TestTokenWindowUsage_BasicAddAndGet(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)
	tokenId := 1001

	addTokenWindowUsage(tokenId, 100)

	usage5h, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 100, usage5h.Used)
	require.EqualValues(t, base.Unix()+TokenQuotaWindow5h.BucketSeconds, usage5h.ResetAt)

	usage7d, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow7d)
	require.NoError(t, err)
	require.EqualValues(t, 100, usage7d.Used)
	require.EqualValues(t, base.Unix()+TokenQuotaWindow7d.BucketSeconds, usage7d.ResetAt)
}

func TestTokenWindowUsage_AccumulatesAcrossBuckets(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(t, base)
	tokenId := 1002

	addTokenWindowUsage(tokenId, 50)
	addTokenWindowUsage(tokenId, 30) // same 5h bucket

	usage, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 80, usage.Used)

	clock.Advance(300 * time.Second) // roll into the next 5h bucket
	addTokenWindowUsage(tokenId, 20)

	usage, err = GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 100, usage.Used)
}

func TestTokenWindowUsage_BucketLeavesWindow(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(t, base)
	tokenId := 1003

	addTokenWindowUsage(tokenId, 100)

	// Advance past the 5h window so the only bucket fully leaves it.
	clock.Advance(time.Duration(TokenQuotaWindow5h.Seconds+TokenQuotaWindow5h.BucketSeconds+1) * time.Second)

	usage5h, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage5h.Used)
	require.EqualValues(t, 0, usage5h.ResetAt)

	// The 7d window only advanced ~5h, so it still holds the usage.
	usage7d, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow7d)
	require.NoError(t, err)
	require.EqualValues(t, 100, usage7d.Used)
}

func TestTokenWindowUsage_Refund(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)
	tokenId := 1004

	addTokenWindowUsage(tokenId, 100)
	addTokenWindowUsage(tokenId, -40) // refund

	usage, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 60, usage.Used)
}

func TestTokenWindowUsage_NegativeClampsToZero(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)
	tokenId := 1005

	addTokenWindowUsage(tokenId, -50) // refund with no prior usage recorded

	usage, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage.Used)
	require.EqualValues(t, 0, usage.ResetAt)
}

func TestTokenWindowUsage_ResetAtTracksEarliestBucket(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(t, base)
	tokenId := 1006

	addTokenWindowUsage(tokenId, 10) // bucket at base

	clock.Advance(300 * time.Second) // next 5h bucket
	addTokenWindowUsage(tokenId, 20)

	usage, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 30, usage.Used)
	// The earliest bucket (at base) leaves the window first.
	require.EqualValues(t, base.Unix()+TokenQuotaWindow5h.BucketSeconds, usage.ResetAt)
}

func TestTokenWindowUsage_MultipleTokensIsolated(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	addTokenWindowUsage(2001, 100)
	addTokenWindowUsage(2002, 50)

	usage1, err := GetTokenWindowUsage(2001, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 100, usage1.Used)

	usage2, err := GetTokenWindowUsage(2002, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 50, usage2.Used)
}

func TestTokenWindowUsage_ZeroDeltaNoop(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)
	tokenId := 1007

	addTokenWindowUsage(tokenId, 0)

	usage, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage.Used)
	require.EqualValues(t, 0, usage.ResetAt)
}

func TestTokenWindowUsage_UnknownTokenReturnsZero(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	usage, err := GetTokenWindowUsage(9999, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage.Used)
	require.EqualValues(t, 0, usage.ResetAt)
}

func TestFlushActiveTokenWindowUsage_PersistsSnapshot(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{Key: "flush-test-key", UserId: 1}
	require.NoError(t, DB.Create(token).Error)

	addTokenWindowUsage(token.Id, 100)

	flushed, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 1, flushed)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, token.Id).Error)
	require.EqualValues(t, 100, reloaded.QuotaUsed5h)
	require.EqualValues(t, base.Unix()+TokenQuotaWindow5h.BucketSeconds, reloaded.QuotaReset5h)
	require.EqualValues(t, 100, reloaded.QuotaUsed7d)
	require.EqualValues(t, base.Unix()+TokenQuotaWindow7d.BucketSeconds, reloaded.QuotaReset7d)
}

func TestFlushActiveTokenWindowUsage_SkipsTokensWithNoActivity(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{Key: "flush-idle-key", UserId: 1}
	require.NoError(t, DB.Create(token).Error)
	// No addTokenWindowUsage call: the token never appears in the active set.

	flushed, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 0, flushed)
}

func TestRestoreTokenWindowUsageFromDB_ReseedsUnexpiredSnapshot(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{
		Key:          "restore-test-key",
		UserId:       1,
		QuotaUsed5h:  80,
		QuotaReset5h: base.Unix() + 1000, // still within the 5h window
	}
	require.NoError(t, DB.Create(token).Error)

	// Cache starts empty, simulating a Redis/process restart losing all buckets.
	usage, err := GetTokenWindowUsage(token.Id, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage.Used)

	result, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, result.Candidates)
	require.Equal(t, 1, result.Restored)
	require.Empty(t, result.Skips)

	usage, err = GetTokenWindowUsage(token.Id, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 80, usage.Used)
	require.EqualValues(t, base.Unix()+1000, usage.ResetAt)
}

func TestRestoreTokenWindowUsageFromDB_SkipsExpiredSnapshot(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{
		Key:          "restore-expired-key",
		UserId:       1,
		QuotaUsed5h:  80,
		QuotaReset5h: base.Unix() - 10, // already elapsed before the process even started
	}
	require.NoError(t, DB.Create(token).Error)

	result, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	// The snapshot's reset time has already passed, so it never even qualifies as a
	// restore candidate: this is an ordinary expired token, not a skip worth reporting.
	require.Equal(t, 0, result.Candidates)
	require.Equal(t, 0, result.Restored)
	require.Empty(t, result.Skips)

	usage, err := GetTokenWindowUsage(token.Id, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 0, usage.Used)
}

func TestRestoreTokenWindowUsageFromDB_DoesNotClobberLiveUsage(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{
		Key:          "restore-live-key",
		UserId:       1,
		QuotaUsed5h:  80,
		QuotaReset5h: base.Unix() + 1000,
	}
	require.NoError(t, DB.Create(token).Error)

	// Live traffic already recorded usage in the current bucket before restore runs.
	addTokenWindowUsage(token.Id, 20)

	_, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)

	usage, err := GetTokenWindowUsage(token.Id, TokenQuotaWindow5h)
	require.NoError(t, err)
	// The restored bucket (positioned at reset_at - BucketSeconds) lands on a different
	// bucket than live traffic's current one, so usage adds up instead of being clobbered.
	require.EqualValues(t, 100, usage.Used)
}

func TestRestoreTokenWindowUsageFromDB_ReportsSkipWhenBucketAlreadySeeded(t *testing.T) {
	truncateTables(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newFakeClock(t, base)

	token := &Token{
		Key:          "restore-twice-key",
		UserId:       1,
		QuotaUsed5h:  80,
		QuotaReset5h: base.Unix() + 1000,
	}
	require.NoError(t, DB.Create(token).Error)

	first, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, first.Restored)
	require.Empty(t, first.Skips)

	// Same snapshot, same target bucket: the second restore (e.g. a duplicate startup
	// call) must not double count, and must report exactly which token/window it skipped
	// and that the skip was benign (bucket already live), not a failure.
	second, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, second.Candidates)
	require.Equal(t, 0, second.Restored)
	require.Len(t, second.Skips, 1)
	require.Equal(t, token.Id, second.Skips[0].TokenId)
	require.Equal(t, TokenQuotaWindow5h.Name, second.Skips[0].Window)
	require.False(t, second.Skips[0].Failed)
	require.NotEmpty(t, second.Skips[0].Reason)

	usage, err := GetTokenWindowUsage(token.Id, TokenQuotaWindow5h)
	require.NoError(t, err)
	require.EqualValues(t, 80, usage.Used)
}
