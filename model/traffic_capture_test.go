package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTrafficCaptureRow(requestId string, createdAt int64, storedSize int64) *TrafficCapture {
	return &TrafficCapture{
		CreatedAt:  createdAt,
		RequestId:  requestId,
		StoredSize: storedSize,
		Payload:    make([]byte, storedSize),
	}
}

func truncateTrafficCaptures(t *testing.T) {
	t.Helper()
	require.NoError(t, LOG_DB.Exec("DELETE FROM traffic_captures").Error)
}

func TestTrafficCaptureInsertAndGetByRequestId(t *testing.T) {
	truncateTrafficCaptures(t)
	payload := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0xff}
	row := &TrafficCapture{
		CreatedAt: 100, RequestId: "req-1", UserId: 9, ModelName: "m", StatusCode: 502,
		Failed: true, NodeName: "node-a", RawSize: 600, StoredSize: int64(len(payload)), Payload: payload,
	}
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{row}))

	got, err := GetTrafficCaptureByRequestId("req-1")
	require.NoError(t, err)
	require.Equal(t, payload, got.Payload)
	require.Equal(t, 9, got.UserId)
	require.True(t, got.Failed)
	require.EqualValues(t, 600, got.RawSize)

	_, err = GetTrafficCaptureByRequestId("missing")
	require.Error(t, err)
}

func TestTrafficCaptureGetByRequestIdReturnsLatestWhenDuplicated(t *testing.T) {
	truncateTrafficCaptures(t)
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{
		{CreatedAt: 1, RequestId: "dup", Payload: []byte("old")},
		{CreatedAt: 2, RequestId: "dup", Payload: []byte("new")},
	}))
	got, err := GetTrafficCaptureByRequestId("dup")
	require.NoError(t, err)
	require.Equal(t, []byte("new"), got.Payload)
}

func TestDeleteTrafficCapturesBeforeOnlyRemovesExpiredRows(t *testing.T) {
	truncateTrafficCaptures(t)
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{
		newTrafficCaptureRow("old-1", 10, 1),
		newTrafficCaptureRow("old-2", 20, 1),
		newTrafficCaptureRow("edge", 30, 1),
		newTrafficCaptureRow("fresh", 40, 1),
	}))

	deleted, err := DeleteTrafficCapturesBefore(30, 500)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.Count)
	require.EqualValues(t, 30, stats.OldestCreatedAt)
}

func TestDeleteTrafficCapturesBeforeSpansMultipleBatches(t *testing.T) {
	truncateTrafficCaptures(t)
	const expired, kept = 23, 5
	rows := make([]*TrafficCapture, 0, expired+kept)
	for i := 0; i < expired; i++ {
		rows = append(rows, newTrafficCaptureRow(fmt.Sprintf("old-%d", i), 10, 1))
	}
	for i := 0; i < kept; i++ {
		rows = append(rows, newTrafficCaptureRow(fmt.Sprintf("new-%d", i), 1000, 1))
	}
	require.NoError(t, InsertTrafficCaptures(rows))

	deleted, err := DeleteTrafficCapturesBefore(500, 10)
	require.NoError(t, err)
	require.EqualValues(t, expired, deleted)

	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.EqualValues(t, kept, stats.Count)
}

func TestDeleteTrafficCapturesBeforeHonorsBatchSizeWithoutOverDeleting(t *testing.T) {
	truncateTrafficCaptures(t)
	rows := make([]*TrafficCapture, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, newTrafficCaptureRow(fmt.Sprintf("r-%d", i), int64(i+1), 1))
	}
	require.NoError(t, InsertTrafficCaptures(rows))

	deleted, err := DeleteTrafficCapturesBefore(6, 2)
	require.NoError(t, err)
	require.EqualValues(t, 5, deleted)

	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.EqualValues(t, 5, stats.Count)
}

func TestDeleteOldestTrafficCapturesUntilBringsTotalUnderCap(t *testing.T) {
	truncateTrafficCaptures(t)
	rows := make([]*TrafficCapture, 0, 6)
	for i := 0; i < 6; i++ {
		rows = append(rows, newTrafficCaptureRow(fmt.Sprintf("r-%d", i), int64(100+i), 100))
	}
	require.NoError(t, InsertTrafficCaptures(rows))

	deleted, err := DeleteOldestTrafficCapturesUntil(250, 2)
	require.NoError(t, err)
	require.EqualValues(t, 4, deleted)

	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.EqualValues(t, 200, stats.StoredBytes)
	require.EqualValues(t, 2, stats.Count)
	require.EqualValues(t, 104, stats.OldestCreatedAt, "the oldest rows must be the ones removed")

	_, err = GetTrafficCaptureByRequestId("r-0")
	require.Error(t, err)
	_, err = GetTrafficCaptureByRequestId("r-5")
	require.NoError(t, err)
}

func TestDeleteOldestTrafficCapturesUntilSpansMoreRowsThanOneBatch(t *testing.T) {
	truncateTrafficCaptures(t)
	const total = 1200
	rows := make([]*TrafficCapture, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, newTrafficCaptureRow(fmt.Sprintf("r-%d", i), int64(i), 10))
	}
	require.NoError(t, InsertTrafficCaptures(rows))

	deleted, err := DeleteOldestTrafficCapturesUntil(1000, 500)
	require.NoError(t, err)
	require.EqualValues(t, 1100, deleted)

	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.EqualValues(t, 100, stats.Count)
	require.EqualValues(t, 1000, stats.StoredBytes)
}

func TestDeleteOldestTrafficCapturesUntilIsNoOpWhenUnderCap(t *testing.T) {
	truncateTrafficCaptures(t)
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{newTrafficCaptureRow("a", 1, 100)}))

	deleted, err := DeleteOldestTrafficCapturesUntil(100, 500)
	require.NoError(t, err)
	require.Zero(t, deleted)
}

func TestDeleteOldestTrafficCapturesUntilCanEmptyTableWhenCapIsZero(t *testing.T) {
	truncateTrafficCaptures(t)
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{
		newTrafficCaptureRow("a", 1, 100), newTrafficCaptureRow("b", 2, 100),
	}))
	deleted, err := DeleteOldestTrafficCapturesUntil(0, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
}

func TestTrafficCaptureStatsOnEmptyTable(t *testing.T) {
	truncateTrafficCaptures(t)
	stats, err := GetTrafficCaptureStats()
	require.NoError(t, err)
	require.Equal(t, TrafficCaptureStats{}, stats)
}

func TestDeleteAllTrafficCaptures(t *testing.T) {
	truncateTrafficCaptures(t)
	require.NoError(t, InsertTrafficCaptures([]*TrafficCapture{
		newTrafficCaptureRow("a", 1, 1), newTrafficCaptureRow("b", 2, 1),
	}))
	deleted, err := DeleteAllTrafficCaptures()
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
}
