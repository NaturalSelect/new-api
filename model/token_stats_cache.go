package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// tokenStatsCacheWatermarkOptionKey stores the earliest UTC day (a day-truncated Unix
// timestamp, see BucketTimestampToDay) that TokenStatsCache fully covers. Days before
// the watermark have not been backfilled yet and must be served from a raw logs scan;
// days in [watermark, today) are safe to read from the cache; today itself is always
// scanned live since its day bucket is still being written to.
const tokenStatsCacheWatermarkOptionKey = "TokenStatsCacheWatermark"

// TokenStatsCache is a day-bucketed pre-aggregation of consume logs. It exists purely
// to speed up GetTokenDistribution/GetKeyDistribution (and their Self variants) on
// deployments with a large logs table; it holds no information that cannot be
// recomputed from logs.
//
// The row grain (day + user_id + token_id + token_name + model_name) reconstructs
// GetKeyDistribution's output shape exactly: it groups by (token_id, token_name,
// model_name) across the whole range, which has no time dimension, so day-bucketed
// storage loses no precision there. GetTokenDistribution groups by (time bucket,
// model_name) where the raw scan buckets by hour; a cache-covered historical range is
// therefore only reconstructed at day granularity, not hour — an intentional trade-off
// (see queryTokenStatsCacheForTokenDistribution) that is invisible in day/week views
// but collapses a cached day's hourly points into a single point in hour views.
// token_name is part of the unique key (never collapsed to "latest name") so a renamed
// key spanning the watermark still produces the same split-by-name rows the raw scan
// has always produced. username is deliberately NOT part of the unique key; see the
// field comment below.
//
// Lives on LOG_DB (not the default DB): it is derived solely from the logs table, and
// every existing token/key distribution scan already reads from LOG_DB — including the
// LOG_SQL_DSN deployment where logs live in a database separate from the main one.
type TokenStatsCache struct {
	Id     int   `json:"id" gorm:"primaryKey"`
	Day    int64 `json:"day" gorm:"bigint;uniqueIndex:idx_tsc_unique,priority:1;index:idx_tsc_day"`
	UserId int   `json:"user_id" gorm:"uniqueIndex:idx_tsc_unique,priority:2;index:idx_tsc_user_id"`
	// Username is a denormalized, last-write-wins snapshot used only for filtering
	// (GetTokenDistribution's username filter / keyDistributionFilter.Username). It is
	// intentionally excluded from the unique key: unlike token_name, it is never an
	// output grouping dimension for either endpoint, only a WHERE-filter value, so
	// collapsing a same-day rename into "whichever write happened last" cannot change
	// a result's shape — at most it can miss this row for a same-day filter query
	// during the rare moment a user renames their account, a negligible and
	// self-correcting effect not worth widening a unique index that every billing
	// request writes through.
	Username         string `json:"username" gorm:"size:32;default:''"`
	TokenId          int    `json:"token_id" gorm:"uniqueIndex:idx_tsc_unique,priority:3"`
	TokenName        string `json:"token_name" gorm:"size:100;uniqueIndex:idx_tsc_unique,priority:4;default:''"`
	ModelName        string `json:"model_name" gorm:"size:100;uniqueIndex:idx_tsc_unique,priority:5;default:''"`
	InputTokens      int64  `json:"input_tokens" gorm:"default:0"`
	OutputTokens     int64  `json:"output_tokens" gorm:"default:0"`
	CacheReadTokens  int64  `json:"cache_read_tokens" gorm:"default:0"`
	CacheWriteTokens int64  `json:"cache_write_tokens" gorm:"default:0"`
	Count            int64  `json:"count" gorm:"default:0"`
}

func (TokenStatsCache) TableName() string {
	return "token_stats_cache"
}

// BucketTimestampToDay truncates a Unix timestamp to the start of its UTC calendar
// day. Used as the cache table's row grain and as the watermark/"today" boundary so
// backfill, live upserts, and reads all agree on the same day boundaries.
func BucketTimestampToDay(timestamp int64) int64 {
	if timestamp <= 0 {
		return 0
	}
	return timestamp - (timestamp % 86400)
}

