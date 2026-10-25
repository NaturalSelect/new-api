package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/trafficcapture"
	"github.com/QuantumNous/new-api/setting/traffic_capture_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	trafficCaptureQueueSize     = 512
	trafficCaptureWriteBatch    = 50
	trafficCaptureDeleteBatch   = 500
	trafficCaptureCleanupPeriod = 1 * time.Minute
)

var (
	trafficCaptureQueue = make(chan trafficcapture.Record, trafficCaptureQueueSize)
	trafficCaptureOnce  sync.Once

	trafficCaptureDropped      atomic.Int64
	trafficCaptureWriteFailed  atomic.Int64
	trafficCaptureLastCleanup  atomic.Int64
	trafficCaptureLastDeletion atomic.Int64
)

type TrafficCaptureView struct {
	CreatedAt  int64                 `json:"created_at"`
	StoredSize int64                 `json:"stored_size"`
	RawSize    int64                 `json:"raw_size"`
	Record     trafficcapture.Record `json:"record"`
}

type TrafficCaptureStatsView struct {
	Count              int64 `json:"count"`
	StoredBytes        int64 `json:"stored_bytes"`
	RawBytes           int64 `json:"raw_bytes"`
	OldestCreatedAt    int64 `json:"oldest_created_at"`
	RetentionSizeBytes int64 `json:"retention_size_bytes"`
	TtlSeconds         int64 `json:"ttl_seconds"`
	Dropped            int64 `json:"dropped"`
	WriteFailures      int64 `json:"write_failures"`
	QueueLen           int   `json:"queue_len"`
	LastCleanupAt      int64 `json:"last_cleanup_at"`
	LastCleanupDeleted int64 `json:"last_cleanup_deleted"`
}

// EnqueueTrafficCapture never blocks the request path: when the writer falls behind and
// the queue is full, the record is dropped and counted instead.
func EnqueueTrafficCapture(rec trafficcapture.Record) bool {
	select {
	case trafficCaptureQueue <- rec:
		return true
	default:
		trafficCaptureDropped.Add(1)
		return false
	}
}

// StartTrafficCaptureTask starts the DB writer on every node, so each node persists the
// requests it captured. Expiry runs on the master node only, like the other singleton
// background tasks, and re-reads the retention settings on every tick so changes made in
// the admin UI apply without a restart.
func StartTrafficCaptureTask() {
	trafficCaptureOnce.Do(func() {
		ctx := context.Background()
		gopool.Go(runTrafficCaptureWriter)
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(ctx, fmt.Sprintf("traffic capture cleanup task started: tick=%s", trafficCaptureCleanupPeriod))
			ticker := time.NewTicker(trafficCaptureCleanupPeriod)
			defer ticker.Stop()
			for range ticker.C {
				cleanupTrafficCaptures(ctx)
			}
		})
	})
}

func runTrafficCaptureWriter() {
	for first := range trafficCaptureQueue {
		batch := append(make([]trafficcapture.Record, 0, trafficCaptureWriteBatch), first)
	drain:
		for len(batch) < trafficCaptureWriteBatch {
			select {
			case next := <-trafficCaptureQueue:
				batch = append(batch, next)
			default:
				break drain
			}
		}
		writeTrafficCaptureBatch(batch)
	}
}

func writeTrafficCaptureBatch(records []trafficcapture.Record) {
	rows := make([]*model.TrafficCapture, 0, len(records))
	for _, rec := range records {
		payload, rawSize, err := trafficcapture.Encode(rec)
		if err != nil {
			trafficCaptureWriteFailed.Add(1)
			common.SysError("failed to encode traffic capture: " + err.Error())
			continue
		}
		rows = append(rows, &model.TrafficCapture{
			CreatedAt:  time.Now().Unix(),
			RequestId:  rec.RequestId,
			UserId:     rec.UserId,
			ModelName:  rec.ModelName,
			StatusCode: rec.Client.Status,
			Failed:     rec.Failed,
			NodeName:   rec.NodeName,
			RawSize:    rawSize,
			StoredSize: int64(len(payload)),
			Payload:    payload,
		})
	}
	if err := model.InsertTrafficCaptures(rows); err != nil {
		trafficCaptureWriteFailed.Add(int64(len(rows)))
		common.SysError("failed to write traffic captures: " + err.Error())
	}
}

func cleanupTrafficCaptures(ctx context.Context) {
	expired, err := model.DeleteTrafficCapturesBefore(
		time.Now().Add(-traffic_capture_setting.GetTTL()).Unix(), trafficCaptureDeleteBatch)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("traffic capture expiry failed: %v", err))
	}
	overflow, err := model.DeleteOldestTrafficCapturesUntil(
		traffic_capture_setting.GetRetentionSizeBytes(), trafficCaptureDeleteBatch)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("traffic capture size trim failed: %v", err))
	}
	trafficCaptureLastCleanup.Store(time.Now().Unix())
	trafficCaptureLastDeletion.Store(expired + overflow)
}

func GetTrafficCaptureRecord(requestId string) (*TrafficCaptureView, error) {
	row, err := model.GetTrafficCaptureByRequestId(requestId)
	if err != nil {
		return nil, err
	}
	rec, err := trafficcapture.Decode(row.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode traffic capture: %w", err)
	}
	return &TrafficCaptureView{
		CreatedAt:  row.CreatedAt,
		StoredSize: row.StoredSize,
		RawSize:    row.RawSize,
		Record:     rec,
	}, nil
}

func GetTrafficCaptureStats() (*TrafficCaptureStatsView, error) {
	stats, err := model.GetTrafficCaptureStats()
	if err != nil {
		return nil, err
	}
	return &TrafficCaptureStatsView{
		Count:              stats.Count,
		StoredBytes:        stats.StoredBytes,
		RawBytes:           stats.RawBytes,
		OldestCreatedAt:    stats.OldestCreatedAt,
		RetentionSizeBytes: traffic_capture_setting.GetRetentionSizeBytes(),
		TtlSeconds:         int64(traffic_capture_setting.GetTTL().Seconds()),
		Dropped:            trafficCaptureDropped.Load(),
		WriteFailures:      trafficCaptureWriteFailed.Load(),
		QueueLen:           len(trafficCaptureQueue),
		LastCleanupAt:      trafficCaptureLastCleanup.Load(),
		LastCleanupDeleted: trafficCaptureLastDeletion.Load(),
	}, nil
}

func PurgeTrafficCaptures() (int64, error) {
	return model.DeleteAllTrafficCaptures()
}
