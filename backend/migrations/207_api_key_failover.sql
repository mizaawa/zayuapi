ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS failover_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS failover_group_id BIGINT,
    ADD COLUMN IF NOT EXISTS failover_max_retries INTEGER NOT NULL DEFAULT 3,
    ADD COLUMN IF NOT EXISTS failover_cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    ADD COLUMN IF NOT EXISTS failover_cooldown_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS failover_revision BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_failover_config_check' AND conrelid = 'api_keys'::regclass) THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_failover_config_check CHECK (
            failover_max_retries BETWEEN 1 AND 10
            AND failover_cooldown_seconds > 0
            AND (NOT failover_enabled OR (group_id IS NOT NULL AND failover_group_id IS NOT NULL AND failover_group_id <> group_id))
        );
    END IF;
END $$;

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        RETURN OLD;
    END IF;

    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.group_id IS DISTINCT FROM NEW.group_id
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at
       OR OLD.failover_enabled IS DISTINCT FROM NEW.failover_enabled
       OR OLD.failover_group_id IS DISTINCT FROM NEW.failover_group_id
       OR OLD.failover_max_retries IS DISTINCT FROM NEW.failover_max_retries
       OR OLD.failover_cooldown_seconds IS DISTINCT FROM NEW.failover_cooldown_seconds
       OR OLD.failover_cooldown_until IS DISTINCT FROM NEW.failover_cooldown_until
       OR OLD.failover_revision IS DISTINCT FROM NEW.failover_revision THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
