ALTER TABLE users
    ADD COLUMN IF NOT EXISTS exclusive_recharge_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_users_exclusive_recharge_enabled
    ON users (id) WHERE exclusive_recharge_enabled = TRUE AND deleted_at IS NULL;