// GetTokenStatsCacheWatermark returns the earliest day TokenStatsCache fully covers.
// It reads from common.OptionMap (synced from the Option table every
// common.SyncFrequency seconds — see SyncOptions), avoiding a DB round trip on the hot
// read path. A missing/unparseable value defaults to "today": on a fresh deployment,
// or on a follower node that has not synced the latest value yet, that correctly means
// nothing is cache-covered, so every read falls back to the pre-existing raw logs scan
// and no data is ever missed — only the speed-up is delayed.
func GetTokenStatsCacheWatermark() int64 {
	today := BucketTimestampToDay(common.GetTimestamp())
	common.OptionMapRWMutex.RLock()
	value, ok := common.OptionMap[tokenStatsCacheWatermarkOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if !ok || value == "" {
		return today
	}
	day, err := strconv.ParseInt(value, 10, 64)
	if err != nil || day > today {
		return today
	}
	return day
}

// AdvanceTokenStatsCacheWatermark persists a new watermark day. Callers (the backfill
// task) must only ever move it backwards in time (further into the past), and only
// after that day's cache rows have been durably written.
func AdvanceTokenStatsCacheWatermark(day int64) error {
	return UpdateOption(tokenStatsCacheWatermarkOptionKey, strconv.FormatInt(day, 10))
}

// tokenStatsCacheDelta is one row's worth of change to apply to TokenStatsCache.
type tokenStatsCacheDelta struct {
	Day              int64
	UserId           int
	Username         string
	TokenId          int
	TokenName        string
	ModelName        string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Count            int64
}

// upsertTokenStatsCacheIncrement atomically adds delta's metrics onto the matching
// TokenStatsCache row (creating it if absent). This is the only write primitive the
// cache table uses — both the live billing path (one log row at a time, see
// upsertTokenStatsCacheForLog) and backfill (one freshly-scanned day at a time, see
// BackfillTokenStatsCacheDay) call it, so a backfilled day and a same-day live
// increment landing concurrently (e.g. a backdated Poe log sync) always compose by
// addition instead of one silently overwriting the other.
//
// Known narrow gap: a day is only ever backfilled once (the watermark never revisits
// a day), so this only matters for a backdated write landing on the exact day currently
// being backfilled. If that log row is committed before BackfillTokenStatsCacheDay's
// raw scan runs, the scan already counts it; if the caller's own upsertTokenStatsCacheForLog
// call for that same row is then delayed (e.g. goroutine preemption) past the backfill
// transaction's commit, it will add that row's delta a second time. This requires an
// unlucky ordering between two independently-timed writers and is not covered by the
// current tests (see TestBackfillTokenStatsCacheDay_ComposesWithConcurrentLiveWrite,
// which only exercises a live write landing after backfill has already returned).
func upsertTokenStatsCacheIncrement(delta tokenStatsCacheDelta) error {
	if delta.Count == 0 && delta.InputTokens == 0 && delta.OutputTokens == 0 &&
		delta.CacheReadTokens == 0 && delta.CacheWriteTokens == 0 {
		return nil
	}
	row := &TokenStatsCache{
		Day:              delta.Day,
		UserId:           delta.UserId,
		Username:         delta.Username,
		TokenId:          delta.TokenId,
		TokenName:        delta.TokenName,
		ModelName:        delta.ModelName,
		InputTokens:      delta.InputTokens,
		OutputTokens:     delta.OutputTokens,
		CacheReadTokens:  delta.CacheReadTokens,
		CacheWriteTokens: delta.CacheWriteTokens,
		Count:            delta.Count,
	}
	return LOG_DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "day"}, {Name: "user_id"}, {Name: "token_id"}, {Name: "token_name"}, {Name: "model_name"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"username":           delta.Username,
			"input_tokens":       gorm.Expr("token_stats_cache.input_tokens + ?", delta.InputTokens),
			"output_tokens":      gorm.Expr("token_stats_cache.output_tokens + ?", delta.OutputTokens),
			"cache_read_tokens":  gorm.Expr("token_stats_cache.cache_read_tokens + ?", delta.CacheReadTokens),
			"cache_write_tokens": gorm.Expr("token_stats_cache.cache_write_tokens + ?", delta.CacheWriteTokens),
			"count":              gorm.Expr("token_stats_cache.count + ?", delta.Count),
		}),
	}).Create(row).Error
}

