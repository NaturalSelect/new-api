package model

import (
	"context"
	"strconv"
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
