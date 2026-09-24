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

var testWindowBase = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func requireWindowUsage(t *testing.T, tokenId int, w TokenQuotaWindow, used int64) TokenWindowUsage {
	t.Helper()
	usage, err := GetTokenWindowUsage(tokenId, w)
	require.NoError(t, err)
	require.EqualValues(t, used, usage.Used)
	return usage
}

func TestBucketStart(t *testing.T) {
	require.EqualValues(t, testWindowBase.Unix(), bucketStart(testWindowBase, 300))
	require.EqualValues(t, testWindowBase.Unix(), bucketStart(testWindowBase.Add(299*time.Second), 300))
	require.EqualValues(t, testWindowBase.Unix()+300, bucketStart(testWindowBase.Add(300*time.Second), 300))
}

func TestTokenWindowUsage_BasicAddAndGet(t *testing.T) {
	newFakeClock(t, testWindowBase)
	tokenId := 1001

	addTokenWindowUsage(tokenId, 100)

	usage5h := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 100)
	require.EqualValues(t, testWindowBase.Add(5*time.Hour+5*time.Minute).Unix(), usage5h.ResetAt)

	usage7d := requireWindowUsage(t, tokenId, TokenQuotaWindow7d, 100)
	require.EqualValues(t, testWindowBase.Add(7*24*time.Hour+time.Hour).Unix(), usage7d.ResetAt)
}

func TestTokenWindowUsage_AccumulatesAcrossBuckets(t *testing.T) {
	clock := newFakeClock(t, testWindowBase)
	tokenId := 1002

	addTokenWindowUsage(tokenId, 50)
	addTokenWindowUsage(tokenId, 30) // same 5h bucket
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 80)

	clock.Advance(300 * time.Second) // roll into the next 5h bucket
	addTokenWindowUsage(tokenId, 20)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 100)
}

func TestTokenWindowUsage_ResetAtIsWhenUsageActuallyLeaves(t *testing.T) {
	clock := newFakeClock(t, testWindowBase)
	tokenId := 1003

	addTokenWindowUsage(tokenId, 100)
	clock.Advance(time.Hour)
	usage := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 100)
	resetAt := time.Unix(usage.ResetAt, 0)
	require.True(t, resetAt.After(clock.Now()), "reset time must be in the future while usage still counts")

	clock.Advance(resetAt.Sub(clock.Now()) - time.Second)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 100)

	clock.Advance(time.Second)
	usage = requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 0)
	require.EqualValues(t, 0, usage.ResetAt)

	// The 7d window only advanced ~5h, so it still holds the usage.
	requireWindowUsage(t, tokenId, TokenQuotaWindow7d, 100)
}

func TestTokenWindowUsage_ResetAtTracksEarliestBucket(t *testing.T) {
	clock := newFakeClock(t, testWindowBase)
	tokenId := 1006

	addTokenWindowUsage(tokenId, 10) // bucket at base
	clock.Advance(300 * time.Second) // next 5h bucket
	addTokenWindowUsage(tokenId, 20)

	usage := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 30)
	require.EqualValues(t, TokenQuotaWindow5h.expiresAt(testWindowBase.Unix()), usage.ResetAt)
}

func TestIncreaseTokenQuota_DoesNotReduceWindowUsage(t *testing.T) {
	truncateTables(t)
	clock := newFakeClock(t, testWindowBase)

	token := &Token{Key: "refund-window-key", UserId: 1, RemainQuota: 1000}
	require.NoError(t, DB.Create(token).Error)

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 100))
	clock.Advance(10 * time.Minute)
	require.NoError(t, IncreaseTokenQuota(token.Id, token.Key, 100))

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, token.Id).Error)
	require.Equal(t, 1000, reloaded.RemainQuota)
	requireWindowUsage(t, token.Id, TokenQuotaWindow5h, 100)
	requireWindowUsage(t, token.Id, TokenQuotaWindow7d, 100)
}

func TestTokenWindowUsage_MultipleTokensIsolated(t *testing.T) {
	newFakeClock(t, testWindowBase)

	addTokenWindowUsage(2001, 100)
	addTokenWindowUsage(2002, 50)

	requireWindowUsage(t, 2001, TokenQuotaWindow5h, 100)
	requireWindowUsage(t, 2002, TokenQuotaWindow5h, 50)
}

