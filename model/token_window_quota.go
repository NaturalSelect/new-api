package model

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// TokenQuotaWindow describes a rolling window used for per-token quota limiting.
type TokenQuotaWindow struct {
	Name          string
	Seconds       int64
	BucketSeconds int64
}

var (
	TokenQuotaWindow5h = TokenQuotaWindow{Name: "5h", Seconds: 5 * 3600, BucketSeconds: 300}
	TokenQuotaWindow7d = TokenQuotaWindow{Name: "7d", Seconds: 7 * 86400, BucketSeconds: 3600}
)

// TokenWindowUsage is the aggregated usage for a token within a rolling window.
type TokenWindowUsage struct {
	Used    int64
	ResetAt int64 // unix seconds when the earliest non-zero bucket leaves the window; 0 if no usage
}

// tokenWindowNow allows tests to control the clock.
var tokenWindowNow = time.Now

func bucketStart(t time.Time, bucketSeconds int64) int64 {
	sec := t.Unix()
	return sec - sec%bucketSeconds
}

// addTokenWindowUsage records a quota delta (can be negative, e.g. refunds) for a token
// across all rolling windows. Best-effort: failures are logged, never returned to the caller,
// since quota accounting must not block the billing hot path.
func addTokenWindowUsage(tokenId int, delta int64) {
	if delta == 0 {
		return
	}
	for _, w := range []TokenQuotaWindow{TokenQuotaWindow5h, TokenQuotaWindow7d} {
		if common.RedisEnabled {
			if err := redisAddTokenWindowUsage(tokenId, w, delta); err != nil {
				common.SysLog("failed to add token window usage: " + err.Error())
			}
		} else {
			memoryAddTokenWindowUsage(tokenId, w, delta)
		}
	}
}

// GetTokenWindowUsage returns the aggregated usage for a token within the given window.
func GetTokenWindowUsage(tokenId int, w TokenQuotaWindow) (TokenWindowUsage, error) {
	if common.RedisEnabled {
		return redisGetTokenWindowUsage(tokenId, w)
	}
	return memoryGetTokenWindowUsage(tokenId, w), nil
}

func tokenWindowRedisKey(tokenId int, w TokenQuotaWindow) string {
	return "token_quota_window:" + strconv.Itoa(tokenId) + ":" + w.Name
}

func redisAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, delta int64) error {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	bucket := bucketStart(tokenWindowNow(), w.BucketSeconds)
	field := strconv.FormatInt(bucket, 10)

	txn := common.RDB.TxPipeline()
	txn.HIncrBy(ctx, key, field, delta)
	txn.Expire(ctx, key, time.Duration(w.Seconds+w.BucketSeconds)*time.Second)
	_, err := txn.Exec(ctx)
	return err
}

func redisGetTokenWindowUsage(tokenId int, w TokenQuotaWindow) (TokenWindowUsage, error) {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	result, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return TokenWindowUsage{}, err
	}

	now := tokenWindowNow().Unix()
	windowStart := now - w.Seconds

	var used int64
	var resetAt int64
	var expiredFields []string
	for field, valueStr := range result {
		bucket, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			continue
		}
		value, err := strconv.ParseInt(valueStr, 10, 64)
		if err != nil {
			continue
		}
		if bucket+w.BucketSeconds <= windowStart {
			// bucket has fully left the window, safe to clean up
			expiredFields = append(expiredFields, field)
			continue
		}
		used += value
		leavesAt := bucket + w.BucketSeconds
		if resetAt == 0 || leavesAt < resetAt {
			resetAt = leavesAt
		}
	}
	if len(expiredFields) > 0 {
		common.RDB.HDel(ctx, key, expiredFields...)
	}
	if used < 0 {
		used = 0
	}
	if used == 0 {
		resetAt = 0
	}
	return TokenWindowUsage{Used: used, ResetAt: resetAt}, nil
}

// in-memory fallback

type tokenWindowMemoryStore struct {
	mutex   sync.Mutex
	buckets map[int]map[string]map[int64]int64 // tokenId -> windowName -> bucketStart -> value
	once    sync.Once
}

var tokenWindowMemory = &tokenWindowMemoryStore{
	buckets: make(map[int]map[string]map[int64]int64),
}

