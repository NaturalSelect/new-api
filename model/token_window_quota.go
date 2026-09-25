package model

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TokenQuotaWindow describes a fixed-length quota window used for per-token limiting. A
// window opens on a token's first charge after the previous window has ended and lasts
// Seconds; the next charge after it ends opens a fresh window with Used starting at 0.
type TokenQuotaWindow struct {
	Name    string
	Seconds int64
}

var (
	TokenQuotaWindow5h = TokenQuotaWindow{Name: "5h", Seconds: 5 * 3600}
	TokenQuotaWindow7d = TokenQuotaWindow{Name: "7d", Seconds: 7 * 86400}

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

// endsAt is the unix second at which a window that started at start stops counting and its
// usage resets to 0.
func (w TokenQuotaWindow) endsAt(start int64) int64 {
	return start + w.Seconds
}

func (w TokenQuotaWindow) isLive(start int64, now int64) bool {
	return w.endsAt(start) > now
}

// TokenWindowUsage is a token's usage within its current quota window.
type TokenWindowUsage struct {
	Used    int64
	ResetAt int64 // unix seconds when the current window ends and Used resets to 0; 0 if no window is open
}

// tokenWindowNow allows tests to control the clock.
var tokenWindowNow = time.Now

// tokenWindowState is a token's current window for one TokenQuotaWindow: it opened at
// Start and has accumulated Used so far.
type tokenWindowState struct {
	Start int64
	Used  int64
}

// addTokenWindowUsage records a charge for a token against every quota window. Best-effort:
// failures are logged, never returned to the caller, since quota accounting must not block
// the billing hot path.
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
	state, ok, err := readTokenWindowState(tokenId, w)
	if err != nil {
		return TokenWindowUsage{}, err
	}
	return summarizeTokenWindowState(w, state, ok, tokenWindowNow().Unix()), nil
}

// readTokenWindowState returns a token's current window state for w, dispatching to
// Redis or the in-memory store. ok is false when no window has ever been opened (or the
// backend has lost it); callers must still check isLive themselves since a stored state can
// belong to a window that has already ended.
func readTokenWindowState(tokenId int, w TokenQuotaWindow) (tokenWindowState, bool, error) {
	if common.RedisEnabled {
		return redisGetTokenWindowState(tokenId, w)
	}
	state, ok := memoryGetTokenWindowState(tokenId, w)
	return state, ok, nil
}

func summarizeTokenWindowState(w TokenQuotaWindow, state tokenWindowState, ok bool, now int64) TokenWindowUsage {
	if !ok || state.Used <= 0 || !w.isLive(state.Start, now) {
		return TokenWindowUsage{}
	}
	return TokenWindowUsage{Used: state.Used, ResetAt: w.endsAt(state.Start)}
}

const tokenWindowRedisKeyPrefix = "token_quota_fixed_window"

func tokenWindowRedisKey(tokenId int, w TokenQuotaWindow) string {
	return tokenWindowRedisKeyPrefix + ":" + strconv.Itoa(tokenId) + ":" + w.Name
}

// The open/accumulate decision (and, on restore, the merge decision) requires a
// read-then-write sequence that must be atomic under concurrent charges for the same
// token, so both are Lua scripts rather than plain pipelined commands.
//
//go:embed lua/token_window_add.lua
var tokenWindowAddLuaSrc string

//go:embed lua/token_window_seed.lua
var tokenWindowSeedLuaSrc string

var (
	tokenWindowAddScript  = redis.NewScript(tokenWindowAddLuaSrc)
	tokenWindowSeedScript = redis.NewScript(tokenWindowSeedLuaSrc)
)

func redisAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, amount int64) error {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	now := tokenWindowNow().Unix()
	return tokenWindowAddScript.Run(ctx, common.RDB, []string{key}, now, w.Seconds, amount).Err()
}