// upsertTokenStatsCacheForLog derives one tokenStatsCacheDelta from a just-persisted
// Log row and applies it as an increment. Call this immediately after any LOG_DB.Create
// of a Log succeeds — RecordConsumeLog, RecordTaskBillingLog, and RecordPoeConsumeLog
// all do — so the cache always reflects every source of consume-log data, not just the
// main text relay billing path: task billing (e.g. Midjourney/Suno differential
// settlement) and Poe log sync both write LogTypeConsume rows the raw scan already
// includes, and would otherwise be silently undercounted once their day falls under the
// cache. Errors are logged and swallowed: a cache write must never fail the request or
// the log write it rides along with.
func upsertTokenStatsCacheForLog(log *Log) {
	if log == nil || log.Type != LogTypeConsume {
		return
	}
	delta := tokenStatsCacheDelta{
		Day:          BucketTimestampToDay(log.CreatedAt),
		UserId:       log.UserId,
		Username:     log.Username,
		TokenId:      log.TokenId,
		TokenName:    log.TokenName,
		ModelName:    log.ModelName,
		InputTokens:  int64(log.PromptTokens),
		OutputTokens: int64(log.CompletionTokens),
		Count:        1,
	}
	cacheReadTokens, cacheWriteTokens, isAnthropic := tokenStatsCacheOtherTokens(log.Other)
	// NOTE: see isAnthropicUsageSemantic in log.go — Claude/Anthropic semantic
	// prompt_tokens is text-only, so cache_read must be added back into InputTokens
	// here too, mirroring the scan-path normalization exactly.
	if isAnthropic {
		delta.InputTokens += cacheReadTokens
	}
	delta.CacheReadTokens = cacheReadTokens
	delta.CacheWriteTokens = cacheWriteTokens
	if err := upsertTokenStatsCacheIncrement(delta); err != nil {
		common.SysLog("failed to upsert token stats cache: " + err.Error())
	}
}

// BackfillTokenStatsCacheDay (re)computes TokenStatsCache for one UTC day from a full
// raw scan of logs, then applies the result as a set of increments (see
// upsertTokenStatsCacheIncrement). day must already be day-truncated (see
// BucketTimestampToDay). It first deletes any existing rows for the day so a retry
// (after a crash, or before the watermark has advanced) recomputes the same
// authoritative total rather than adding onto a previous attempt — the delete makes
// re-running idempotent; the increment-based re-insert is what lets a concurrent live
// write for this same day (see upsertTokenStatsCacheForLog) compose by addition rather
// than being clobbered by the delete. See upsertTokenStatsCacheIncrement's doc comment
// for the narrow ordering gap this does not close. It returns the number of raw log
// rows scanned, for the caller to log backfill progress.
func BackfillTokenStatsCacheDay(day int64) (int64, error) {
	deltas, scanned, err := scanLogsForStatsCacheDay(day)
	if err != nil {
		return 0, err
	}
	err = LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("day = ?", day).Delete(&TokenStatsCache{}).Error; err != nil {
			return err
		}
		for _, delta := range deltas {
			row := &TokenStatsCache{
				Day:              delta.Day,
				UserId:           delta.UserId,
				Username:         delta.Username,
				TokenId:          delta.TokenId,
				TokenName:        delta.TokenName,
				ModelName:        delta.ModelName,
				InputTokens:      delta.InputTokens,
				OutputTokens:     delta.OutputTokens,
				CacheReadTokens:  delta.CacheReadTokens,
				CacheWriteTokens: delta.CacheWriteTokens,
				Count:            delta.Count,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "day"}, {Name: "user_id"}, {Name: "token_id"}, {Name: "token_name"}, {Name: "model_name"},
				},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"username":           delta.Username,
					"input_tokens":       gorm.Expr("token_stats_cache.input_tokens + ?", delta.InputTokens),
					"output_tokens":      gorm.Expr("token_stats_cache.output_tokens + ?", delta.OutputTokens),
					"cache_read_tokens":  gorm.Expr("token_stats_cache.cache_read_tokens + ?", delta.CacheReadTokens),
					"cache_write_tokens": gorm.Expr("token_stats_cache.cache_write_tokens + ?", delta.CacheWriteTokens),
					"count":              gorm.Expr("token_stats_cache.count + ?", delta.Count),
				}),
			}).Create(row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return scanned, nil
}

