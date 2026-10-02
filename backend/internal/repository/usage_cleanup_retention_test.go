package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageCleanupRepositoryDatabaseStorageStatsIncludesEveryUserTable(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageCleanupRepository{sql: db}

	mock.ExpectQuery(`WITH RECURSIVE.*current_database\(\).*pg_database_size\(current_database\(\)\).*FROM pg_class c.*c\.relkind IN \('r', 'm', 'p'\).*NOT c\.relispartition.*relation_tree\(root_oid, relid\) AS.*JOIN pg_inherits.*child\.relispartition.*pg_table_size\(physical_table\.oid\).*pg_indexes_size\(physical_table\.oid\).*pg_total_relation_size\(physical_table\.oid\).*physical_table\.relkind IN \('r', 'm'\).*LEFT JOIN table_sizes`).
		WillReturnRows(sqlmock.NewRows([]string{"database_name", "database_bytes", "table_name", "table_bytes", "index_bytes", "total_bytes"}).
			AddRow("app", int64(300), "public.usage_logs", int64(100), int64(40), int64(140)).
			AddRow("app", int64(300), "public.users", int64(20), int64(10), int64(30)))

	before := time.Now().UTC()
	stats, err := repo.GetDatabaseStorageStats(context.Background())
	require.NoError(t, err)
	require.Equal(t, "app", stats.DatabaseName)
	require.Equal(t, int64(300), stats.DatabaseBytes)
	require.Equal(t, int64(120), stats.TableBytes)
	require.Equal(t, int64(50), stats.IndexBytes)
	require.Equal(t, int64(170), stats.TotalBytes)
	require.Equal(t, []string{"public.usage_logs", "public.users"}, []string{stats.Tables[0].TableName, stats.Tables[1].TableName})
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
