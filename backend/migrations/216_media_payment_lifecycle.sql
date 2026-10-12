ALTER TABLE media_billing_reservations
    ADD COLUMN workbench_kind TEXT NOT NULL DEFAULT '' CHECK (workbench_kind IN ('', 'image', 'video')),
    ADD COLUMN queue_lease_until TIMESTAMPTZ,
    ADD COLUMN dispatch_started_at TIMESTAMPTZ;

CREATE INDEX media_billing_queued_idx ON media_billing_reservations(queue_lease_until)
    WHERE state = 'reserved' AND workbench_kind <> '' AND dispatch_started_at IS NULL;

CREATE TABLE media_billing_events (
    reservation_id TEXT NOT NULL REFERENCES media_billing_reservations(id) ON DELETE CASCADE,
    ordinal SMALLINT NOT NULL CHECK (ordinal BETWEEN 1 AND 3),
    stage TEXT NOT NULL CHECK (stage IN ('request_created', 'prepaid', 'settled', 'refunded')),
    amount NUMERIC(20,8) NOT NULL CHECK (amount >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (reservation_id, ordinal),
    CHECK ((ordinal = 1 AND stage = 'request_created') OR
           (ordinal = 2 AND stage = 'prepaid') OR
           (ordinal = 3 AND stage IN ('settled', 'refunded')))
);
