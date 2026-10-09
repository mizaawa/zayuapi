package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestUserUsageListFastPaginationSkipsCountWithDateFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 30)
	// An unexpected COUNT would fail this request, reproducing the dependency
	// that used to prevent the user's first page from loading on large histories.
	mock.ExpectQuery(`SELECT .* FROM usage_logs WHERE user_id = \$1 AND created_at >= \$2 AND created_at < \$3 ORDER BY created_at DESC, id DESC LIMIT \$4 OFFSET \$5`).
		WithArgs(int64(7), start, end, 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{
		Page: 1, PageSize: 20, SortBy: "created_at", SortOrder: "desc",
	}, UsageLogFilters{UserID: 7, StartTime: &start, EndTime: &end, SkipTotal: true})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.Zero(t, page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserUsageStatsSkipUnusedUpstreamQueries(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		name := "transaction"
		if pooled {
			name = "pool"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &usageLogRepository{sql: db}
			if pooled {
				repo = newUsageLogRepositoryWithSQL(nil, db)
				mock.MatchExpectationsInOrder(false)
			}
			start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 0, 30)
			mock.ExpectQuery(`SELECT COUNT\(\*\) as total_requests`).
				WithArgs(int64(7), start, end).
				WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "cache", "cache_creation", "cache_read", "cost", "actual", "account", "duration"}).
					AddRow(50, 100, 200, 30, 10, 20, 1, 0.8, 0.2, 100))
			mock.ExpectQuery(`SELECT .*TRIM\(inbound_endpoint\).* GROUP BY endpoint`).
				WithArgs(start, end, int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "tokens", "cost", "actual"}).
					AddRow("/v1/responses", 50, 330, 1, 0.8))
			stats, err := repo.GetStatsWithFilters(context.Background(), UsageLogFilters{
				UserID: 7, StartTime: &start, EndTime: &end, SkipUpstreamStats: true,
			})
			require.NoError(t, err)
			require.EqualValues(t, 50, stats.TotalRequests)
			require.EqualValues(t, 330, stats.TotalTokens)
			require.Equal(t, 0.8, stats.TotalActualCost)
			require.Len(t, stats.Endpoints, 1)
			require.Empty(t, stats.UpstreamEndpoints)
			require.Empty(t, stats.EndpointPaths)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUserUsageStatsDistinguishEndpointFailureFromEmptyHistory(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name := "empty"
		if unavailable {
			name = "unavailable"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &usageLogRepository{sql: db}
			mock.ExpectQuery(`SELECT COUNT\(\*\) as total_requests`).
				WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "cache", "cache_creation", "cache_read", "cost", "actual", "account", "duration"}).
					AddRow(1, 100, 200, 30, 10, 20, 1, 0.8, 0.2, 100))
			endpointQuery := mock.ExpectQuery(`SELECT .*TRIM\(inbound_endpoint\).* GROUP BY endpoint`).
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(7))
			if unavailable {
				endpointQuery.WillReturnError(context.DeadlineExceeded)
			} else {
				endpointQuery.WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "tokens", "cost", "actual"}))
			}
			stats, err := repo.GetStatsWithFilters(context.Background(), UsageLogFilters{UserID: 7, SkipUpstreamStats: true})
			require.NoError(t, err)
			require.EqualValues(t, 1, stats.TotalRequests)
			require.Empty(t, stats.Endpoints)
			require.Equal(t, unavailable, stats.EndpointsUnavailable)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
