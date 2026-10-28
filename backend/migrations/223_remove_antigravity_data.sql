-- Retire the Antigravity platform: remove its accounts, groups and pricing.
--
-- Accounts and groups use soft delete (deleted_at), matching what the admin
-- delete actions do, so historical usage and billing rows stay intact and
-- every query that filters deleted_at IS NULL stops seeing them. Join rows and
-- platform-scoped configuration are removed outright. Nothing here touches
-- usage logs, balances or payment records.

-- 1) Groups on the Antigravity platform.
DROP TABLE IF EXISTS pg_temp.retired_antigravity_groups;
CREATE TEMP TABLE retired_antigravity_groups ON COMMIT DROP AS
SELECT id FROM groups WHERE platform = 'antigravity' AND deleted_at IS NULL;

-- Subscriptions to those groups end, as when an admin deletes the group.
UPDATE user_subscriptions SET deleted_at = NOW()
WHERE deleted_at IS NULL AND group_id IN (SELECT id FROM retired_antigravity_groups);

DELETE FROM user_allowed_groups WHERE group_id IN (SELECT id FROM retired_antigravity_groups);
DELETE FROM user_blocked_groups WHERE group_id IN (SELECT id FROM retired_antigravity_groups);
DELETE FROM user_group_rate_multipliers WHERE group_id IN (SELECT id FROM retired_antigravity_groups);
DELETE FROM channel_groups WHERE group_id IN (SELECT id FROM retired_antigravity_groups);
DELETE FROM account_groups WHERE group_id IN (SELECT id FROM retired_antigravity_groups);

-- Plans that sell an Antigravity group can no longer be fulfilled.
UPDATE subscription_plans SET for_sale = FALSE, updated_at = NOW()
WHERE for_sale = TRUE AND group_id IN (SELECT id FROM retired_antigravity_groups);

-- Other groups must not fall back to a removed group.
UPDATE groups SET fallback_group_id = NULL
WHERE fallback_group_id IN (SELECT id FROM retired_antigravity_groups);
UPDATE groups SET fallback_group_id_on_invalid_request = NULL
WHERE fallback_group_id_on_invalid_request IN (SELECT id FROM retired_antigravity_groups);

-- API keys bound to a removed group become ungrouped, as with ON DELETE SET NULL.
UPDATE api_keys SET group_id = NULL
WHERE group_id IN (SELECT id FROM retired_antigravity_groups);

UPDATE composite_model_routes SET deleted_at = NOW()
WHERE deleted_at IS NULL AND group_id IN (SELECT id FROM retired_antigravity_groups);

UPDATE groups SET deleted_at = NOW()
WHERE id IN (SELECT id FROM retired_antigravity_groups);

-- 2) Antigravity accounts (spark shadows of an Antigravity parent included).
DROP TABLE IF EXISTS pg_temp.retired_antigravity_accounts;
CREATE TEMP TABLE retired_antigravity_accounts ON COMMIT DROP AS
SELECT id FROM accounts
WHERE deleted_at IS NULL
  AND (platform = 'antigravity'
       OR parent_account_id IN (SELECT id FROM accounts WHERE platform = 'antigravity'));

DELETE FROM account_groups WHERE account_id IN (SELECT id FROM retired_antigravity_accounts);
DELETE FROM scheduled_test_plans WHERE account_id IN (SELECT id FROM retired_antigravity_accounts);

UPDATE accounts SET deleted_at = NOW(), schedulable = FALSE
WHERE id IN (SELECT id FROM retired_antigravity_accounts);

-- 3) Antigravity routes inside other (composite) groups.
DROP TABLE IF EXISTS pg_temp.retired_antigravity_routes;
CREATE TEMP TABLE retired_antigravity_routes ON COMMIT DROP AS
SELECT id FROM composite_model_routes
WHERE deleted_at IS NULL AND target_platform = 'antigravity';

UPDATE composite_model_routes SET deleted_at = NOW()
WHERE id IN (SELECT id FROM retired_antigravity_routes);

-- 4) Channel pricing, mappings and account-stats rules for Antigravity.
DELETE FROM channel_model_pricing WHERE platform = 'antigravity';
DELETE FROM channel_account_stats_model_pricing WHERE platform = 'antigravity';
DELETE FROM channel_account_stats_pricing_rules AS rule
WHERE NOT EXISTS (
    SELECT 1 FROM channel_account_stats_model_pricing AS pricing WHERE pricing.rule_id = rule.id
) AND (
    rule.group_ids && ARRAY(SELECT id FROM groups WHERE platform = 'antigravity')
    OR rule.account_ids && ARRAY(SELECT id FROM accounts WHERE platform = 'antigravity')
);
UPDATE channels SET model_mapping = model_mapping - 'antigravity', updated_at = NOW()
WHERE model_mapping ? 'antigravity';

-- 5) Per-user Antigravity spending quotas.
UPDATE user_platform_quotas SET deleted_at = NOW()
WHERE deleted_at IS NULL AND platform = 'antigravity';

-- 6) Rebuild scheduler snapshots so removed accounts, groups and routes leave
-- the cache. Only enqueue when something was actually retired.
INSERT INTO scheduler_outbox (event_type)
SELECT 'full_rebuild'
WHERE EXISTS (SELECT 1 FROM retired_antigravity_groups)
   OR EXISTS (SELECT 1 FROM retired_antigravity_accounts)
   OR EXISTS (SELECT 1 FROM retired_antigravity_routes);