func TestTokenWindowUsage_ZeroAmountNoop(t *testing.T) {
	newFakeClock(t, testWindowBase)
	tokenId := 1007

	addTokenWindowUsage(tokenId, 0)

	usage := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 0)
	require.EqualValues(t, 0, usage.ResetAt)
	require.Empty(t, activeMemoryTokenWindowIds())
}

func TestTokenWindowUsage_UnknownTokenReturnsZero(t *testing.T) {
	newFakeClock(t, testWindowBase)

	usage := requireWindowUsage(t, 9999, TokenQuotaWindow5h, 0)
	require.EqualValues(t, 0, usage.ResetAt)
}

func TestTokenWindowMemoryCleanup_UsesEachWindowLength(t *testing.T) {
	clock := newFakeClock(t, testWindowBase)
	tokenId := 1011

	addTokenWindowUsage(tokenId, 100)
	clock.Advance(6 * time.Hour)
	tokenWindowMemory.cleanup()

	tokenWindowMemory.mutex.Lock()
	windows := tokenWindowMemory.buckets[tokenId]
	_, has5h := windows[TokenQuotaWindow5h.Name]
	_, has7d := windows[TokenQuotaWindow7d.Name]
	tokenWindowMemory.mutex.Unlock()
	require.False(t, has5h, "expired 5h buckets should be dropped")
	require.True(t, has7d, "7d buckets are still inside their window")

	clock.Advance(7 * 24 * time.Hour)
	tokenWindowMemory.cleanup()
	require.Empty(t, activeMemoryTokenWindowIds())
}

func persistedTokenWindowBuckets(t *testing.T, tokenId int) []TokenWindowBucket {
	t.Helper()
	var rows []TokenWindowBucket
	require.NoError(t, DB.Where("token_id = ?", tokenId).Order("window_name, bucket_start").Find(&rows).Error)
	return rows
}

func TestFlushActiveTokenWindowUsage_PersistsBuckets(t *testing.T) {
	truncateTables(t)
	clock := newFakeClock(t, testWindowBase)
	tokenId := 3001

	addTokenWindowUsage(tokenId, 100)
	clock.Advance(10 * time.Minute)
	addTokenWindowUsage(tokenId, 20)

	flushed, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 1, flushed)

	require.Equal(t, []TokenWindowBucket{
		{TokenId: tokenId, WindowName: "5h", BucketStart: testWindowBase.Unix(), Used: 100},
		{TokenId: tokenId, WindowName: "5h", BucketStart: testWindowBase.Add(10 * time.Minute).Unix(), Used: 20},
		{TokenId: tokenId, WindowName: "7d", BucketStart: testWindowBase.Unix(), Used: 120},
	}, persistedTokenWindowBuckets(t, tokenId))

	// Nothing changed since the last flush, so nothing is rewritten.
	flushed, err = FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 0, flushed)
}

func TestFlushActiveTokenWindowUsage_SkipsTokensWithNoActivity(t *testing.T) {
	truncateTables(t)
	newFakeClock(t, testWindowBase)

	flushed, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 0, flushed)
}

func TestFlushActiveTokenWindowUsage_RemovesExpiredBucketsOfActiveToken(t *testing.T) {
	truncateTables(t)
	clock := newFakeClock(t, testWindowBase)
	tokenId := 3002

	addTokenWindowUsage(tokenId, 100)
	_, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Len(t, persistedTokenWindowBuckets(t, tokenId), 2)

	// The 5h bucket expires while the token stays active through its 7d bucket.
	clock.Advance(6 * time.Hour)
	flushed, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Equal(t, 1, flushed)
	require.Equal(t, []TokenWindowBucket{
		{TokenId: tokenId, WindowName: "7d", BucketStart: testWindowBase.Unix(), Used: 100},
	}, persistedTokenWindowBuckets(t, tokenId))

	resetTokenWindowMemory()
	clock.Advance(time.Minute)
	_, err = RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 0)
	requireWindowUsage(t, tokenId, TokenQuotaWindow7d, 100)
}

