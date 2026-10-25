//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Old clients may still send removed settings. They must not restore a deleted
// control or overwrite legacy database rows during an unrelated settings save.
func TestGatewaySettings_RemovedOAuthFieldsAreIgnoredAndNotExposed(t *testing.T) {
	legacyKeys := []string{
		"enable_fingerprint_unification", "enable_metadata_passthrough", "enable_cch_signing",
		"enable_claude_oauth_system_prompt_injection", "claude_oauth_system_prompt", "claude_oauth_system_prompt_blocks",
		"enable_anthropic_cache_ttl_1h_injection", "rewrite_message_cache_control", "enable_client_dateline_normalization",
		"antigravity_user_agent_version", "openai_codex_user_agent", "openai_codex_client_version",
		"openai_codex_client_version_synced", "openai_codex_version_auto_sync_enabled", "min_codex_version", "max_codex_version",
		"codex_cli_only_blacklist", "codex_cli_only_whitelist", "codex_cli_only_allow_app_server_clients", "codex_cli_only_engine_fingerprint_signals",
	}
	stored := map[string]string{service.SettingKeySiteName: "Gateway", service.SettingKeyPromoCodeEnabled: "true"}
	payload := map[string]any{"risk_control_enabled": true}
	for _, key := range legacyKeys {
		stored[key] = "legacy-stored"
		// A structured value would fail binding for the old scalar fields.
		payload[key] = map[string]any{"removed": true}
	}
	h, repo := newStepUpSwitchTestHandler(t, stored)
	rec := doUpdateSettings(t, h, payload, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyRiskControlEnabled])
	for _, key := range legacyKeys {
		require.Equal(t, "legacy-stored", repo.values[key], key)
	}
	for _, response := range []*httptest.ResponseRecorder{rec, getGatewaySettingsForRemovalTest(t, h)} {
		var decoded struct{ Data map[string]any }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
		for _, key := range legacyKeys {
			require.NotContains(t, decoded.Data, key)
		}
		require.Equal(t, "Gateway", decoded.Data["site_name"])
	}
}

func getGatewaySettingsForRemovalTest(t *testing.T, h *SettingHandler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec
}
