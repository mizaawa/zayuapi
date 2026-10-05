//go:build unit

package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const monitorUsagePredicate = "NOT EXISTS (SELECT 1 FROM api_keys monitor_key WHERE monitor_key.id = usage_logs.api_key_id AND monitor_key.purpose = 'channel_monitor')"

func TestChannelMonitorUsageLogs_ListFiltersBeforePagingAndCount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		filters UsageLogFilters
	}{
		{"admin exact", UsageLogFilters{HideChannelMonitorLogs: true, ExactTotal: true}},
		{"admin fast", UsageLogFilters{HideChannelMonitorLogs: true}},
		{"personal", UsageLogFilters{HideChannelMonitorLogs: true, UserID: 7}},
		{"monitor key selected", UsageLogFilters{HideChannelMonitorLogs: true, APIKeyID: 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			if !shouldUseFastUsageLogTotal(tc.filters) {
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE " + regexp.QuoteMeta(monitorUsagePredicate)).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			}
			mock.ExpectQuery("SELECT .* FROM usage_logs WHERE " + regexp.QuoteMeta(monitorUsagePredicate) + ".* ORDER BY .* LIMIT .* OFFSET").
				WillReturnRows(sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")))
			logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 2}, tc.filters)
			require.NoError(t, err)
			require.Empty(t, logs)
			require.Zero(t, page.Total)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelMonitorUsageLogs_StatsIncludeAllEndpointBreakdowns(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta(monitorUsagePredicate)).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "cache", "cache_creation", "cache_read", "cost", "actual_cost", "account_cost", "duration"}).
			AddRow(1, 2, 3, 0, 0, 0, 0.5, 0.5, 0.5, 10))
	for i := 0; i < 3; i++ {
		mock.ExpectQuery(regexp.QuoteMeta(monitorUsagePredicate)+" GROUP BY endpoint").
			WithArgs(start, end).
			WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "tokens", "cost", "actual_cost"}).AddRow("/v1/messages", 1, 5, 0.5, 0.5))
	}
	stats, err := repo.GetStatsWithFilters(context.Background(), UsageLogFilters{HideChannelMonitorLogs: true, StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	for _, rows := range [][]usagestats.EndpointStat{stats.Endpoints, stats.UpstreamEndpoints, stats.EndpointPaths} {
		require.Len(t, rows, 1)
		require.Equal(t, int64(1), rows[0].Requests)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorUsageLogs_DetailAndChartQueries(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	ctx := context.Background()
	filters := UsageLogFilters{HideChannelMonitorLogs: true}
	mock.ExpectQuery("WHERE id = \\$1 AND " + regexp.QuoteMeta(monitorUsagePredicate)).
		WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows(strings.Split(usageLogSelectColumns, ", ")))
	_, err := repo.GetByIDWithFilters(ctx, 42, filters)
	require.ErrorIs(t, err, service.ErrUsageLogNotFound)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mock.ExpectQuery("FROM usage_logs.*"+regexp.QuoteMeta(monitorUsagePredicate)+" GROUP BY date").
		WithArgs(start, end).WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input", "output", "cache_create", "cache_read", "tokens", "cost", "actual"}))
	_, err = repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", filters)
	require.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta(monitorUsagePredicate)+" GROUP BY").
		WithArgs(start, end).WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input", "output", "cache_create", "cache_read", "tokens", "cost", "actual", "account"}))
	_, err = repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta(strings.Replace(monitorUsagePredicate, "usage_logs.api_key_id", "ul.api_key_id", 1))+" GROUP BY").
		WithArgs(start, end).WillReturnRows(sqlmock.NewRows([]string{"group_id", "group_name", "requests", "tokens", "cost", "actual", "account"}))
	_, err = repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
