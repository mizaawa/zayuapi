package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyCustomSystemPromptMigration(t *testing.T) {
	content, err := FS.ReadFile("208_api_key_custom_system_prompt.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, field := range []string{"custom_system_prompt_enabled", "custom_system_prompt_force", "custom_system_prompt"} {
		require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS "+field)
		require.Contains(t, sql, "OLD."+field+" IS DISTINCT FROM NEW."+field)
	}
	require.Contains(t, sql, "custom_system_prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "custom_system_prompt_force BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "custom_system_prompt TEXT NOT NULL DEFAULT ''")
	require.Contains(t, sql, "octet_length(custom_system_prompt) <= 32768")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()")
}