func TestFlushActiveTokenWindowUsage_PrunesExpiredRows(t *testing.T) {
	truncateTables(t)
	clock := newFakeClock(t, testWindowBase)
	tokenId := 3003

	addTokenWindowUsage(tokenId, 100)
	_, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)

	// Memory is lost, so the token never shows up as active again; pruning must still
	// remove its rows once they have left every window.
	resetTokenWindowMemory()
	clock.Advance(8 * 24 * time.Hour)
	_, err = FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	require.Empty(t, persistedTokenWindowBuckets(t, tokenId))
}

func TestRestoreTokenWindowUsageFromDB_ResumesAfterRestart(t *testing.T) {
	truncateTables(t)
	clock := newFakeClock(t, testWindowBase)
	tokenId := 3004

	addTokenWindowUsage(tokenId, 100)
	clock.Advance(2 * time.Hour)
	addTokenWindowUsage(tokenId, 20)
	_, err := FlushActiveTokenWindowUsage()
	require.NoError(t, err)
	before := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 120)

	// Simulate a Redis/process restart losing all buckets.
	resetTokenWindowMemory()
	clock.Advance(time.Minute)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 0)

	result, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, result.Candidates)
	require.Equal(t, 1, result.Restored)
	require.Empty(t, result.Skips)

	after := requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 120)
	require.Equal(t, before.ResetAt, after.ResetAt)
	requireWindowUsage(t, tokenId, TokenQuotaWindow7d, 120)

	// Each restored bucket still expires on its own schedule.
	clock.Advance(time.Unix(after.ResetAt, 0).Sub(clock.Now()))
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 20)
}

func TestRestoreTokenWindowUsageFromDB_SkipsExpiredBuckets(t *testing.T) {
	truncateTables(t)
	newFakeClock(t, testWindowBase)
	tokenId := 3005

	expired := testWindowBase.Add(-6 * time.Hour).Unix()
	require.NoError(t, DB.Create(&TokenWindowBucket{TokenId: tokenId, WindowName: "5h", BucketStart: expired, Used: 80}).Error)

	result, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 0, result.Candidates)
	require.Equal(t, 0, result.Restored)
	require.Empty(t, result.Skips)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 0)
}

func TestRestoreTokenWindowUsageFromDB_DoesNotClobberLiveUsage(t *testing.T) {
	truncateTables(t)
	newFakeClock(t, testWindowBase)
	tokenId := 3006

	earlier := testWindowBase.Add(-time.Hour).Unix()
	require.NoError(t, DB.Create(&TokenWindowBucket{TokenId: tokenId, WindowName: "5h", BucketStart: earlier, Used: 80}).Error)

	// Live traffic already recorded usage in the current bucket before restore runs.
	addTokenWindowUsage(tokenId, 20)

	_, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 100)
}

func TestRestoreTokenWindowUsageFromDB_ReportsSkipWhenBucketAlreadySeeded(t *testing.T) {
	truncateTables(t)
	newFakeClock(t, testWindowBase)
	tokenId := 3007

	bucket := testWindowBase.Add(-time.Hour).Unix()
	require.NoError(t, DB.Create(&TokenWindowBucket{TokenId: tokenId, WindowName: "5h", BucketStart: bucket, Used: 80}).Error)

	first, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, first.Restored)
	require.Empty(t, first.Skips)

	// Same snapshot, same target bucket: the second restore (e.g. a duplicate startup
	// call) must not double count, and must report exactly which bucket it skipped and
	// that the skip was benign (bucket already live), not a failure.
	second, err := RestoreTokenWindowUsageFromDB()
	require.NoError(t, err)
	require.Equal(t, 1, second.Candidates)
	require.Equal(t, 0, second.Restored)
	require.Len(t, second.Skips, 1)
	require.Equal(t, tokenId, second.Skips[0].TokenId)
	require.Equal(t, TokenQuotaWindow5h.Name, second.Skips[0].Window)
	require.Equal(t, bucket, second.Skips[0].BucketStart)
	require.False(t, second.Skips[0].Failed)
	require.NotEmpty(t, second.Skips[0].Reason)

	requireWindowUsage(t, tokenId, TokenQuotaWindow5h, 80)
}
