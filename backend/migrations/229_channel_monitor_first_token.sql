-- latency_ms is the complete request duration; old checks have no measured TTFT.
ALTER TABLE channel_monitor_histories ADD COLUMN IF NOT EXISTS first_token_ms INTEGER;
