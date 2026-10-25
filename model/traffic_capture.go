package model

import (
	"github.com/QuantumNous/new-api/common"
)

// TrafficCapture stores one zstd-compressed trafficcapture.Record per relayed request, for
// troubleshooting. Only the columns needed to find, expire and size-cap a row live outside
// Payload. Payload maps to longblob on MySQL, bytea on PostgreSQL and blob on SQLite.
type TrafficCapture struct {
	Id         int    `json:"id"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	RequestId  string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	UserId     int    `json:"user_id" gorm:"default:0"`
	ModelName  string `json:"model_name" gorm:"type:varchar(255);default:''"`
	StatusCode int    `json:"status_code" gorm:"default:0"`
	Failed     bool   `json:"failed" gorm:"default:false"`
	NodeName   string `json:"node_name" gorm:"type:varchar(64);default:''"`
	RawSize    int64  `json:"raw_size" gorm:"default:0"`
	StoredSize int64  `json:"stored_size" gorm:"default:0;index"`
	Payload    []byte `json:"-"`
}

type TrafficCaptureStats struct {
	Count           int64
	StoredBytes     int64
	RawBytes        int64
	OldestCreatedAt int64
}

func InsertTrafficCaptures(rows []*TrafficCapture) error {
	if len(rows) == 0 {
		return nil
	}
	return LOG_DB.CreateInBatches(rows, 50).Error
}

func GetTrafficCaptureByRequestId(requestId string) (*TrafficCapture, error) {
	var row TrafficCapture
	if err := LOG_DB.Where("request_id = ?", requestId).Order("id desc").First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteTrafficCapturesBefore removes rows older than createdAt, batch rows at a time.
// NOTE: It deliberately avoids Limit().Delete(), which GORM only honors on MySQL; on
// PostgreSQL and SQLite the limit is silently dropped and every matching row is deleted.
func DeleteTrafficCapturesBefore(createdAt int64, batch int) (int64, error) {
	var deleted int64
	for {
		var ids []int
		if err := LOG_DB.Model(&TrafficCapture{}).
			Where("created_at < ?", createdAt).
			Order("id asc").Limit(batch).
			Pluck("id", &ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		result := LOG_DB.Where("id IN ?", ids).Delete(&TrafficCapture{})
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
		if len(ids) < batch {
			return deleted, nil
		}
	}
}

// DeleteOldestTrafficCapturesUntil removes the oldest rows until the summed stored_size is
// at most maxStoredBytes.
func DeleteOldestTrafficCapturesUntil(maxStoredBytes int64, batch int) (int64, error) {
	var total int64
	if err := LOG_DB.Model(&TrafficCapture{}).
		Select("COALESCE(SUM(stored_size), 0)").
		Scan(&total).Error; err != nil {
		return 0, err
	}

	type sizedRow struct {
		Id         int
		StoredSize int64
	}
	var deleted int64
	for total > maxStoredBytes {
		var oldest []sizedRow
		if err := LOG_DB.Model(&TrafficCapture{}).
			Select("id", "stored_size").
			Order("id asc").Limit(batch).
			Find(&oldest).Error; err != nil {
			return deleted, err
		}
		if len(oldest) == 0 {
			return deleted, nil
		}
		ids := make([]int, 0, len(oldest))
		freed := int64(0)
		for _, row := range oldest {
			if total-freed <= maxStoredBytes {
				break
			}
			ids = append(ids, row.Id)
			freed += row.StoredSize
		}
		result := LOG_DB.Where("id IN ?", ids).Delete(&TrafficCapture{})
		if result.Error != nil {
			return deleted, result.Error
		}
		deleted += result.RowsAffected
		total -= freed
	}
	return deleted, nil
}

func GetTrafficCaptureStats() (TrafficCaptureStats, error) {
	var stats TrafficCaptureStats
	err := LOG_DB.Model(&TrafficCapture{}).
		Select("COUNT(*) AS count, COALESCE(SUM(stored_size), 0) AS stored_bytes, " +
			"COALESCE(SUM(raw_size), 0) AS raw_bytes, COALESCE(MIN(created_at), 0) AS oldest_created_at").
		Scan(&stats).Error
	if err != nil {
		common.SysError("failed to query traffic capture stats: " + err.Error())
	}
	return stats, err
}

func DeleteAllTrafficCaptures() (int64, error) {
	result := LOG_DB.Where("id > ?", 0).Delete(&TrafficCapture{})
	return result.RowsAffected, result.Error
}
