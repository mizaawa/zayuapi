-- A token request owns its funds until a single durable terminal outcome.
-- Quotes intentionally retain the price/ledger snapshot at admission and the
-- exact final charge; neither Redis nor an in-memory worker owns money.
CREATE TABLE token_billing_reservations (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'reserved' CHECK (state IN ('reserved', 'dispatched', 'settled', 'released')),
    hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount >= 0),
    quote JSONB NOT NULL,
    final_quote JSONB,
    settled_quote JSONB,
    settlement_kind TEXT NOT NULL DEFAULT '' CHECK (settlement_kind IN ('', 'actual', 'maximum')),
    outstanding_amount NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (outstanding_amount >= 0),
    lease_until TIMESTAMPTZ NOT NULL,
    dispatched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX token_billing_recovery_idx ON token_billing_reservations(lease_until)
    WHERE state IN ('reserved', 'dispatched');
CREATE INDEX token_billing_debt_idx ON token_billing_reservations(user_id)
    WHERE outstanding_amount > 0;
