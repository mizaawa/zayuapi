ALTER TABLE media_billing_reservations
    ADD COLUMN final_quote JSONB,
    ADD COLUMN refund_requested BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX media_billing_outcome_pending_idx
    ON media_billing_reservations(next_check_at)
    WHERE state IN ('reserved', 'pending') AND (final_quote IS NOT NULL OR refund_requested);
