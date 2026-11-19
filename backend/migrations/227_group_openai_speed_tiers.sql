-- Old groups retain their routing and old bills do not acquire a speculative rate.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS ultrafast_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 6
        CHECK (ultrafast_rate_multiplier BETWEEN 0.1 AND 100),
    ADD COLUMN IF NOT EXISTS forced_fast_mode VARCHAR(16) NOT NULL DEFAULT 'off'
        CHECK (forced_fast_mode IN ('off','fast','ultrafast') AND (forced_fast_mode = 'off' OR platform = 'openai'));

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS service_tier_multiplier DECIMAL(10,4)
    CHECK (service_tier_multiplier BETWEEN 0.1 AND 100);
