-- Only new inserts contribute. Existing logs and credited recharge records are
-- deliberately not backfilled. No foreign keys or DELETE triggers: retention
-- and user deletion must not erase cumulative usage or credited recharges.
CREATE TABLE IF NOT EXISTS usage_counters (
    bucket_date DATE NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC(30, 10) NOT NULL DEFAULT 0,
    actual_cost NUMERIC(30, 10) NOT NULL DEFAULT 0,
    account_cost NUMERIC(38, 16) NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    billable_requests BIGINT NOT NULL DEFAULT 0,
    billable_tokens BIGINT NOT NULL DEFAULT 0,
    billable_cost NUMERIC(30, 10) NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (bucket_date, user_id, api_key_id, group_id, account_id)
);
CREATE INDEX IF NOT EXISTS usage_counters_user_bucket_idx ON usage_counters (user_id, bucket_date);
COMMENT ON COLUMN usage_counters.bucket_date IS '0001-01-01 holds lifetime totals; other dates use the application database connection timezone';

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS counters_recorded BOOLEAN NOT NULL DEFAULT FALSE;
-- Existing billing identities are outside this release's accounting window.
-- A constant default marks them without scanning or backfilling their logs.
ALTER TABLE usage_billing_dedup ADD COLUMN IF NOT EXISTS counters_recorded BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE usage_billing_dedup ALTER COLUMN counters_recorded SET DEFAULT FALSE;
ALTER TABLE usage_billing_dedup_archive ADD COLUMN IF NOT EXISTS counters_recorded BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE usage_billing_dedup_archive ALTER COLUMN counters_recorded SET DEFAULT FALSE;

-- Only asynchronous video charges need a refundable receipt after log cleanup.
-- This contains numeric accounting selectors, never request/response content.
CREATE TABLE IF NOT EXISTS usage_counter_refunds (
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    bucket_date DATE NOT NULL,
    actual_cost NUMERIC(30, 10) NOT NULL,
    account_cost NUMERIC(38, 16) NOT NULL,
    billable_tokens BIGINT NOT NULL,
    PRIMARY KEY (request_id, api_key_id)
);

CREATE OR REPLACE FUNCTION mark_usage_counters_recorded() RETURNS TRIGGER AS $$
BEGIN
    NEW.counters_recorded := TRUE;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION accumulate_usage_counters(log_row usage_logs, delta BIGINT) RETURNS VOID AS $$