// ceilToDay rounds a Unix timestamp up to the start of its UTC calendar day if it is
// not already day-aligned. Used to make sure a query's cache zone never claims a day
// that the caller only partially requested (see tokenStatsCacheZonesFor) — the cache
// stores one row per whole day, so it can only ever answer for whole days.
func ceilToDay(timestamp int64) int64 {
	day := BucketTimestampToDay(timestamp)
	if day == timestamp {
		return day
	}
	return day + 86400
}

// tokenStatsCacheZones splits a query's [startTimestamp, endTimestamp] (0 = unbounded
// on either side, matching GetTokenDistribution/GetKeyDistribution's existing
// inclusive >=/<= filter semantics) into up to three sub-ranges: Before and After are
// answered by the original raw logs scan, Cache is answered by TokenStatsCache.
//
// Cache only ever claims whole UTC days that are BOTH watermark-covered AND fully
// contained in the request — a request whose start or end falls mid-day on its
// boundary day always pushes that one partial day to a raw scan instead, so a
// day-granularity cache row is never asked to answer a sub-day question. Before/After
// then absorb whatever Cache does not claim, so the three segments always partition
// the full request with no gap and no overlap. Each zone's *Ok is false when it is
// empty, in which case the caller must skip it entirely.
type tokenStatsCacheZones struct {
	BeforeStart, BeforeEnd int64
	BeforeOk               bool
	// CacheFirstDay/CacheLastDay are both day-aligned and inclusive: the cache covers
	// every day in [CacheFirstDay, CacheLastDay], each a whole 86400s bucket.
	CacheFirstDay, CacheLastDay int64
	CacheOk                     bool
	AfterStart, AfterEnd        int64
	AfterOk                     bool
}

func tokenStatsCacheZonesFor(startTimestamp, endTimestamp int64) tokenStatsCacheZones {
	watermark := GetTokenStatsCacheWatermark()
	today := BucketTimestampToDay(common.GetTimestamp())
	if watermark > today {
		watermark = today
	}

	cacheFirstDay := watermark
	if startTimestamp > cacheFirstDay {
		cacheFirstDay = ceilToDay(startTimestamp)
	}
	// today is never cache-covered (its bucket is still being written to), so the
	// exclusive upper bound below can be at most today.
	cacheLastDayExclusive := today
	if endTimestamp != 0 {
		endDay := BucketTimestampToDay(endTimestamp)
		if endTimestamp >= endDay+86399 {
			endDay += 86400 // the end-of-range day is fully requested; include it
		}
		if endDay < cacheLastDayExclusive {
			cacheLastDayExclusive = endDay
		}
	}

	var z tokenStatsCacheZones
	z.BeforeStart, z.BeforeEnd = startTimestamp, endTimestamp
	z.BeforeOk = endTimestamp == 0 || startTimestamp <= endTimestamp
	z.CacheOk = cacheFirstDay < cacheLastDayExclusive
	if z.CacheOk {
		z.CacheFirstDay = cacheFirstDay
		z.CacheLastDay = cacheLastDayExclusive - 86400
		z.BeforeEnd = z.CacheFirstDay - 1
		z.BeforeOk = z.BeforeStart <= z.BeforeEnd
		z.AfterStart = cacheLastDayExclusive
		z.AfterEnd = endTimestamp
		z.AfterOk = endTimestamp == 0 || z.AfterStart <= endTimestamp
	}
	return z
}

