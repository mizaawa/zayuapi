-- Existing monitors continue making real requests until simulation is enabled.
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS simulate_requests BOOLEAN NOT NULL DEFAULT FALSE;