BEGIN
    INSERT INTO usage_counters AS c (
        bucket_date, user_id, api_key_id, group_id, account_id,
        requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
        total_cost, actual_cost, account_cost, total_duration_ms, duration_count,
        billable_requests, billable_tokens, billable_cost, last_used_at
    )
    SELECT bucket_date, log_row.user_id, log_row.api_key_id, COALESCE(log_row.group_id, 0), log_row.account_id,
        delta, delta * log_row.input_tokens, delta * log_row.output_tokens,
        delta * log_row.cache_creation_tokens, delta * log_row.cache_read_tokens,
        delta * log_row.total_cost, delta * log_row.actual_cost,
        delta * COALESCE(log_row.account_stats_cost, log_row.total_cost) * COALESCE(log_row.account_rate_multiplier, 1),
        delta * COALESCE(log_row.duration_ms, 0), delta * (log_row.duration_ms IS NOT NULL)::int,
        delta * (log_row.actual_cost > 0)::int,
        CASE WHEN log_row.actual_cost > 0 THEN delta * (log_row.input_tokens::bigint + log_row.output_tokens + log_row.cache_creation_tokens + log_row.cache_read_tokens) ELSE 0 END,
        CASE WHEN log_row.actual_cost > 0 THEN delta * log_row.actual_cost ELSE 0 END,
        log_row.created_at
    FROM (VALUES (DATE '0001-01-01'), (log_row.created_at::date)) AS buckets(bucket_date)
    ORDER BY bucket_date
    ON CONFLICT (bucket_date, user_id, api_key_id, group_id, account_id) DO UPDATE SET
        requests = c.requests + EXCLUDED.requests,
        input_tokens = c.input_tokens + EXCLUDED.input_tokens,
        output_tokens = c.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = c.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = c.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = c.total_cost + EXCLUDED.total_cost,
        actual_cost = c.actual_cost + EXCLUDED.actual_cost,
        account_cost = c.account_cost + EXCLUDED.account_cost,
        total_duration_ms = c.total_duration_ms + EXCLUDED.total_duration_ms,
        duration_count = c.duration_count + EXCLUDED.duration_count,
        billable_requests = c.billable_requests + EXCLUDED.billable_requests,
        billable_tokens = c.billable_tokens + EXCLUDED.billable_tokens,
        billable_cost = c.billable_cost + EXCLUDED.billable_cost,
        last_used_at = GREATEST(c.last_used_at, EXCLUDED.last_used_at);
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION maintain_usage_counters() RETURNS TRIGGER AS $$
DECLARE
    already_counted BOOLEAN;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NOT OLD.counters_recorded THEN
            RETURN NEW;
        END IF;
        PERFORM accumulate_usage_counters(OLD, -1);
    ELSE
        -- Reuse the durable billing identity after the detailed log is deleted.
        SELECT counters_recorded INTO already_counted FROM usage_billing_dedup
        WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id FOR UPDATE;
        IF FOUND THEN
            UPDATE usage_billing_dedup SET counters_recorded = TRUE
            WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id AND NOT counters_recorded;
        ELSE
            SELECT counters_recorded INTO already_counted FROM usage_billing_dedup_archive
            WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id FOR UPDATE;
            IF FOUND THEN
                UPDATE usage_billing_dedup_archive SET counters_recorded = TRUE
                WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id AND NOT counters_recorded;
            END IF;
        END IF;
        IF already_counted THEN
            UPDATE usage_logs SET counters_recorded = FALSE WHERE id = NEW.id;
            RETURN NEW;
        END IF;
    END IF;
    PERFORM accumulate_usage_counters(NEW, 1);
    IF (NEW.billing_mode = 'video' OR NEW.request_id LIKE 'video:%') AND NEW.request_id <> '' THEN
        IF NEW.actual_cost > 0 THEN
            INSERT INTO usage_counter_refunds (request_id, api_key_id, user_id, group_id, account_id,
                bucket_date, actual_cost, account_cost, billable_tokens)
            VALUES (NEW.request_id, NEW.api_key_id, NEW.user_id, COALESCE(NEW.group_id, 0), NEW.account_id,
                NEW.created_at::date, NEW.actual_cost,
                COALESCE(NEW.account_stats_cost, 0) * COALESCE(NEW.account_rate_multiplier, 1),
                NEW.input_tokens::bigint + NEW.output_tokens + NEW.cache_creation_tokens + NEW.cache_read_tokens)
            ON CONFLICT (request_id, api_key_id) DO UPDATE SET
                user_id = EXCLUDED.user_id, group_id = EXCLUDED.group_id,
                account_id = EXCLUDED.account_id, bucket_date = EXCLUDED.bucket_date,
                actual_cost = EXCLUDED.actual_cost, account_cost = EXCLUDED.account_cost,
                billable_tokens = EXCLUDED.billable_tokens;
        ELSE
            DELETE FROM usage_counter_refunds WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reverse_missing_usage_counters(request_key TEXT, key_id BIGINT) RETURNS VOID AS $$
DECLARE
    receipt usage_counter_refunds;
BEGIN
    SELECT * INTO receipt FROM usage_counter_refunds WHERE request_id = request_key AND api_key_id = key_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- Match the lifetime -> day -> receipt lock order used by log updates.
    PERFORM 1 FROM usage_counters
    WHERE user_id = receipt.user_id AND api_key_id = receipt.api_key_id
        AND group_id = receipt.group_id AND account_id = receipt.account_id
        AND bucket_date IN (DATE '0001-01-01', receipt.bucket_date)
    ORDER BY bucket_date FOR UPDATE;
    DELETE FROM usage_counter_refunds WHERE request_id = request_key AND api_key_id = key_id RETURNING * INTO receipt;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE usage_counters SET
        actual_cost = actual_cost - receipt.actual_cost,
        account_cost = account_cost - receipt.account_cost,
        billable_cost = billable_cost - receipt.actual_cost,
        billable_requests = billable_requests - 1,
        billable_tokens = billable_tokens - receipt.billable_tokens
    WHERE user_id = receipt.user_id AND api_key_id = receipt.api_key_id
        AND group_id = receipt.group_id AND account_id = receipt.account_id
        AND bucket_date IN (DATE '0001-01-01', receipt.bucket_date);
END;
$$ LANGUAGE plpgsql;

