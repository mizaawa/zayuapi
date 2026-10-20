-- Expiration releases only the funds owned by one durable hold. Keeping this
-- receipt separate from terminal refunds lets long-running batch jobs settle
-- their successful output after the money becomes available again.
CREATE TABLE IF NOT EXISTS balance_hold_releases (
    source_kind TEXT NOT NULL CHECK (source_kind IN ('batch_image', 'legacy_token')),
    source_id TEXT NOT NULL,
    api_key_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0),
    released_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (source_kind, source_id, api_key_id)
);

CREATE INDEX IF NOT EXISTS balance_hold_releases_user_idx
    ON balance_hold_releases (user_id, released_at);
