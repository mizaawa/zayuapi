//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPendingUsageSettlementDoesNotSpendNewWindows(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	u := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 10})
	g := mustCreateGroup(t, client, &service.Group{Name: uuid.NewString(), Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription})
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: u.ID, GroupID: g.ID})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, GroupID: &g.ID, Key: "sk-" + uuid.NewString(), Name: "windows"})
	current := time.Now().UTC()
	old := current.Add(-24 * time.Hour)
	a := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey, Extra: map[string]any{
		"quota_daily_limit": 100.0, "quota_daily_used": 4.0, "quota_daily_start": current.Format(time.RFC3339Nano),
		"quota_weekly_limit": 100.0, "quota_weekly_used": 6.0, "quota_weekly_start": old.Add(-time.Hour).Format(time.RFC3339Nano),
	}})
	_, err := integrationDB.Exec(`UPDATE user_subscriptions SET daily_window_start=$2,weekly_window_start=$3,daily_usage_usd=4,weekly_usage_usd=6 WHERE id=$1`, sub.ID, current, old.Add(-time.Hour))
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE api_keys SET window_1d_start=$2,window_7d_start=$3,usage_1d=4,usage_7d=6 WHERE id=$1`, k.ID, current, old.Add(-time.Hour))
	require.NoError(t, err)
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: k.ID, UserID: u.ID, AccountID: a.ID, AccountType: a.Type, SubscriptionID: &sub.ID, SubscriptionCost: 1, APIKeyRateLimitCost: 1, AccountQuotaCost: 1, Model: "test", ChargedAt: old}
	row := &service.UsageLog{RequestID: cmd.RequestID, APIKeyID: k.ID, UserID: u.ID, AccountID: a.ID, Model: "test", GroupID: &g.ID, SubscriptionID: &sub.ID, ActualCost: 1, CreatedAt: old}
	repo := &usageBillingRepository{db: integrationDB}
	require.NoError(t, repo.StageUsage(ctx, cmd, row))
	_, err = repo.SettleUsage(ctx, cmd.RequestID, k.ID, cmd.RequestFingerprint)
	require.NoError(t, err)
	var d, w float64
	for _, q := range []struct {
		sql string
		id  int64
	}{
		{`SELECT daily_usage_usd,weekly_usage_usd FROM user_subscriptions WHERE id=$1`, sub.ID},
		{`SELECT usage_1d,usage_7d FROM api_keys WHERE id=$1`, k.ID},
		{`SELECT (extra->>'quota_daily_used')::numeric,(extra->>'quota_weekly_used')::numeric FROM accounts WHERE id=$1`, a.ID},
	} {
		require.NoError(t, integrationDB.QueryRow(q.sql, q.id).Scan(&d, &w))
		require.Equal(t, 4.0, d)
		require.Equal(t, 7.0, w)
	}
}

func TestPendingUsageSettlementRecoversExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := &usageBillingRepository{db: integrationDB}
	u := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 10})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, Key: "sk-" + uuid.NewString(), Name: "settlement"})
	a := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString(), Type: service.AccountTypeAPIKey})
	cmd := &service.UsageBillingCommand{RequestID: uuid.NewString(), APIKeyID: k.ID, AccountID: a.ID, AccountType: a.Type, UserID: u.ID, BalanceCost: 1.25, Model: "test", ChargedAt: time.Now().UTC()}
	usage := &service.UsageLog{RequestID: cmd.RequestID, APIKeyID: k.ID, UserID: u.ID, AccountID: a.ID, Model: "test", ActualCost: 1.25, TotalCost: 1.25, RateMultiplier: 1, CreatedAt: cmd.ChargedAt, APIKey: &service.APIKey{Key: "private-test-credential"}}
	require.NoError(t, repo.StageUsage(ctx, cmd, usage))
	require.NoError(t, repo.StageUsage(ctx, cmd, usage))
	var payload string
	require.NoError(t, integrationDB.QueryRow(`SELECT receipt::text FROM pending_usage_settlements WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, k.ID).Scan(&payload))
	require.NotContains(t, payload, "private-test-credential")
	conflict := *cmd
	conflict.RequestFingerprint = ""
	conflict.BalanceCost = 5
	require.ErrorIs(t, repo.StageUsage(ctx, &conflict, usage), service.ErrUsageBillingRequestConflict)

	lock, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Rollback() })
	_, err = lock.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID)
	require.NoError(t, err)
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = repo.SettleUsage(short, cmd.RequestID, k.ID, cmd.RequestFingerprint)
	cancel()
	require.Error(t, err)
	require.NoError(t, lock.Rollback())
	_, err = integrationDB.Exec(`UPDATE pending_usage_settlements SET retry_at=now() WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, k.ID)
	require.NoError(t, err)
	restarted := &usageBillingRepository{db: integrationDB}
	key, err := restarted.ClaimPendingUsage(ctx)
	require.NoError(t, err)
	require.NotNil(t, key)
	require.Equal(t, cmd.RequestID, key.RequestID)
	type outcome struct {
		applied bool
		err     error
	}
	ch := make(chan outcome, 2)
	for range 2 {
		go func() {
			res, err := restarted.SettleUsage(ctx, key.RequestID, key.APIKeyID, key.Fingerprint)
			ch <- outcome{res != nil && res.Applied, err}
		}()
	}
	one, two := <-ch, <-ch
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.NotEqual(t, one.applied, two.applied)
	// Lost reply after commit: staging and settling the same command does not resurrect it or debit again.
	require.NoError(t, repo.StageUsage(ctx, cmd, usage))
	res, err := repo.SettleUsage(ctx, cmd.RequestID, k.ID, cmd.RequestFingerprint)
	require.NoError(t, err)
	require.False(t, res.Applied)
	var balance, total float64
	var rows, claims, pending int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, u.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*),sum(actual_cost) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, k.ID).Scan(&rows, &total))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, k.ID).Scan(&claims))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM pending_usage_settlements WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, k.ID).Scan(&pending))
	require.Equal(t, 8.75, balance)
	require.Equal(t, 1.25, total)
	require.Equal(t, 1, rows)
	require.Equal(t, 1, claims)
	require.Zero(t, pending)
}