func (s *tokenWindowMemoryStore) startJanitor() {
	s.once.Do(func() {
		go func() {
			for {
				time.Sleep(10 * time.Minute)
				s.cleanup()
			}
		}()
	})
}

func (s *tokenWindowMemoryStore) cleanup() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	now := tokenWindowNow().Unix()
	maxWindow := TokenQuotaWindow7d.Seconds
	for tokenId, windows := range s.buckets {
		for name, buckets := range windows {
			bucketSeconds := TokenQuotaWindow5h.BucketSeconds
			if name == TokenQuotaWindow7d.Name {
				bucketSeconds = TokenQuotaWindow7d.BucketSeconds
			}
			for bucket := range buckets {
				if bucket+bucketSeconds <= now-maxWindow {
					delete(buckets, bucket)
				}
			}
			if len(buckets) == 0 {
				delete(windows, name)
			}
		}
		if len(windows) == 0 {
			delete(s.buckets, tokenId)
		}
	}
}

func memoryAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, delta int64) {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	windows, ok := tokenWindowMemory.buckets[tokenId]
	if !ok {
		windows = make(map[string]map[int64]int64)
		tokenWindowMemory.buckets[tokenId] = windows
	}
	buckets, ok := windows[w.Name]
	if !ok {
		buckets = make(map[int64]int64)
		windows[w.Name] = buckets
	}
	bucket := bucketStart(tokenWindowNow(), w.BucketSeconds)
	buckets[bucket] += delta
}

func memoryGetTokenWindowUsage(tokenId int, w TokenQuotaWindow) TokenWindowUsage {
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	windows, ok := tokenWindowMemory.buckets[tokenId]
	if !ok {
		return TokenWindowUsage{}
	}
	buckets, ok := windows[w.Name]
	if !ok {
		return TokenWindowUsage{}
	}

	now := tokenWindowNow().Unix()
	windowStart := now - w.Seconds

	var used int64
	var resetAt int64
	for bucket, value := range buckets {
		if bucket+w.BucketSeconds <= windowStart {
			continue
		}
		used += value
		leavesAt := bucket + w.BucketSeconds
		if resetAt == 0 || leavesAt < resetAt {
			resetAt = leavesAt
		}
	}
	if used < 0 {
		used = 0
	}
	if used == 0 {
		resetAt = 0
	}
	return TokenWindowUsage{Used: used, ResetAt: resetAt}
}

// persistence: periodic snapshot + restore
//
// Redis/memory above are the only source of truth while the process is up; nothing here
// ever changes that. The functions below add a durable, best-effort snapshot of that state
// on the tokens row (quota_used_5h/7d, quota_reset_5h/7d) purely so a Redis/memory loss
// (restart, eviction, TTL expiry) degrades to "resume from the last snapshot" instead of
// silently resetting every token's usage to zero.

// FlushActiveTokenWindowUsage persists a snapshot of every currently-active token's
// rolling window usage into its tokens row. Returns the number of tokens flushed.
func FlushActiveTokenWindowUsage() (int, error) {
	var tokenIds []int
	if common.RedisEnabled {
		ids, err := activeRedisTokenWindowIds(context.Background())
		if err != nil {
			return 0, err
		}
		tokenIds = ids
	} else {
		tokenIds = activeMemoryTokenWindowIds()
	}

	flushed := 0
	for _, tokenId := range tokenIds {
		if flushTokenWindowSnapshot(tokenId) {
			flushed++
		}
	}
	return flushed, nil
}