// redisGetTokenWindowState reads the raw stored state without judging whether the window is
// still live; ok is false only when the hash (or a field of it) is missing.
func redisGetTokenWindowState(tokenId int, w TokenQuotaWindow) (tokenWindowState, bool, error) {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	values, err := common.RDB.HMGet(ctx, key, "start", "used").Result()
	if err != nil {
		return tokenWindowState{}, false, err
	}
	if len(values) != 2 || values[0] == nil || values[1] == nil {
		return tokenWindowState{}, false, nil
	}
	startStr, ok := values[0].(string)
	if !ok {
		return tokenWindowState{}, false, nil
	}
	usedStr, ok := values[1].(string)
	if !ok {
		return tokenWindowState{}, false, nil
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return tokenWindowState{}, false, nil
	}
	used, err := strconv.ParseInt(usedStr, 10, 64)
	if err != nil {
		return tokenWindowState{}, false, nil
	}
	return tokenWindowState{Start: start, Used: used}, true, nil
}

// in-memory fallback

type tokenWindowMemoryStore struct {
	mutex   sync.Mutex
	windows map[int]map[string]tokenWindowState // tokenId -> windowName -> state
	once    sync.Once
}

var tokenWindowMemory = &tokenWindowMemoryStore{
	windows: make(map[int]map[string]tokenWindowState),
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
	for tokenId, windows := range s.windows {
		for name, state := range windows {
			w, known := tokenQuotaWindowByName(name)
			if !known || !w.isLive(state.Start, now) {
				delete(windows, name)
			}
		}
		if len(windows) == 0 {
			delete(s.windows, tokenId)
		}
	}
}

// getLocked returns tokenId's stored state for w. Caller must hold s.mutex.
func (s *tokenWindowMemoryStore) getLocked(tokenId int, w TokenQuotaWindow) (tokenWindowState, bool) {
	windows, ok := s.windows[tokenId]
	if !ok {
		return tokenWindowState{}, false
	}
	state, ok := windows[w.Name]
	return state, ok
}

// setLocked stores tokenId's state for w. Caller must hold s.mutex.
func (s *tokenWindowMemoryStore) setLocked(tokenId int, w TokenQuotaWindow, state tokenWindowState) {
	windows, ok := s.windows[tokenId]
	if !ok {
		windows = make(map[string]tokenWindowState)
		s.windows[tokenId] = windows
	}
	windows[w.Name] = state
}

func memoryAddTokenWindowUsage(tokenId int, w TokenQuotaWindow, amount int64) {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	now := tokenWindowNow().Unix()
	state, ok := tokenWindowMemory.getLocked(tokenId, w)
	if !ok || !w.isLive(state.Start, now) {
		state = tokenWindowState{Start: now, Used: amount}
	} else {
		state.Used += amount
	}
	tokenWindowMemory.setLocked(tokenId, w, state)
}

func memoryGetTokenWindowState(tokenId int, w TokenQuotaWindow) (tokenWindowState, bool) {
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	return tokenWindowMemory.getLocked(tokenId, w)
}

// persistence: periodic snapshot + restore
//
// Redis/memory above are the only source of truth while the process is up. The functions
// below keep a durable, best-effort copy of each token's current window in
// token_window_buckets purely so a Redis/memory loss (restart, eviction, TTL expiry)
// resumes the same window instead of silently resetting every token's usage to zero.

// TokenWindowBucket is one persisted quota-window snapshot of a token's usage. BucketStart
// holds the window's start time (not a sub-window bucket boundary) so a Redis/memory loss
// can resume the exact same window instead of losing track of when it opened.
type TokenWindowBucket struct {
	TokenId     int    `gorm:"primaryKey;autoIncrement:false"`
	WindowName  string `gorm:"primaryKey;type:varchar(16)"`
	BucketStart int64  `gorm:"primaryKey;autoIncrement:false;index"`
	Used        int64  `gorm:"not null;default:0"`
}

func (TokenWindowBucket) TableName() string {
	return "token_window_buckets"
}

