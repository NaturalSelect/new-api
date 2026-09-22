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