// activeRedisTokenWindowIds scans (never KEYS, to stay safe on large keyspaces) for
// token_quota_window:* keys and returns the distinct token ids with a live bucket.
func activeRedisTokenWindowIds(ctx context.Context) ([]int, error) {
	seen := make(map[int]struct{})
	var cursor uint64
	for {
		keys, next, err := common.RDB.Scan(ctx, cursor, "token_quota_window:*", 200).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			parts := strings.Split(key, ":")
			if len(parts) != 3 {
				continue
			}
			id, err := strconv.Atoi(parts[1])
			if err != nil {
				continue
			}
			seen[id] = struct{}{}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

func activeMemoryTokenWindowIds() []int {
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()
	ids := make([]int, 0, len(tokenWindowMemory.buckets))
	for id := range tokenWindowMemory.buckets {
		ids = append(ids, id)
	}
	return ids
}

// flushTokenWindowSnapshot writes tokenId's current 5h/7d usage into its tokens row.
// Best-effort like addTokenWindowUsage: failures are logged, not returned, so one token
// failing to persist never blocks the rest of the flush.
func flushTokenWindowSnapshot(tokenId int) bool {
	usage5h, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow5h)
	if err != nil {
		common.SysLog("failed to read token 5h window usage during flush: " + err.Error())
		return false
	}
	usage7d, err := GetTokenWindowUsage(tokenId, TokenQuotaWindow7d)
	if err != nil {
		common.SysLog("failed to read token 7d window usage during flush: " + err.Error())
		return false
	}
	if usage5h.Used == 0 && usage7d.Used == 0 {
		return false
	}
	err = DB.Model(&Token{}).Where("id = ?", tokenId).Updates(map[string]interface{}{
		"quota_used_5h":  usage5h.Used,
		"quota_reset_5h": usage5h.ResetAt,
		"quota_used_7d":  usage7d.Used,
		"quota_reset_7d": usage7d.ResetAt,
	}).Error
	if err != nil {
		common.SysLog("failed to persist token window quota snapshot: " + err.Error())
		return false
	}
	return true
}

// RestoreTokenWindowUsageFromDB reseeds Redis/memory from the last flushed snapshot for
// every token whose window hasn't fully elapsed yet. Must run once at startup, before
// traffic flows. Returns the number of tokens restored.
func RestoreTokenWindowUsageFromDB() (int, error) {
	now := tokenWindowNow().Unix()
	var tokens []Token
	err := DB.Select("id", "quota_used_5h", "quota_reset_5h", "quota_used_7d", "quota_reset_7d").
		Where("(quota_used_5h > 0 AND quota_reset_5h > ?) OR (quota_used_7d > 0 AND quota_reset_7d > ?)", now, now).
		Find(&tokens).Error
	if err != nil {
		return 0, err
	}

	restored := 0
	for _, t := range tokens {
		did := false
		if t.QuotaUsed5h > 0 && t.QuotaReset5h > now {
			if seedTokenWindowUsage(t.Id, TokenQuotaWindow5h, t.QuotaUsed5h, t.QuotaReset5h) {
				did = true
			}
		}
		if t.QuotaUsed7d > 0 && t.QuotaReset7d > now {
			if seedTokenWindowUsage(t.Id, TokenQuotaWindow7d, t.QuotaUsed7d, t.QuotaReset7d) {
				did = true
			}
		}
		if did {
			restored++
		}
	}
	return restored, nil
}

// seedTokenWindowUsage restores a single bucket positioned so it leaves the window at
// resetAt, the same instant recorded in the snapshot, holding the snapshotted usage. It
// only writes when that exact bucket is still empty, so it can never double-count usage
// that live traffic (or an earlier restore) already recorded.
func seedTokenWindowUsage(tokenId int, w TokenQuotaWindow, used int64, resetAt int64) bool {
	bucket := resetAt - w.BucketSeconds
	if common.RedisEnabled {
		return redisSeedTokenWindowUsage(tokenId, w, bucket, used)
	}
	return memorySeedTokenWindowUsage(tokenId, w, bucket, used)
}

func redisSeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, bucket int64, used int64) bool {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	field := strconv.FormatInt(bucket, 10)
	ok, err := common.RDB.HSetNX(ctx, key, field, used).Result()
	if err != nil {
		common.SysLog("failed to restore token window usage: " + err.Error())
		return false
	}
	if ok {
		common.RDB.Expire(ctx, key, time.Duration(w.Seconds+w.BucketSeconds)*time.Second)
	}
	return ok
}

func memorySeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, bucket int64, used int64) bool {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	windows, ok := tokenWindowMemory.buckets[tokenId]
	if !ok {
		windows = make(map[string]map[int64]int64)
		tokenWindowMemory.buckets[tokenId] = windows
	}
	buckets, ok := windows[w.Name]
	if !ok {
		buckets = make(map[int64]int64)
		windows[w.Name] = buckets
	}
	if _, exists := buckets[bucket]; exists {
		return false
	}
	buckets[bucket] = used
	return true
}
