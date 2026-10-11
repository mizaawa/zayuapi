CREATE FUNCTION test_assert(label TEXT, ok BOOLEAN) RETURNS VOID AS $$
BEGIN
    IF ok IS DISTINCT FROM TRUE THEN
        RAISE EXCEPTION 'Counter assertion failed: %', label;
    END IF;
END;
$$ LANGUAGE plpgsql;

SELECT test_assert('no historical usage', NOT EXISTS (SELECT 1 FROM usage_counters));
INSERT INTO usage_logs (id, request_id) VALUES (100, 'legacy-request');
SELECT test_assert('legacy billing replay is outside the release window', NOT EXISTS (SELECT 1 FROM usage_counters));
DELETE FROM usage_logs WHERE id = 100;
SELECT test_assert('preserve current balance, no historical recharge',
    (SELECT SUM(amount) = 12 AND COUNT(*) FILTER (WHERE metric = 'recharged') = 0 FROM dashboard_user_financials));
UPDATE usage_logs SET actual_cost = 999 WHERE id = 1;
UPDATE redeem_codes SET value = 300 WHERE id = 1;
SELECT test_assert('legacy edits do not backfill usage', NOT EXISTS (SELECT 1 FROM usage_counters));
SELECT test_assert('legacy edits do not backfill recharge', NOT EXISTS (SELECT 1 FROM dashboard_user_financials WHERE metric = 'recharged'));

