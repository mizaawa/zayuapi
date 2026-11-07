-- OpenAI Fast 用户计费倍率。默认关闭，存量分组沿用原有 service_tier 计费。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS fast_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS fast_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 2.0;
