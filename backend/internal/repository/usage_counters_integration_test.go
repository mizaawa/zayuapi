//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestPersistentUsageCounterMigration(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	_, err := tx.ExecContext(ctx, `CREATE SCHEMA counter_migration_test; SET LOCAL search_path TO counter_migration_test`)
	require.NoError(t, err)
	before, err := os.ReadFile("testdata/usage_counters_before.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(before))
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("213_persistent_usage_counters.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	after, err := os.ReadFile("testdata/usage_counters_after.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(after))
	require.NoError(t, err)
}

func TestPersistentUsageCountersSurviveCleanup(t *testing.T) {
	for _, mode := range []string{"all", "retention", "filtered"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			client := tx.Client()
			repo := newUsageLogRepositoryWithSQL(client, tx)
			user := mustCreateUser(t, client, &service.User{})
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "counter-cleanup"})
			account := mustCreateAccount(t, client, &service.Account{Name: "counter-cleanup", Platform: "openai"})
			log := &service.UsageLog{
				UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
				RequestID: "counter-cleanup", Model: "gpt-test", InputTokens: 10, OutputTokens: 20,
				CacheCreationTokens: 3, CacheReadTokens: 4, TotalCost: 5, ActualCost: 2.5,
				CreatedAt: time.Now(),
			}
			inserted, err := repo.Create(ctx, log)
			require.NoError(t, err)
			require.True(t, inserted)
			inserted, err = repo.Create(ctx, log)
			require.NoError(t, err)
			require.False(t, inserted)
			before, err := repo.GetDashboardStats(ctx)
			require.NoError(t, err)
			cleanup := newUsageCleanupRepositoryWithSQL(nil, tx)
			var deleted int64
			switch mode {
			case "all":
				deleted, err = cleanup.DeleteUsageLogsThroughIDBatch(ctx, log.ID, 10000)
			case "retention":
				deleted, err = cleanup.DeleteUsageLogsBeforeBatch(ctx, log.CreatedAt.Add(time.Second), 10000)
			case "filtered":
				deleted, err = cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{
					StartTime: log.CreatedAt.Add(-time.Second), EndTime: log.CreatedAt.Add(time.Second), UserID: &user.ID,
				}, 10000)
			}
			require.NoError(t, err)
			require.Greater(t, deleted, int64(0))
			userStats, err := repo.GetUserDashboardStats(ctx, user.ID)
			require.NoError(t, err)
			require.EqualValues(t, 1, userStats.TotalRequests)
			require.EqualValues(t, 37, userStats.TotalTokens)
			require.EqualValues(t, 1, userStats.TodayRequests)
			require.Equal(t, 2.5, userStats.TotalActualCost)
			require.Len(t, userStats.ByPlatform, 1)
			require.Equal(t, 2.5, userStats.ByPlatform[0].TotalActualCost)
			after, err := repo.GetDashboardStats(ctx)
			require.NoError(t, err)
			require.Equal(t, before.TotalRequests, after.TotalRequests)
			require.Equal(t, before.TotalTokens, after.TotalTokens)
			require.Equal(t, before.TotalActualCost, after.TotalActualCost)
			require.Equal(t, before.TodayActualCost, after.TodayActualCost)
			require.Equal(t, before.TotalAccountCost, after.TotalAccountCost)
		})
	}
}

func TestPersistentUsageCountersRefundAfterCleanup(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	billing := &usageBillingRepository{db: integrationDB}
	requestID := fmt.Sprintf("video:counter-refund:%d", time.Now().UnixNano())
	user := mustCreateUser(t, client, &service.User{Balance: 10})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: requestID})
	account := mustCreateAccount(t, client, &service.Account{Name: "counter-refund", Platform: "openai"})
	cmd := &service.UsageBillingCommand{
		RequestID: requestID, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
		BalanceCost: 2.5,
	}
	result, err := billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	accountCost, rate := 2.0, 0.5
	log := &service.UsageLog{
		UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
		RequestID: requestID, Model: "video-test", InputTokens: 10, OutputTokens: 20,
		TotalCost: 5, ActualCost: 2.5, AccountStatsCost: &accountCost, AccountRateMultiplier: &rate,
		CreatedAt: time.Now(),
	}
	_, err = repo.Create(ctx, log)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM usage_logs WHERE user_id = $1`, user.ID)
	require.NoError(t, err)
	_, err = repo.Create(ctx, log)
	require.NoError(t, err)
	before, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, before.TotalRequests)
	refund := &service.UsageBillingReversalCommand{ReversalRequestID: requestID + ":refund", Original: *cmd}
	for _, applied := range []bool{true, false} {
		result, err := billing.Reverse(ctx, refund)
		require.NoError(t, err)
		require.Equal(t, applied, result.Applied)
	}
	after, err := repo.GetUserDashboardStats(ctx, user.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, after.TotalRequests)
	require.EqualValues(t, 30, after.TotalTokens)
	require.Zero(t, after.TotalActualCost)
	var accountTotal, balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT SUM(account_cost) FROM usage_counters WHERE user_id = $1 AND bucket_date = DATE '0001-01-01'`, user.ID).Scan(&accountTotal))
	require.Zero(t, accountTotal)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, user.ID).Scan(&balance))
	require.Equal(t, 10.0, balance)
}