-- Mark before insertion, accumulate AFTER insertion so ON CONFLICT DO NOTHING
-- and transaction rollbacks never count a request twice.
DROP TRIGGER IF EXISTS usage_counters_mark ON usage_logs;
CREATE TRIGGER usage_counters_mark BEFORE INSERT ON usage_logs
FOR EACH ROW EXECUTE FUNCTION mark_usage_counters_recorded();
DROP TRIGGER IF EXISTS usage_counters_write ON usage_logs;
CREATE TRIGGER usage_counters_write AFTER INSERT OR UPDATE OF
    user_id, api_key_id, group_id, account_id, created_at,
    input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
    total_cost, actual_cost, account_stats_cost, account_rate_multiplier, duration_ms
ON usage_logs FOR EACH ROW EXECUTE FUNCTION maintain_usage_counters();

CREATE TABLE IF NOT EXISTS dashboard_user_financials (
    user_id BIGINT NOT NULL,
    metric TEXT NOT NULL CHECK (metric IN ('balance', 'recharged')),
    amount NUMERIC(30, 10) NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, metric)
);
-- Separate rows avoid reversing the user/balance lock order when a redeem
-- transaction records its credit before updating the user's balance.
-- Balance is a current asset, not historical usage. Preserve existing balances.
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;
INSERT INTO dashboard_user_financials (user_id, metric, amount)
SELECT id, 'balance', balance FROM users WHERE deleted_at IS NULL
ON CONFLICT (user_id, metric) DO NOTHING;

CREATE OR REPLACE FUNCTION maintain_dashboard_balance() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE dashboard_user_financials SET amount = 0 WHERE user_id = OLD.id AND metric = 'balance';
        RETURN OLD;
    END IF;
    INSERT INTO dashboard_user_financials (user_id, metric, amount)
    VALUES (NEW.id, 'balance', CASE WHEN NEW.deleted_at IS NULL THEN NEW.balance ELSE 0 END)
    ON CONFLICT (user_id, metric) DO UPDATE SET amount = EXCLUDED.amount;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS dashboard_balance_write ON users;
CREATE TRIGGER dashboard_balance_write AFTER INSERT OR UPDATE OF balance, deleted_at OR DELETE ON users
FOR EACH ROW EXECUTE FUNCTION maintain_dashboard_balance();

ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS recharge_counted BOOLEAN NOT NULL DEFAULT FALSE;

CREATE OR REPLACE FUNCTION mark_dashboard_recharge() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.recharge_counted := OLD.recharge_counted OR
            (OLD.status = 'used' AND OLD.used_by IS NOT NULL AND OLD.type IN ('balance', 'admin_balance') AND OLD.value > 0);
    ELSE
        NEW.recharge_counted := FALSE;
    END IF;
    NEW.recharge_counted := NEW.recharge_counted OR
        (NEW.status = 'used' AND NEW.used_by IS NOT NULL AND NEW.type IN ('balance', 'admin_balance') AND NEW.value > 0);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION maintain_dashboard_recharge() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.recharge_counted OR
            (OLD.status = 'used' AND OLD.used_by IS NOT NULL AND OLD.type IN ('balance', 'admin_balance') AND OLD.value > 0) THEN
            RETURN NEW;
        END IF;
    END IF;
    IF NEW.status = 'used' AND NEW.used_by IS NOT NULL AND NEW.type IN ('balance', 'admin_balance') AND NEW.value > 0 THEN
        -- Match the user -> financial row lock order of admin adjustments.
        PERFORM 1 FROM users WHERE id = NEW.used_by FOR NO KEY UPDATE;
        INSERT INTO dashboard_user_financials AS f (user_id, metric, amount)
        VALUES (NEW.used_by, 'recharged', NEW.value)
        ON CONFLICT (user_id, metric) DO UPDATE SET amount = f.amount + EXCLUDED.amount;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS dashboard_recharge_mark ON redeem_codes;
CREATE TRIGGER dashboard_recharge_mark BEFORE INSERT OR UPDATE ON redeem_codes
FOR EACH ROW EXECUTE FUNCTION mark_dashboard_recharge();
DROP TRIGGER IF EXISTS dashboard_recharge_write ON redeem_codes;
CREATE TRIGGER dashboard_recharge_write AFTER INSERT OR UPDATE ON redeem_codes
FOR EACH ROW EXECUTE FUNCTION maintain_dashboard_recharge();