INSERT INTO usage_logs (id, duration_ms) VALUES (2, 100);
INSERT INTO usage_logs (id) VALUES (2) ON CONFLICT DO NOTHING;
INSERT INTO usage_logs (id, created_at) VALUES (3, '2026-10-10 23:59:00+08');
SELECT test_assert('deduplicated lifetime counters',
    (SELECT requests = 2 AND input_tokens = 20 AND output_tokens = 40 AND cache_creation_tokens = 6 AND cache_read_tokens = 8
        AND actual_cost = 5 AND total_cost = 10 AND account_cost = 5 AND duration_count = 1 AND total_duration_ms = 100
        FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
SELECT test_assert('timezone day boundary',
    (SELECT COUNT(*) = 2 AND SUM(requests) = 2 FROM usage_counters WHERE bucket_date <> DATE '0001-01-01'));
SELECT test_assert('today is separate from yesterday',
    (SELECT requests = 1 AND actual_cost = 2.5 FROM usage_counters WHERE bucket_date = DATE '2026-10-11'));

SAVEPOINT counter_rollback;
INSERT INTO usage_logs (id) VALUES (4);
UPDATE users SET balance = balance + 100 WHERE id = 1;
UPDATE redeem_codes SET status = 'used', used_by = 1 WHERE id = 2;
ROLLBACK TO SAVEPOINT counter_rollback;
SELECT test_assert('request rollback', (SELECT requests = 2 FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
SELECT test_assert('financial rollback', (SELECT SUM(amount) = 12 AND COUNT(*) = 1 FROM dashboard_user_financials WHERE user_id = 1));

UPDATE usage_logs SET actual_cost = 0, account_stats_cost = 0 WHERE id = 2;
UPDATE usage_logs SET actual_cost = 0, account_stats_cost = 0 WHERE id = 2;
SELECT test_assert('refund changes spend once, preserves requests and tokens',
    (SELECT requests = 2 AND input_tokens = 20 AND actual_cost = 2.5 AND account_cost = 2.5
        AND billable_requests = 1 AND billable_tokens = 37 AND billable_cost = 2.5
        FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
DELETE FROM usage_logs;
SELECT test_assert('cleanup preserves lifetime and daily counters',
    (SELECT COUNT(*) = 3 AND SUM(requests) = 4 AND SUM(actual_cost) = 5 FROM usage_counters));

INSERT INTO usage_billing_dedup (request_id, api_key_id) VALUES ('billed-request', 1);
INSERT INTO usage_logs (id, request_id) VALUES (10, 'billed-request');
DELETE FROM usage_logs;
INSERT INTO usage_logs (id, request_id) VALUES (11, 'billed-request');
SELECT test_assert('billing replay after log cleanup is not counted',
    (SELECT requests = 3 AND actual_cost = 5 FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
UPDATE usage_logs SET actual_cost = 0 WHERE id = 11;
SELECT test_assert('uncounted replay cannot subtract existing totals',
    (SELECT actual_cost = 5 FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
INSERT INTO usage_billing_dedup_archive SELECT * FROM usage_billing_dedup;
DELETE FROM usage_billing_dedup;
DELETE FROM usage_logs;
INSERT INTO usage_logs (id, request_id) VALUES (12, 'billed-request');
SELECT test_assert('archived billing identity retains deduplication',
    (SELECT requests = 3 FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));

INSERT INTO usage_logs (id, request_id, billing_mode, account_stats_cost) VALUES (13, 'video:refund', 'video', 2);
DELETE FROM usage_logs;
SELECT reverse_missing_usage_counters('video:refund', 1);
SELECT reverse_missing_usage_counters('video:refund', 1);
SELECT test_assert('refund after cleanup adjusts spend once',
    (SELECT requests = 4 AND actual_cost = 5 AND account_cost = 5 AND billable_requests = 2
        FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));
SELECT test_assert('refund receipt removed', NOT EXISTS (SELECT 1 FROM usage_counter_refunds));
INSERT INTO usage_logs (id, request_id, billing_mode, account_stats_cost) VALUES (14, 'video:present', 'video', 2);
UPDATE usage_logs SET actual_cost = 0, account_stats_cost = 0 WHERE id = 14;
SELECT reverse_missing_usage_counters('video:present', 1);
SELECT test_assert('refund with log still present does not double subtract',
    (SELECT requests = 5 AND actual_cost = 5 AND account_cost = 5 FROM usage_counters WHERE bucket_date = DATE '0001-01-01'));

UPDATE redeem_codes SET status = 'used', used_by = 1 WHERE id = 2;
UPDATE redeem_codes SET status = 'used', used_by = 1 WHERE id = 2;
UPDATE redeem_codes SET status = 'unused' WHERE id = 2;
UPDATE redeem_codes SET status = 'used' WHERE id = 2;
INSERT INTO redeem_codes VALUES (3, 'used', 1, 'admin_balance', 7);
INSERT INTO redeem_codes VALUES (3, 'used', 1, 'admin_balance', 7) ON CONFLICT DO NOTHING;
INSERT INTO redeem_codes VALUES (4, 'used', 1, 'balance', -9), (5, 'used', 1, 'subscription', 99);
SELECT test_assert('credited balance and admin recharge counted once',
    (SELECT amount = 17 FROM dashboard_user_financials WHERE user_id = 1 AND metric = 'recharged'));
DELETE FROM redeem_codes;
UPDATE users SET balance = 8 WHERE id = 1;
SELECT test_assert('balance tracks changes, recharge survives deletion',
    (SELECT SUM(amount) FILTER (WHERE metric = 'balance') = 8 AND SUM(amount) FILTER (WHERE metric = 'recharged') = 17 FROM dashboard_user_financials WHERE user_id = 1));
UPDATE users SET deleted_at = now() WHERE id = 1;
SELECT test_assert('deleted user balance excluded, recharge retained',
    (SELECT SUM(amount) FILTER (WHERE metric = 'balance') = 0 AND SUM(amount) FILTER (WHERE metric = 'recharged') = 17 FROM dashboard_user_financials WHERE user_id = 1));
DELETE FROM users;
SELECT test_assert('hard deletion retains recharge', (SELECT SUM(amount) = 17 FROM dashboard_user_financials WHERE metric = 'recharged'));
INSERT INTO usage_logs (id, user_id, total_cost, actual_cost, account_rate_multiplier)
VALUES (20, 9, 0.00000001, 0.00000001, 0.0001);
SELECT test_assert('account multipliers retain sub-cent precision',
    (SELECT account_cost = 0.000000000001 FROM usage_counters WHERE user_id = 9 AND bucket_date = DATE '0001-01-01'));
