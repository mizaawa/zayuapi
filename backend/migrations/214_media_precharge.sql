CREATE TABLE media_billing_reservations (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'pending', 'settled', 'refunded')),
    quote JSONB NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX media_billing_task_owner_idx ON media_billing_reservations(api_key_id, task_id) WHERE task_id <> '';
CREATE INDEX media_billing_pending_idx ON media_billing_reservations(next_check_at) WHERE state = 'pending';
