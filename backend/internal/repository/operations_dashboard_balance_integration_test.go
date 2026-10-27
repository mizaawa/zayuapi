//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardBalanceDailyChangesTrackNetMovement(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users (email, password_hash, role, status, balance, concurrency)
		VALUES ('operations-balance-movement@example.test', 'hash', 'user', 'active', 10, 1)
	`)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT id FROM users WHERE email = 'operations-balance-movement@example.test'`).Scan(&userID))
	_, err = tx.ExecContext(ctx, `UPDATE users SET balance = 15 WHERE id = $1`, userID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE users SET balance = 12 WHERE id = $1`, userID)
	require.NoError(t, err)
	var movement float64
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT amount FROM dashboard_balance_daily_changes WHERE bucket_date = CURRENT_DATE AND user_id = $1 AND role = 'user'`, userID).Scan(&movement))
	require.Equal(t, 12.0, movement)
	_, err = tx.ExecContext(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, userID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT amount FROM dashboard_balance_daily_changes WHERE bucket_date = CURRENT_DATE AND user_id = $1 AND role = 'user'`, userID).Scan(&movement))
	require.Zero(t, movement)
}
