-- Financial snapshots contain the current balance only. Record new daily net
-- changes per user so prior days can be shown without reading retained logs.
CREATE TABLE IF NOT EXISTS dashboard_balance_daily_changes (
    bucket_date DATE NOT NULL,
    user_id BIGINT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('user', 'admin')),
    amount NUMERIC(30, 10) NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_date, user_id, role)
);

CREATE TABLE IF NOT EXISTS dashboard_balance_tracking (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    started_on DATE NOT NULL
);
INSERT INTO dashboard_balance_tracking (singleton, started_on)
-- Installation day may be partial; the following day is the first complete bucket.
VALUES (TRUE, CURRENT_DATE + 1)
ON CONFLICT (singleton) DO NOTHING;

CREATE OR REPLACE FUNCTION maintain_dashboard_balance_daily_changes() RETURNS TRIGGER AS $$
DECLARE
    old_amount NUMERIC(30, 10) := 0;
    new_amount NUMERIC(30, 10) := 0;
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.deleted_at IS NULL AND NEW.role IN ('user', 'admin') THEN
            new_amount := NEW.balance;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.deleted_at IS NULL AND OLD.role IN ('user', 'admin') THEN
            old_amount := OLD.balance;
        END IF;
    ELSE
        IF OLD.deleted_at IS NULL AND OLD.role IN ('user', 'admin') THEN
            old_amount := OLD.balance;
        END IF;
        IF NEW.deleted_at IS NULL AND NEW.role IN ('user', 'admin') THEN
            new_amount := NEW.balance;
        END IF;
    END IF;

    IF TG_OP = 'UPDATE' THEN
        IF OLD.role IS DISTINCT FROM NEW.role THEN
            IF old_amount <> 0 THEN
                INSERT INTO dashboard_balance_daily_changes AS d (bucket_date, user_id, role, amount)
                VALUES (CURRENT_DATE, OLD.id, OLD.role, -old_amount)
                ON CONFLICT (bucket_date, user_id, role) DO UPDATE SET amount = d.amount + EXCLUDED.amount;
            END IF;
            old_amount := 0;
        END IF;
    END IF;

    IF TG_OP = 'DELETE' THEN
        IF old_amount <> 0 THEN
            INSERT INTO dashboard_balance_daily_changes AS d (bucket_date, user_id, role, amount)
            VALUES (CURRENT_DATE, OLD.id, OLD.role, -old_amount)
            ON CONFLICT (bucket_date, user_id, role) DO UPDATE SET amount = d.amount + EXCLUDED.amount;
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.role IN ('user', 'admin') AND new_amount <> old_amount THEN
        INSERT INTO dashboard_balance_daily_changes AS d (bucket_date, user_id, role, amount)
        VALUES (CURRENT_DATE, NEW.id, NEW.role, new_amount - old_amount)
        ON CONFLICT (bucket_date, user_id, role) DO UPDATE SET amount = d.amount + EXCLUDED.amount;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS dashboard_balance_daily_changes_write ON users;
CREATE TRIGGER dashboard_balance_daily_changes_write
AFTER INSERT OR UPDATE OF balance, role, deleted_at OR DELETE ON users
FOR EACH ROW EXECUTE FUNCTION maintain_dashboard_balance_daily_changes();
