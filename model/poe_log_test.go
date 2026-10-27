package model

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedPoeLog(t *testing.T, channelId int, seq int, creationSeconds int64) {
	t.Helper()
	require.NoError(t, DB.Create(&PoeLog{
		ChannelId:    channelId,
		QueryId:      fmt.Sprintf("q-%d-%d", channelId, seq),
		CreationTime: creationSeconds * 1_000_000,
	}).Error)
}

func TestDeleteOldPoeLogs(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&PoeLog{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM poe_logs") })

	t.Run("deletes only rows before the cutoff", func(t *testing.T) {
		DB.Exec("DELETE FROM poe_logs")
		seedPoeLog(t, 1, 1, 100)
		seedPoeLog(t, 1, 2, 200)
		seedPoeLog(t, 1, 3, 300)
		seedPoeLog(t, 1, 4, 400)

		deleted, err := DeleteOldPoeLogs(300)
		require.NoError(t, err)
		assert.EqualValues(t, 2, deleted)

		var remaining int64
		require.NoError(t, DB.Model(&PoeLog{}).Count(&remaining).Error)
		assert.EqualValues(t, 2, remaining)
	})

	t.Run("keeps the newest row of each channel as sync cursor", func(t *testing.T) {
		DB.Exec("DELETE FROM poe_logs")
		seedPoeLog(t, 1, 1, 100)
		seedPoeLog(t, 1, 2, 200)
		seedPoeLog(t, 2, 1, 150)

		deleted, err := DeleteOldPoeLogs(1000)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)

		for channelId, wantQueryId := range map[int]string{1: "q-1-2", 2: "q-2-1"} {
			cursor, err := GetPoeLogLatestQueryId(channelId)
			require.NoError(t, err)
			assert.Equal(t, wantQueryId, cursor)
		}
	})

	t.Run("does nothing when no row is old enough", func(t *testing.T) {
		DB.Exec("DELETE FROM poe_logs")
		seedPoeLog(t, 1, 1, 500)
		seedPoeLog(t, 1, 2, 600)

		deleted, err := DeleteOldPoeLogs(100)
		require.NoError(t, err)
		assert.EqualValues(t, 0, deleted)
	})

	t.Run("does not touch related consume logs", func(t *testing.T) {
		DB.Exec("DELETE FROM poe_logs")
		DB.Exec("DELETE FROM logs")
		consume := &Log{Type: LogTypeConsume, CreatedAt: 100}
		require.NoError(t, DB.Create(consume).Error)
		require.NoError(t, DB.Create(&PoeLog{
			ChannelId: 1, QueryId: "q-linked", CreationTime: 100 * 1_000_000, LogId: consume.Id,
		}).Error)
		seedPoeLog(t, 1, 2, 500)

		deleted, err := DeleteOldPoeLogs(300)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)

		var logCount int64
		require.NoError(t, DB.Model(&Log{}).Where("id = ?", consume.Id).Count(&logCount).Error)
		assert.EqualValues(t, 1, logCount)
	})
}
