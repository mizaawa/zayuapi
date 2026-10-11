SET LOCAL TIME ZONE 'Asia/Shanghai';
CREATE TABLE users (id BIGINT PRIMARY KEY, balance NUMERIC NOT NULL, deleted_at TIMESTAMPTZ);
CREATE TABLE redeem_codes (
    id BIGINT PRIMARY KEY, status TEXT NOT NULL, used_by BIGINT, type TEXT NOT NULL, value NUMERIC NOT NULL
);
CREATE TABLE usage_logs (
    id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL DEFAULT 1, api_key_id BIGINT NOT NULL DEFAULT 1,
    request_id TEXT NOT NULL DEFAULT '', billing_mode TEXT NOT NULL DEFAULT 'token',
    group_id BIGINT, account_id BIGINT NOT NULL DEFAULT 1,
    input_tokens INTEGER NOT NULL DEFAULT 10, output_tokens INTEGER NOT NULL DEFAULT 20,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 3, cache_read_tokens INTEGER NOT NULL DEFAULT 4,
    total_cost NUMERIC NOT NULL DEFAULT 5, actual_cost NUMERIC NOT NULL DEFAULT 2.5,
    account_stats_cost NUMERIC, account_rate_multiplier NUMERIC DEFAULT 0.5,
    duration_ms INTEGER, created_at TIMESTAMPTZ NOT NULL DEFAULT '2026-10-11 00:01:00+08'
);
CREATE TABLE usage_billing_dedup (request_id TEXT, api_key_id BIGINT, PRIMARY KEY (request_id, api_key_id));
CREATE TABLE usage_billing_dedup_archive (LIKE usage_billing_dedup INCLUDING ALL);
INSERT INTO usage_billing_dedup VALUES ('legacy-request', 1);
INSERT INTO users VALUES (1, 12, NULL), (2, 100, now());
INSERT INTO redeem_codes VALUES (1, 'used', 1, 'balance', 200), (2, 'unused', NULL, 'balance', 10);
INSERT INTO usage_logs (id) VALUES (1);
