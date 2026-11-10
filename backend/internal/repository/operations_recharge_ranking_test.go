package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOperationsRechargeRankingQueriesAndScans(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)

	mock.ExpectQuery(`WITH recharges AS \( SELECT po\.user_id, po\.amount::double precision AS amount FROM payment_orders po WHERE po\.order_type = 'balance' AND po\.status IN \('PAID', 'RECHARGING', 'COMPLETED'\) AND po\.paid_at >= \$1 AND po\.paid_at < \$2 UNION ALL .*FROM redeem_codes rc .*rc\.type IN \('balance', 'admin_balance'\) .*rc\.code NOT LIKE 'PAY-%' AND rc\.used_at >= \$1 AND rc\.used_at < \$2 .*WHERE u\.role = 'user' .*SUM\(amount\) OVER \(\) AS total_amount, COUNT\(\*\) OVER \(\) AS total_users FROM per_user ORDER BY amount DESC, id ASC LIMIT \$3`).
		WithArgs(start, end, 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "amount", "recharge_count", "total_amount", "total_users"}).
			AddRow(int64(7), "big@example.com", 300.0, int64(2), 512.5, int64(4)).
			AddRow(int64(3), "small@example.com", 12.5, int64(1), 512.5, int64(4)))

	data, err := repo.GetOperationsRechargeRanking(context.Background(), start, end, 50)
	require.NoError(t, err)
	require.Len(t, data.Users, 2)
	require.Equal(t, int64(7), data.Users[0].UserID)
	require.Equal(t, "big@example.com", data.Users[0].Email)
	require.Equal(t, 300.0, data.Users[0].Amount)
	require.Equal(t, int64(2), data.Users[0].RechargeCount)
	require.Equal(t, "small@example.com", data.Users[1].Email)
	require.Equal(t, 512.5, data.TotalAmount)
	require.Equal(t, int64(4), data.TotalUsers)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationsRechargeRankingWithoutRechargesIsEmpty(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`WITH recharges AS`).
		WithArgs(start, start.AddDate(0, 0, 1), 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "amount", "recharge_count", "total_amount", "total_users"}))

	data, err := repo.GetOperationsRechargeRanking(context.Background(), start, start.AddDate(0, 0, 1), 50)
	require.NoError(t, err)
	require.NotNil(t, data.Users)
	require.Empty(t, data.Users)
	require.Zero(t, data.TotalAmount)
	require.Zero(t, data.TotalUsers)
	require.NoError(t, mock.ExpectationsWereMet())
}
