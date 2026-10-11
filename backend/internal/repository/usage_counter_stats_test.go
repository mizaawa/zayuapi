package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageEndpointChartsSkipLogSummaryScan(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start, end := time.Now().Add(-time.Hour), time.Now()
	mock.ExpectQuery(`SELECT COALESCE\(NULLIF\(TRIM\(inbound_endpoint\), ''\), 'unknown'\) AS endpoint, .* GROUP BY endpoint ORDER BY requests DESC`).
		WithArgs(start, end, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}).
			AddRow("/v1/responses", 3, 150, 2, 1).
			AddRow("unknown", 2, 0, 0, 0))
	stats, err := repo.GetStatsWithFilters(context.Background(), UsageLogFilters{
		UserID: 7, StartTime: &start, EndTime: &end, SkipSummaryStats: true, SkipUpstreamStats: true,
	})
	require.NoError(t, err)
	require.EqualValues(t, 5, stats.TotalRequests)
	require.Len(t, stats.Endpoints, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}
