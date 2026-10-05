ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS custom_system_prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS custom_system_prompt_force BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS custom_system_prompt TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'api_keys_custom_system_prompt_check' AND conrelid = 'api_keys'::regclass) THEN
        ALTER TABLE api_keys ADD CONSTRAINT api_keys_custom_system_prompt_check CHECK (
            octet_length(custom_system_prompt) <= 32768
            AND (NOT custom_system_prompt_enabled OR custom_system_prompt !~ '^[[:space:]]*$')
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
       OR OLD.failover_revision IS DISTINCT FROM NEW.failover_revision
       OR OLD.custom_system_prompt_enabled IS DISTINCT FROM NEW.custom_system_prompt_enabled
       OR OLD.custom_system_prompt_force IS DISTINCT FROM NEW.custom_system_prompt_force
       OR OLD.custom_system_prompt IS DISTINCT FROM NEW.custom_system_prompt THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
