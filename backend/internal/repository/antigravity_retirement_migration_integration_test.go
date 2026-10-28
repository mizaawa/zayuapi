//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration223RemovesAntigravityDataAndKeepsOthers(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("223_remove_antigravity_data.sql")
	require.NoError(t, err)

	insertID := func(query string, args ...any) int64 {
		t.Helper()
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&id))
		return id
	}
	suffix := uniqueNano()

	userID := insertID(`INSERT INTO users (email, password_hash, role, status) VALUES ($1, 'hash', 'user', 'active') RETURNING id`, "ag-retire-"+suffix+"@example.com")
	agGroup := insertID(`INSERT INTO groups (name, platform, status, subscription_type, rate_multiplier) VALUES ($1, 'antigravity', 'active', 'subscription', 1) RETURNING id`, "ag-group-"+suffix)
	keepGroup := insertID(`INSERT INTO groups (name, platform, status, subscription_type, rate_multiplier, fallback_group_id) VALUES ($1, 'anthropic', 'active', 'standard', 1, $2) RETURNING id`, "keep-group-"+suffix, agGroup)
	agAccount := insertID(`INSERT INTO accounts (name, platform, type) VALUES ($1, 'antigravity', 'apikey') RETURNING id`, "ag-account-"+suffix)
	keepAccount := insertID(`INSERT INTO accounts (name, platform, type) VALUES ($1, 'anthropic', 'apikey') RETURNING id`, "keep-account-"+suffix)
	_, err = tx.ExecContext(ctx, `INSERT INTO account_groups (account_id, group_id, priority) VALUES ($1, $2, 1), ($3, $4, 1)`, agAccount, agGroup, keepAccount, keepGroup)
	require.NoError(t, err)
	agKey := insertID(`INSERT INTO api_keys (user_id, key, name, group_id, status) VALUES ($1, $2, 'ag key', $3, 'active') RETURNING id`, userID, "sk-ag-"+suffix, agGroup)
	subID := insertID(`INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status) VALUES ($1, $2, NOW(), NOW() + INTERVAL '30 days', 'active') RETURNING id`, userID, agGroup)
	planID := insertID(`INSERT INTO subscription_plans (group_id, name, price) VALUES ($1, 'AG plan', 10) RETURNING id`, agGroup)
	channelID := insertID(`INSERT INTO channels (name, model_mapping) VALUES ($1, '{"antigravity":{"a":"b"},"anthropic":{"c":"d"}}'::jsonb) RETURNING id`, "ag-channel-"+suffix)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_model_pricing (channel_id, platform, models) VALUES ($1, 'antigravity', '["ag-model"]'::jsonb), ($1, 'anthropic', '["claude"]'::jsonb)`, channelID)
	require.NoError(t, err)
	agQuota := insertID(`INSERT INTO user_platform_quotas (user_id, platform, daily_limit_usd) VALUES ($1, 'antigravity', 5) RETURNING id`, userID)

	var outboxBefore int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = 'full_rebuild'`).Scan(&outboxBefore))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err, "migration must be safe to run twice")

	isDeleted := func(table string, id int64) bool {
		t.Helper()
		var deleted bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM `+table+` WHERE id = $1`, id).Scan(&deleted))
		return deleted
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, tx.QueryRowContext(ctx, query, args...).Scan(&n))
		return n
	}

	require.True(t, isDeleted("groups", agGroup))
	require.True(t, isDeleted("accounts", agAccount))
	require.True(t, isDeleted("user_subscriptions", subID))
	require.True(t, isDeleted("user_platform_quotas", agQuota))
	require.False(t, isDeleted("groups", keepGroup))
	require.False(t, isDeleted("accounts", keepAccount))

	require.Zero(t, count(`SELECT COUNT(*) FROM account_groups WHERE group_id = $1 OR account_id = $2`, agGroup, agAccount))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM account_groups WHERE group_id = $1 AND account_id = $2`, keepGroup, keepAccount))
	require.Zero(t, count(`SELECT COUNT(*) FROM api_keys WHERE id = $1 AND group_id IS NOT NULL`, agKey))
	require.Zero(t, count(`SELECT COUNT(*) FROM groups WHERE id = $1 AND fallback_group_id IS NOT NULL`, keepGroup))
	require.Zero(t, count(`SELECT COUNT(*) FROM subscription_plans WHERE id = $1 AND for_sale`, planID))
	require.Zero(t, count(`SELECT COUNT(*) FROM channel_model_pricing WHERE channel_id = $1 AND platform = 'antigravity'`, channelID))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM channel_model_pricing WHERE channel_id = $1 AND platform = 'anthropic'`, channelID))
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM channels WHERE id = $1 AND NOT (model_mapping ? 'antigravity') AND model_mapping ? 'anthropic'`, channelID))
	// One rebuild for the first run; the second run retires nothing and adds none.
	require.Equal(t, 1, count(`SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = 'full_rebuild'`)-outboxBefore)
}
