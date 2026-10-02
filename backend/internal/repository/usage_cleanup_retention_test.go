package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageCleanupRepositoryStorageStatsIncludesPartitionLeaves(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageCleanupRepository{sql: db}

	// The parent has no heap when partitioned; statistics must traverse its leaves.
	mock.ExpectQuery(`WITH RECURSIVE relations\(relid\) AS .*JOIN relations parent ON i.inhparent = parent.relid.*WHERE NOT EXISTS .*SUM\(pg_table_size\(relid::regclass\)\).*SUM\(pg_indexes_size\(relid::regclass\)\).*SUM\(pg_total_relation_size\(relid::regclass\)\).*FROM leaves`).
		WillReturnRows(sqlmock.NewRows([]string{"table_bytes", "index_bytes", "total_bytes"}).AddRow(int64(100), int64(40), int64(160)))

	before := time.Now().UTC()
	stats, err := repo.GetUsageLogsStorageStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(100), stats.TableBytes)
	require.Equal(t, int64(40), stats.IndexBytes)
	require.Equal(t, int64(160), stats.TotalBytes)
	require.False(t, stats.MeasuredAt.Before(before))
	require.False(t, stats.MeasuredAt.After(time.Now().UTC()))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryDeleteBeforeUsesStrictCutoffAndBatchLimit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageCleanupRepository{sql: db}
	cutoff := time.Date(2026, 7, 15, 8, 30, 0, 0, time.FixedZone("CST", 8*60*60))

	mock.ExpectQuery(`WITH target AS .*SELECT tableoid, ctid.*WHERE created_at < \$1.*LIMIT \$2.*DELETE FROM usage_logs.*WHERE usage_logs.tableoid = target.tableoid AND usage_logs.ctid = target.ctid`).
		WithArgs(cutoff.UTC(), 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	deleted, err := repo.DeleteUsageLogsBeforeBatch(context.Background(), cutoff, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageCleanupRepositoryMaxIDBoundary(t *testing.T) {
	for _, maxID := range []int64{0, 42} {
		t.Run(time.Duration(maxID).String(), func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageCleanupRepository{sql: db}
			mock.ExpectQuery(`SELECT COALESCE\(MAX\(id\), 0\) FROM usage_logs`).
				WillReturnRows(sqlmock.NewRows([]string{"max_id"}).AddRow(maxID))

			got, err := repo.GetUsageLogsMaxID(context.Background())
			require.NoError(t, err)
			require.Equal(t, maxID, got)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageCleanupRepositoryDeleteAllUsesCapturedIDAndBatchLimit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageCleanupRepository{sql: db}
	mock.ExpectQuery(`WITH target AS .*SELECT tableoid, ctid.*WHERE id <= \$1.*ORDER BY id ASC.*LIMIT \$2.*DELETE FROM usage_logs.*WHERE usage_logs.tableoid = target.tableoid AND usage_logs.ctid = target.ctid`).
		WithArgs(int64(42), 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	deleted, err := repo.DeleteUsageLogsThroughIDBatch(context.Background(), 42, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}
