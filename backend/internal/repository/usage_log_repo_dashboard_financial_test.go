package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDashboardStats_FinancialTotals(t *testing.T) {
	for _, aggregated := range []bool{true, false} {
		name := "usage_logs"
		if aggregated {
			name = "aggregated"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := newUsageLogRepositoryWithSQL(nil, db)

			mock.ExpectQuery(`SELECT COUNT\(\*\) as total_users, .* COALESCE\(SUM\(balance\), 0\) as total_balance FROM users WHERE deleted_at IS NULL`).
				WithArgs(sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"total_users", "today_new_users", "total_balance"}).AddRow(3, 1, 123.45))
			mock.ExpectQuery(`SELECT COALESCE\(SUM\(value\), 0\) FROM redeem_codes WHERE status = \$1 AND used_by IS NOT NULL AND type IN \(\$2, \$3\) AND value > 0`).
				WithArgs(service.StatusUsed, service.RedeemTypeBalance, service.AdjustmentTypeAdminBalance).
				WillReturnRows(sqlmock.NewRows([]string{"total_recharged"}).AddRow(456.78))
			mock.ExpectQuery(`SELECT .* FROM api_keys WHERE deleted_at IS NULL AND purpose = ''`).
				WithArgs(service.StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"total_api_keys", "active_api_keys"}).AddRow(2, 2))
			mock.ExpectQuery(`SELECT .* FROM accounts WHERE deleted_at IS NULL`).
				WithArgs(service.StatusActive, service.StatusError, sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"total_accounts", "normal_accounts", "error_accounts", "ratelimit_accounts", "overload_accounts"}).AddRow(1, 1, 0, 0, 0))

			totalColumns := []string{"total_requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_cost", "actual_cost", "account_cost", "total_duration_ms"}
			todayColumns := []string{"today_requests", "today_input_tokens", "today_output_tokens", "today_cache_creation_tokens", "today_cache_read_tokens", "today_cost", "today_actual_cost", "today_account_cost"}
			if aggregated {
				mock.ExpectQuery(`SELECT .* FROM usage_dashboard_daily$`).
					WillReturnRows(sqlmock.NewRows(totalColumns).AddRow(10, 20, 30, 0, 0, 50, 25, 15, 100))
				mock.ExpectQuery(`SELECT .* FROM usage_dashboard_daily WHERE bucket_date = \$1::date`).
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows(append(todayColumns, "active_users")).AddRow(1, 2, 3, 0, 0, 5, 2.5, 1.5, 1))
				mock.ExpectQuery(`SELECT active_users FROM usage_dashboard_hourly WHERE bucket_start = \$1`).
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"active_users"}).AddRow(1))
			} else {
				mock.ExpectQuery(`WITH scoped AS .* FROM usage_logs .* FROM scoped`).
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows(append(totalColumns, todayColumns...)).AddRow(1, 2, 3, 0, 0, 5, 2.5, 1.5, 100, 1, 2, 3, 0, 0, 5, 2.5, 1.5))
				mock.ExpectQuery(`WITH scoped AS .* SELECT user_id, created_at FROM usage_logs .* FROM scoped`).
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"active_users", "hourly_active_users"}).AddRow(1, 1))
				mock.ExpectQuery(`^SELECT COALESCE\(SUM\(actual_cost\), 0\) FROM usage_logs$`).
					WillReturnRows(sqlmock.NewRows([]string{"actual_cost"}).AddRow(25))
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) as request_count, .* FROM usage_logs WHERE created_at >= \$1`).
				WithArgs(sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count"}).AddRow(0, 0))

			var stats *DashboardStats
			if aggregated {
				stats, err = repo.GetDashboardStats(context.Background())
			} else {
				end := time.Now()
				stats, err = repo.GetDashboardStatsWithRange(context.Background(), end.Add(-24*time.Hour), end)
			}
			require.NoError(t, err)
			require.Equal(t, 123.45, stats.TotalBalance)
			require.Equal(t, 456.78, stats.TotalRecharged)
			require.Equal(t, 25.0, stats.TotalConsumption)
			require.Equal(t, 2.5, stats.TodayActualCost)
			if !aggregated {
				require.Equal(t, 2.5, stats.TotalActualCost, "existing token statistics retain the fallback range")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
