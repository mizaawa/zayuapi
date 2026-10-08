package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func userDashboardRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"platform", "requests", "input", "output", "write", "read", "cost", "actual", "duration", "duration_count", "today_requests", "today_input", "today_output", "today_write", "today_read", "today_cost", "today_actual", "recent_requests", "recent_tokens", "platform_requests", "platform_tokens", "platform_cost", "platform_today_requests", "platform_today_tokens", "platform_today_cost"}).
		AddRow("openai", 10, 100, 200, 30, 40, 10, 8, 1000, 2, 5, 50, 100, 15, 20, 5, 4, 5, 150, 9, 370, 8, 4, 185, 4).
		AddRow("", 2, 1, 2, 3, 4, 2, 1, 100, 1, 1, 1, 2, 3, 4, 1, 0.5, 0, 0, 1, 10, 1, 1, 10, 0.5)
}

func TestUserDashboardQueryPreservesTotalsAndBoundsToday(t *testing.T) {
	for _, totals := range []bool{false, true} {
		t.Run(map[bool]string{false: "today", true: "history"}[totals], func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if strings.Contains(actual, "WITH route_usage") {
					require.Equal(t, !totals, strings.Contains(actual, "ul.created_at >= LEAST"))
					require.Contains(t, actual, "ul.user_id = $1")
					require.Contains(t, actual, "monitor_key.purpose = 'channel_monitor'")
					require.Contains(t, actual, "FROM route_usage ul")
					require.Contains(t, actual, "COUNT(duration_ms)")
					require.Contains(t, actual, "g.platform = 'composite'")
				}
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})))
			require.NoError(t, err)
			defer db.Close()
			repo := newUsageLogRepositoryWithSQL(nil, db)
			mock.ExpectQuery(`SELECT COUNT\(\*\), COUNT\(\*\) FILTER`).WithArgs(int64(7), service.StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"total", "active"}).AddRow(3, 2))
			mock.ExpectQuery(`WITH route_usage AS MATERIALIZED`).WithArgs(int64(7), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(userDashboardRows())
			stats, err := repo.GetUserDashboardStatsWithOptions(context.Background(), 7, totals, true)
			require.NoError(t, err)
			require.EqualValues(t, 3, stats.TotalAPIKeys)
			require.EqualValues(t, 2, stats.ActiveAPIKeys)
			require.EqualValues(t, 6, stats.TodayRequests)
			require.EqualValues(t, 195, stats.TodayTokens)
			require.Equal(t, 6.0, stats.TodayCost)
			require.Equal(t, 4.5, stats.TodayActualCost)
			require.EqualValues(t, 1, stats.Rpm)
			require.EqualValues(t, 30, stats.Tpm)
			require.Len(t, stats.ByPlatform, 1)
			if totals {
				require.EqualValues(t, 12, stats.TotalRequests)
				require.EqualValues(t, 380, stats.TotalTokens)
				require.Equal(t, 12.0, stats.TotalCost)
				require.Equal(t, 9.0, stats.TotalActualCost)
				require.InDelta(t, 1100.0/3, stats.AverageDurationMs, 0.001)
				require.EqualValues(t, 9, stats.ByPlatform[0].TotalRequests)
			} else {
				require.True(t, stats.TotalsPending)
				require.Zero(t, stats.TotalRequests)
				require.Zero(t, stats.TotalCost)
				require.Zero(t, stats.ByPlatform[0].TotalRequests)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUserDashboardRecentDoesNotCountHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := newUsageLogRepositoryWithSQL(nil, db)
	params := pagination.PaginationParams{Page: 1, PageSize: 5, SortBy: "created_at", SortOrder: "desc"}
	query := "SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3"
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(int64(7), 6, 0).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	logs, _, err := repo.ListWithFilters(context.Background(), params, UsageLogFilters{UserID: 7, SkipTotal: true})
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NoError(t, mock.ExpectationsWereMet())
}
