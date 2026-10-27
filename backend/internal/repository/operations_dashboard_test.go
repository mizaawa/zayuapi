package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOperationsDashboardDataQueriesAndScans(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	today := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)
	rangeStart := today.AddDate(0, 0, -6)
	trendStart := today.AddDate(0, 0, -14)

	mock.ExpectQuery(`SELECT\s+COALESCE\(\(SELECT SUM\(uc\.actual_cost\).*SUM\(po\.amount\).*SUM\(rc\.value\).*SUM\(uc\.account_cost\).*SUM\(uc\.actual_cost\).*SUM\(f\.amount\).*SUM\(uc\.account_cost\).*SUM\(f\.amount\).*SUM\(uc\.account_cost\)`).
		WithArgs("2026-10-26", today, tomorrow, "2026-10-25", yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"today_user", "recharge", "today_monitor", "total_user", "user_balance", "total_monitor", "admin_balance", "today_upstream", "yesterday_user", "yesterday_recharge", "yesterday_monitor", "yesterday_upstream", "yesterday_balance_change", "yesterday_admin_consumption"}).
			AddRow(8.25, 15.5, 0.75, 100.5, 75.25, 12.5, 200.0, 3.0, 7.0, 10.0, 0.5, 2.0, -4.0, 1.25))
	mock.ExpectQuery(`FROM usage_logs ul JOIN users u.*GROUP BY date ORDER BY date ASC`).
		WithArgs(trendStart, tomorrow, "Asia/Shanghai").
		WillReturnRows(sqlmock.NewRows([]string{"date", "upstream", "user", "profit"}).AddRow("2026-10-26", 3.0, 8.0, 5.0))
	mock.ExpectQuery(`WITH usage_by_group AS .*FROM groups g LEFT JOIN usage_by_group ug.*UNION ALL.*ORDER BY 1 ASC`).
		WithArgs(rangeStart, tomorrow).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "rate", "user", "upstream"}).AddRow(2, "标准", "openai", 1.25, 8.0, 3.0))
	mock.ExpectQuery(`SELECT COALESCE\(NULLIF\(TRIM\(ul\.requested_model\).*GROUP BY 1 ORDER BY 1`).
		WithArgs(rangeStart, tomorrow, int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"model", "user", "upstream", "platforms"}).AddRow("gpt-5", 8.0, 3.0, "anthropic,openai"))

	data, err := repo.GetOperationsDashboardData(context.Background(), today, tomorrow, rangeStart, tomorrow, trendStart, tomorrow, "Asia/Shanghai", 2)
	require.NoError(t, err)
	require.Equal(t, 8.25, data.Metrics.TodayUserConsumption)
	require.Equal(t, 15.5, data.Metrics.TodayUserRecharge)
	require.Equal(t, 0.75, data.Metrics.TodayMonitorConsumption)
	require.Equal(t, 100.5, data.Metrics.TotalUserConsumption)
	require.Equal(t, 75.25, data.Metrics.TotalUserBalance)
	require.Equal(t, 12.5, data.Metrics.TotalMonitorConsumption)
	require.Equal(t, 200.0, data.Metrics.AdminBalance)
	require.Equal(t, map[string]float64{"USD": 4.5}, data.Metrics.TodayRevenue)
	require.Equal(t, 7.0, data.Metrics.YesterdayUserConsumption)
	require.Equal(t, 10.0, data.Metrics.YesterdayUserRecharge)
	require.Equal(t, 0.5, data.Metrics.YesterdayMonitorConsumption)
	require.Equal(t, map[string]float64{"USD": 4.5}, data.Metrics.YesterdayRevenue)
	require.Equal(t, -4.0, *data.Metrics.YesterdayUserBalanceChange)
	require.Equal(t, 1.25, data.Metrics.YesterdayAdminConsumption)
	require.Len(t, data.Trend, 1)
	require.Len(t, data.Groups, 1)
	require.Equal(t, "openai", data.Groups[0].Platform)
	require.Len(t, data.Models, 1)
	require.Equal(t, "gpt-5", data.Models[0].Model)
	require.Equal(t, []string{"anthropic", "openai"}, data.Models[0].Platforms)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationsDashboardUnrecordedBalanceChangeIsNull(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`SELECT NULL`).WillReturnRows(sqlmock.NewRows([]string{"amount"}).AddRow(nil))
	var change *float64
	require.NoError(t, scanSingleRow(context.Background(), db, `SELECT NULL`, nil, &change))
	require.Nil(t, change)
	require.NoError(t, mock.ExpectationsWereMet())
}