// FlushActiveTokenWindowUsage persists a snapshot of every currently-active token's current
// window state and prunes rows for windows that have since ended. Returns the number of
// tokens whose snapshot was written.
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
		err := DB.Where("window_name = ? AND bucket_start <= ?", w.Name, now-w.Seconds).
			Delete(&TokenWindowBucket{}).Error
		if err != nil {
			common.SysLog("failed to prune expired token window buckets: " + err.Error())
		}
	}
	return flushed, nil
}

// activeRedisTokenWindowIds scans (never KEYS, to stay safe on large keyspaces) for
// token_quota_fixed_window:* keys and returns the distinct token ids with a live window.
func activeRedisTokenWindowIds(ctx context.Context) ([]int, error) {
	seen := make(map[int]struct{})
	var cursor uint64
	for {
		keys, next, err := common.RDB.Scan(ctx, cursor, tokenWindowRedisKeyPrefix+":*", 200).Result()
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
	ids := make([]int, 0, len(tokenWindowMemory.windows))
	for id := range tokenWindowMemory.windows {
		ids = append(ids, id)
	}
	return ids
}

// flushTokenWindowSnapshot syncs tokenId's persisted window rows to its live state: a
// changed or newly-opened window is upserted, and a row belonging to a window that has
// since ended or rolled over to a new start is deleted. Best-effort like
// addTokenWindowUsage: failures are logged, not returned, so one token failing to persist
// never blocks the rest of the flush.
func flushTokenWindowSnapshot(tokenId int) bool {
	now := tokenWindowNow().Unix()
	live := make(map[string]tokenWindowState) // windowName -> state
	for _, w := range tokenQuotaWindows {
		state, ok, err := readTokenWindowState(tokenId, w)
		if err != nil {
			common.SysLog("failed to read token " + w.Name + " window usage during flush: " + err.Error())
			return false
		}
		if ok && state.Used > 0 && w.isLive(state.Start, now) {
			live[w.Name] = state
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
		state, ok := live[row.WindowName]
		if !ok || state.Start != row.BucketStart {
			stale = append(stale, row)
			continue
		}
		if state.Used != row.Used {
			upserts = append(upserts, TokenWindowBucket{TokenId: tokenId, WindowName: row.WindowName, BucketStart: state.Start, Used: state.Used})
		}
		delete(live, row.WindowName)
	}
	for name, state := range live {
		upserts = append(upserts, TokenWindowBucket{TokenId: tokenId, WindowName: name, BucketStart: state.Start, Used: state.Used})
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

// RestoreSkip records why one persisted window was not freshly seeded during a
// RestoreTokenWindowUsageFromDB run, so callers can log the specific token and reason
// instead of only a bare count.
type RestoreSkip struct {
	TokenId     int
	Window      string
	WindowStart int64
	Reason      string
	// Failed is true when a backend error caused the skip (a real problem worth alerting
	// on). It is false when the window was already live (e.g. live traffic or an earlier
	// restore already recorded it), which is an expected, harmless outcome.
	Failed bool
}

// RestoreResult summarizes a RestoreTokenWindowUsageFromDB run.
type RestoreResult struct {
	Candidates int // tokens with at least one persisted window still inside its lifetime
	Restored   int // tokens where at least one window was freshly seeded
	Skips      []RestoreSkip
}

// RestoreTokenWindowUsageFromDB reseeds Redis/memory from the last flushed snapshot with
// every persisted window that has not yet ended. Must run once at startup, before traffic
// flows. A token can still have more than one persisted row per window immediately after
// upgrading from the previous bucketed implementation; those are merged in Go (earliest
// start, summed usage) into a single window before seeding.
func RestoreTokenWindowUsageFromDB() (RestoreResult, error) {
	now := tokenWindowNow().Unix()
	candidates := make(map[int]struct{})
	restored := make(map[int]struct{})
	var result RestoreResult
	for _, w := range tokenQuotaWindows {
		var rows []TokenWindowBucket
		err := DB.Where("window_name = ? AND bucket_start > ? AND used > 0", w.Name, now-w.Seconds).
			Find(&rows).Error
		if err != nil {
			return RestoreResult{}, err
		}

		merged := make(map[int]TokenWindowBucket, len(rows))
		for _, row := range rows {
			candidates[row.TokenId] = struct{}{}
			existing, ok := merged[row.TokenId]
			if !ok {
				merged[row.TokenId] = row
				continue
			}
			if row.BucketStart < existing.BucketStart {
				existing.BucketStart = row.BucketStart
			}
			existing.Used += row.Used
			merged[row.TokenId] = existing
		}

		for tokenId, row := range merged {
			outcome := seedTokenWindowUsage(tokenId, w, row.BucketStart, row.Used)
			if outcome.seeded {
				restored[tokenId] = struct{}{}
				continue
			}
			result.Skips = append(result.Skips, RestoreSkip{
				TokenId:     tokenId,
				Window:      w.Name,
				WindowStart: row.BucketStart,
				Reason:      outcome.reason,
				Failed:      outcome.failed,
			})
		}
	}
	result.Candidates = len(candidates)
	result.Restored = len(restored)
	return result, nil
}

// seedOutcome reports what happened when seeding a single token/window from a DB snapshot,
// distinguishing a harmless skip (a live window already covers it) from a real backend
// failure, so RestoreTokenWindowUsageFromDB can report each one accurately.
type seedOutcome struct {
	seeded bool
	failed bool // only meaningful when seeded is false
	reason string
}

// seedTokenWindowUsage restores one persisted window. When a live window already exists
// and started no later than the persisted one, it is left untouched (it is the
// authoritative, possibly fresher, source). When a live window exists but opened after the
// persisted window's start, meaning the persisted window had not actually ended when data
// was lost, the two are merged instead of one clobbering the other.
func seedTokenWindowUsage(tokenId int, w TokenQuotaWindow, start int64, used int64) seedOutcome {
	if common.RedisEnabled {
		return redisSeedTokenWindowUsage(tokenId, w, start, used)
	}
	return memorySeedTokenWindowUsage(tokenId, w, start, used)
}

func redisSeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, start int64, used int64) seedOutcome {
	ctx := context.Background()
	key := tokenWindowRedisKey(tokenId, w)
	now := tokenWindowNow().Unix()
	res, err := tokenWindowSeedScript.Run(ctx, common.RDB, []string{key}, now, w.Seconds, start, used).Result()
	if err != nil {
		return seedOutcome{failed: true, reason: "redis error: " + err.Error()}
	}
	code, ok := res.(int64)
	if !ok {
		return seedOutcome{failed: true, reason: fmt.Sprintf("unexpected token window seed result: %v", res)}
	}
	switch code {
	case 1:
		return seedOutcome{seeded: true}
	case 2:
		return seedOutcome{reason: "window already live, not overwritten"}
	default:
		return seedOutcome{reason: "persisted window already expired"}
	}
}

func memorySeedTokenWindowUsage(tokenId int, w TokenQuotaWindow, start int64, used int64) seedOutcome {
	tokenWindowMemory.startJanitor()
	tokenWindowMemory.mutex.Lock()
	defer tokenWindowMemory.mutex.Unlock()

	now := tokenWindowNow().Unix()
	if !w.isLive(start, now) {
		return seedOutcome{reason: "persisted window already expired"}
	}

	state, ok := tokenWindowMemory.getLocked(tokenId, w)
	if !ok || !w.isLive(state.Start, now) {
		tokenWindowMemory.setLocked(tokenId, w, tokenWindowState{Start: start, Used: used})
		return seedOutcome{seeded: true}
	}
	if state.Start <= start {
		return seedOutcome{reason: "window already live, not overwritten"}
	}
	tokenWindowMemory.setLocked(tokenId, w, tokenWindowState{Start: start, Used: used + state.Used})
	return seedOutcome{seeded: true}
}
