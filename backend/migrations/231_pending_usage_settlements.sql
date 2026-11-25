-- A completed upstream call must remain recoverable if its accounting transaction times out.
CREATE TABLE IF NOT EXISTS pending_usage_settlements (
    request_id text NOT NULL,
    api_key_id bigint NOT NULL,
    fingerprint text NOT NULL,
    receipt jsonb NOT NULL,
    retry_at timestamptz NOT NULL DEFAULT (now() + interval '2 minutes'),
    attempts integer NOT NULL DEFAULT 0,
    PRIMARY KEY (request_id, api_key_id)
);
CREATE INDEX IF NOT EXISTS pending_usage_settlements_due ON pending_usage_settlements (retry_at);
