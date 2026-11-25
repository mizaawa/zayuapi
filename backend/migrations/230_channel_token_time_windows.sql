-- Price schedules are independent of context-length tiers. Preserve existing cards and group rates.
ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS time_windows jsonb NOT NULL DEFAULT '[]';
ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS time_windows jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS image_input_price numeric(20,12);
ALTER TABLE groups ALTER COLUMN ultrafast_rate_multiplier SET DEFAULT 8;
