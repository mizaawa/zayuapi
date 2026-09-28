//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDashboardStats_BalancesAndCreditedRecharges(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	baseline, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)

	active := mustCreateUser(t, client, &service.User{Balance: 12.50})
	mustCreateUser(t, client, &service.User{Balance: 5.25, Status: service.StatusDisabled})
	mustCreateUser(t, client, &service.User{Balance: -0.25})
	deleted := mustCreateUser(t, client, &service.User{Balance: 200})
	require.NoError(t, client.User.DeleteOneID(deleted.ID).Exec(ctx))

	for _, record := range []struct {
		code   string
		kind   string
		status string
		value  float64
		userID *int64
	}{
		{"balance-credited", service.RedeemTypeBalance, service.StatusUsed, 20, &active.ID},
		{"admin-credited", service.AdjustmentTypeAdminBalance, service.StatusUsed, 5, &active.ID},
		{"deleted-user-credited", service.RedeemTypeBalance, service.StatusUsed, 8, &deleted.ID},
		{"balance-deducted", service.RedeemTypeBalance, service.StatusUsed, -3, &active.ID},
		{"admin-deducted", service.AdjustmentTypeAdminBalance, service.StatusUsed, -2, &active.ID},
		{"balance-unused", service.RedeemTypeBalance, service.StatusUnused, 100, nil},
		{"unused-assigned", service.RedeemTypeBalance, service.StatusUnused, 100, &active.ID},
		{"balance-zero", service.RedeemTypeBalance, service.StatusUsed, 0, &active.ID},
		{"concurrency-used", service.RedeemTypeConcurrency, service.StatusUsed, 10, &active.ID},
		{"subscription-used", service.RedeemTypeSubscription, service.StatusUsed, 40, &active.ID},
	} {
		err := client.RedeemCode.Create().
			SetCode(record.code).
			SetType(record.kind).
			SetStatus(record.status).
			SetValue(record.value).
			SetNillableUsedBy(record.userID).
			Exec(ctx)
		require.NoError(t, err)
	}

	stats, err := repo.GetDashboardStats(ctx)
	require.NoError(t, err)
	require.InDelta(t, baseline.TotalBalance+17.5, stats.TotalBalance, 0.000001)
	require.InDelta(t, baseline.TotalRecharged+33, stats.TotalRecharged, 0.000001)

	end := time.Now()
	ranged, err := repo.GetDashboardStatsWithRange(ctx, end.Add(-time.Hour), end)
	require.NoError(t, err)
	require.Equal(t, stats.TotalBalance, ranged.TotalBalance)
	require.Equal(t, stats.TotalRecharged, ranged.TotalRecharged)
}