// tokenStatsCacheDayRow is one SUM-aggregated row read back out of TokenStatsCache,
// shared by both cache queries below since they select the same set of metric sums —
// they differ only in which dimension columns they group/select by.
type tokenStatsCacheDayRow struct {
	Day              int64
	UserId           int
	TokenId          int
	TokenName        string
	ModelName        string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Count            int64
}

// queryTokenStatsCacheForTokenDistribution reads the cache-covered portion of Token
// Distribution: SUM the metrics across every user/token for each (day, model_name),
// matching scanTokenDistribution's (hour, model_name) output grain except bucketed by
// day instead of hour — see the TokenStatsCache doc comment for why day granularity is
// an intentional, documented trade-off rather than an hourly bucket.
func queryTokenStatsCacheForTokenDistribution(firstDay, lastDay int64, username string) ([]*TokenDistributionData, error) {
	tx := LOG_DB.Table("token_stats_cache").
		Select("day, model_name, SUM(input_tokens) as input_tokens, SUM(output_tokens) as output_tokens, SUM(cache_read_tokens) as cache_read_tokens, SUM(cache_write_tokens) as cache_write_tokens, SUM(count) as count").
		Where("day >= ? AND day <= ?", firstDay, lastDay).
		Group("day, model_name")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	var rows []tokenStatsCacheDayRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*TokenDistributionData, 0, len(rows))
	for _, r := range rows {
		modelName := r.ModelName
		if modelName == "" {
			modelName = "Unknown"
		}
		result = append(result, &TokenDistributionData{
			CreatedAt:        r.Day,
			ModelName:        modelName,
			InputTokens:      int(r.InputTokens),
			OutputTokens:     int(r.OutputTokens),
			CacheReadTokens:  int(r.CacheReadTokens),
			CacheWriteTokens: int(r.CacheWriteTokens),
			Count:            int(r.Count),
		})
	}
	return result, nil
}

// queryTokenStatsCacheForKeyDistribution reads the cache-covered portion of Key
// Distribution: SUM the metrics across every day/user for each (token_id, token_name,
// model_name), matching scanKeyDistribution's output grain exactly (no day dimension
// in the output, so unlike Token Distribution there is no granularity trade-off here).
func queryTokenStatsCacheForKeyDistribution(firstDay, lastDay int64, filter keyDistributionFilter) ([]*KeyDistributionData, error) {
	tx := LOG_DB.Table("token_stats_cache").
		Select("token_id, token_name, model_name, SUM(input_tokens) as input_tokens, SUM(output_tokens) as output_tokens, SUM(cache_read_tokens) as cache_read_tokens, SUM(cache_write_tokens) as cache_write_tokens, SUM(count) as count").
		Where("day >= ? AND day <= ?", firstDay, lastDay).
		Group("token_id, token_name, model_name")
	if filter.UserId != 0 {
		tx = tx.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		tx = tx.Where("username = ?", filter.Username)
	}
	var rows []tokenStatsCacheDayRow
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*KeyDistributionData, 0, len(rows))
	for _, r := range rows {
		item := &KeyDistributionData{
			TokenId:          r.TokenId,
			TokenName:        r.TokenName,
			ModelName:        r.ModelName,
			InputTokens:      int(r.InputTokens),
			OutputTokens:     int(r.OutputTokens),
			CacheReadTokens:  int(r.CacheReadTokens),
			CacheWriteTokens: int(r.CacheWriteTokens),
			Count:            int(r.Count),
		}
		item.TotalTokens = item.InputTokens + item.OutputTokens + item.CacheWriteTokens
		result = append(result, item)
	}
	return result, nil
}
