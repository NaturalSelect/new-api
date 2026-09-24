package model

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

	tokenQuotaWindows = []TokenQuotaWindow{TokenQuotaWindow5h, TokenQuotaWindow7d}
)

func tokenQuotaWindowByName(name string) (TokenQuotaWindow, bool) {
	for _, w := range tokenQuotaWindows {
		if w.Name == name {
			return w, true
		}
	}
	return TokenQuotaWindow{}, false
}

// expiresAt is the unix second at which a bucket stops counting toward the window.
func (w TokenQuotaWindow) expiresAt(bucket int64) int64 {
	return bucket + w.BucketSeconds + w.Seconds
}

func (w TokenQuotaWindow) isLive(bucket int64, now int64) bool {
	return w.expiresAt(bucket) > now
}

func (w TokenQuotaWindow) retention() time.Duration {
	return time.Duration(w.Seconds+w.BucketSeconds) * time.Second
}

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

// addTokenWindowUsage records a charge for a token in the current bucket of every rolling
// window. Best-effort: failures are logged, never returned to the caller, since quota
// accounting must not block the billing hot path.
func addTokenWindowUsage(tokenId int, amount int64) {
	if amount <= 0 {
		return
	}
	for _, w := range tokenQuotaWindows {
		if common.RedisEnabled {
			if err := redisAddTokenWindowUsage(tokenId, w, amount); err != nil {
				common.SysLog("failed to add token window usage: " + err.Error())
			}
		} else {
			memoryAddTokenWindowUsage(tokenId, w, amount)
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

func summarizeTokenWindowBuckets(w TokenQuotaWindow, buckets map[int64]int64, now int64) TokenWindowUsage {
	var used int64
	var resetAt int64
	for bucket, value := range buckets {
		if value <= 0 || !w.isLive(bucket, now) {
			continue
		}
		used += value
		if at := w.expiresAt(bucket); resetAt == 0 || at < resetAt {
			resetAt = at
		}
	}
	return TokenWindowUsage{Used: used, ResetAt: resetAt}
}

func tokenWindowRedisKey(tokenId int, w TokenQuotaWindow) string {
	return "token_quota_window:" + strconv.Itoa(tokenId) + ":" + w.Name
}

func parseRedisTokenWindowBuckets(fields map[string]string) map[int64]int64 {
	buckets := make(map[int64]int64, len(fields))
	for field, valueStr := range fields {
		bucket, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			continue
		}
		value, err := strconv.ParseInt(valueStr, 10, 64)
		if err != nil {
			continue
		}
		buckets[bucket] = value
	}
	return buckets
}

func redisAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, amount int64) error {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	field := strconv.FormatInt(bucketStart(tokenWindowNow(), w.BucketSeconds), 10)

	txn := common.RDB.TxPipeline()
	txn.HIncrBy(ctx, key, field, amount)
	txn.Expire(ctx, key, w.retention())
	_, err := txn.Exec(ctx)
	return err
}

func redisGetTokenWindowUsage(tokenId int, w TokenQuotaWindow) (TokenWindowUsage, error) {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	fields, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return TokenWindowUsage{}, err
	}

	now := tokenWindowNow().Unix()
	buckets := parseRedisTokenWindowBuckets(fields)
	var expiredFields []string
	for bucket := range buckets {
		if !w.isLive(bucket, now) {
			expiredFields = append(expiredFields, strconv.FormatInt(bucket, 10))
		}
	}
	if len(expiredFields) > 0 {
		common.RDB.HDel(ctx, key, expiredFields...)
	}
	return summarizeTokenWindowBuckets(w, buckets, now), nil
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
	for tokenId, windows := range s.buckets {
		for name, buckets := range windows {
			w, known := tokenQuotaWindowByName(name)
			for bucket := range buckets {
				if !known || !w.isLive(bucket, now) {
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

// windowBucketsLocked returns the bucket map for tokenId/w, creating it when create is set.
// Caller must hold s.mutex.
func (s *tokenWindowMemoryStore) windowBucketsLocked(tokenId int, w TokenQuotaWindow, create bool) map[int64]int64 {
	windows, ok := s.buckets[tokenId]
	if !ok {
		if !create {
			return nil
		}
		windows = make(map[string]map[int64]int64)
		s.buckets[tokenId] = windows
	}
	buckets, ok := windows[w.Name]
	if !ok && create {
		buckets = make(map[int64]int64)
		windows[w.Name] = buckets
	}
	return buckets
}

func memoryAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, amount int64) {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	buckets := tokenWindowMemory.windowBucketsLocked(tokenId, w, true)
	buckets[bucketStart(tokenWindowNow(), w.BucketSeconds)] += amount
}

func memoryGetTokenWindowUsage(tokenId int, w TokenQuotaWindow) TokenWindowUsage {
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	buckets := tokenWindowMemory.windowBucketsLocked(tokenId, w, false)
	return summarizeTokenWindowBuckets(w, buckets, tokenWindowNow().Unix())
}

// persistence: periodic snapshot + restore
//
// Redis/memory above are the only source of truth while the process is up. The functions
// below keep a durable, best-effort per-bucket copy in token_window_buckets purely so a
// Redis/memory loss (restart, eviction, TTL expiry) resumes from the last snapshot instead
// of silently resetting every token's usage to zero. Buckets are stored individually so
// each restored bucket still leaves its window at the moment it originally would have.

// TokenWindowBucket is one persisted rolling-window bucket of a token's usage.
type TokenWindowBucket struct {
	TokenId     int    `gorm:"primaryKey;autoIncrement:false"`
	WindowName  string `gorm:"primaryKey;type:varchar(16)"`
	BucketStart int64  `gorm:"primaryKey;autoIncrement:false;index"`
	Used        int64  `gorm:"not null;default:0"`
}

func (TokenWindowBucket) TableName() string {
	return "token_window_buckets"
}

// FlushActiveTokenWindowUsage persists a snapshot of every currently-active token's
// rolling window buckets and prunes rows that have left every window. Returns the number
// of tokens whose snapshot was written.
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

	now := tokenWindowNow().Unix()
	for _, w := range tokenQuotaWindows {
		err := DB.Where("window_name = ? AND bucket_start <= ?", w.Name, now-w.Seconds-w.BucketSeconds).
			Delete(&TokenWindowBucket{}).Error
		if err != nil {
			common.SysLog("failed to prune expired token window buckets: " + err.Error())
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

func loadTokenWindowBuckets(tokenId int, w TokenQuotaWindow) (map[int64]int64, error) {
	if common.RedisEnabled {
		fields, err := common.RDB.HGetAll(context.Background(), tokenWindowRedisKey(tokenId, w)).Result()
		if err != nil {
			return nil, err
		}
		return parseRedisTokenWindowBuckets(fields), nil
	}
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()
	buckets := make(map[int64]int64)
	for bucket, value := range tokenWindowMemory.windowBucketsLocked(tokenId, w, false) {
		buckets[bucket] = value
	}
	return buckets, nil
}

type tokenWindowBucketKey struct {
	window string
	bucket int64
}

// flushTokenWindowSnapshot syncs tokenId's persisted buckets to its live ones: changed
// buckets are upserted and buckets that were refunded to zero or have expired are deleted.
// Best-effort like addTokenWindowUsage: failures are logged, not returned, so one token
// failing to persist never blocks the rest of the flush.
func flushTokenWindowSnapshot(tokenId int) bool {
	now := tokenWindowNow().Unix()
	live := make(map[tokenWindowBucketKey]int64)
	for _, w := range tokenQuotaWindows {
		buckets, err := loadTokenWindowBuckets(tokenId, w)
		if err != nil {
			common.SysLog("failed to read token " + w.Name + " window usage during flush: " + err.Error())
			return false
		}
		for bucket, value := range buckets {
			if value > 0 && w.isLive(bucket, now) {
				live[tokenWindowBucketKey{window: w.Name, bucket: bucket}] = value
			}
		}
	}

	var persisted []TokenWindowBucket
	if err := DB.Where("token_id = ?", tokenId).Find(&persisted).Error; err != nil {
		common.SysLog("failed to read persisted token window buckets: " + err.Error())
		return false
	}
	var upserts []TokenWindowBucket
	var stale []TokenWindowBucket
	for _, row := range persisted {
		key := tokenWindowBucketKey{window: row.WindowName, bucket: row.BucketStart}
		value, ok := live[key]
		if !ok {
			stale = append(stale, row)
			continue
		}
		if value != row.Used {
			upserts = append(upserts, TokenWindowBucket{TokenId: tokenId, WindowName: row.WindowName, BucketStart: row.BucketStart, Used: value})
		}
		delete(live, key)
	}
	for key, value := range live {
		upserts = append(upserts, TokenWindowBucket{TokenId: tokenId, WindowName: key.window, BucketStart: key.bucket, Used: value})
	}
	if len(upserts) == 0 && len(stale) == 0 {
		return false
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		for _, row := range stale {
			err := tx.Where("token_id = ? AND window_name = ? AND bucket_start = ?", row.TokenId, row.WindowName, row.BucketStart).
				Delete(&TokenWindowBucket{}).Error
			if err != nil {
				return err
			}
		}
		if len(upserts) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "token_id"}, {Name: "window_name"}, {Name: "bucket_start"}},
			DoUpdates: clause.AssignmentColumns([]string{"used"}),
		}).Create(&upserts).Error
	})
	if err != nil {
		common.SysLog("failed to persist token window quota snapshot: " + err.Error())
		return false
	}
	return true
}

// RestoreSkip records why one persisted bucket was not freshly seeded during a
// RestoreTokenWindowUsageFromDB run, so callers can log the specific token and reason
// instead of only a bare count.
type RestoreSkip struct {
	TokenId     int
	Window      string
	BucketStart int64
	Reason      string
	// Failed is true when a backend error caused the skip (a real problem worth alerting
	// on). It is false when the bucket was already live (e.g. live traffic or an earlier
	// restore already recorded it), which is an expected, harmless outcome.
	Failed bool
}

// RestoreResult summarizes a RestoreTokenWindowUsageFromDB run.
type RestoreResult struct {
	Candidates int // tokens with at least one persisted bucket still inside its window
	Restored   int // tokens where at least one bucket was freshly seeded
	Skips      []RestoreSkip
}

// RestoreTokenWindowUsageFromDB reseeds Redis/memory from the last flushed snapshot with
// every persisted bucket that is still inside its window. Must run once at startup, before
// traffic flows.
func RestoreTokenWindowUsageFromDB() (RestoreResult, error) {
	now := tokenWindowNow().Unix()
	candidates := make(map[int]struct{})
	restored := make(map[int]struct{})
	var result RestoreResult
	for _, w := range tokenQuotaWindows {
		var rows []TokenWindowBucket
		err := DB.Where("window_name = ? AND bucket_start > ? AND used > 0", w.Name, now-w.Seconds-w.BucketSeconds).
			Find(&rows).Error
		if err != nil {
			return RestoreResult{}, err
		}
		for _, row := range rows {
			candidates[row.TokenId] = struct{}{}
			outcome := seedTokenWindowUsage(row.TokenId, w, row.BucketStart, row.Used)
			if outcome.seeded {
				restored[row.TokenId] = struct{}{}
				continue
			}
			result.Skips = append(result.Skips, RestoreSkip{
				TokenId:     row.TokenId,
				Window:      w.Name,
				BucketStart: row.BucketStart,
				Reason:      outcome.reason,
				Failed:      outcome.failed,
			})
		}
	}
	result.Candidates = len(candidates)
	result.Restored = len(restored)
	return result, nil
}

// seedOutcome reports what happened when seeding a single token/window bucket from a DB
// snapshot, distinguishing a harmless skip (bucket already live) from a real backend
// failure, so RestoreTokenWindowUsageFromDB can report each one accurately.
type seedOutcome struct {
	seeded bool
	failed bool // only meaningful when seeded is false
	reason string
}

// seedTokenWindowUsage restores one persisted bucket. It only writes when that exact bucket
// is still empty, so it can never double-count usage that live traffic (or an earlier
// restore) already recorded.
func seedTokenWindowUsage(tokenId int, w TokenQuotaWindow, bucket int64, used int64) seedOutcome {
	if common.RedisEnabled {
		return redisSeedTokenWindowUsage(tokenId, w, bucket, used)
	}
	return memorySeedTokenWindowUsage(tokenId, w, bucket, used)
}

func redisSeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, bucket int64, used int64) seedOutcome {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	field := strconv.FormatInt(bucket, 10)
	ok, err := common.RDB.HSetNX(ctx, key, field, used).Result()
	if err != nil {
		return seedOutcome{failed: true, reason: "redis error: " + err.Error()}
	}
	if !ok {
		return seedOutcome{reason: "bucket already live, not overwritten"}
	}
	common.RDB.Expire(ctx, key, w.retention())
	return seedOutcome{seeded: true}
}

func memorySeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, bucket int64, used int64) seedOutcome {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	buckets := tokenWindowMemory.windowBucketsLocked(tokenId, w, true)
	if _, exists := buckets[bucket]; exists {
		return seedOutcome{reason: "bucket already live, not overwritten"}
	}
	buckets[bucket] = used
	return seedOutcome{seeded: true}
}
