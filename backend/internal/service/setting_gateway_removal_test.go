//go:build unit

package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewaySettings_UpdateDoesNotPersistRemovedOAuthControls(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	settings := &SystemSettings{MinClaudeCodeVersion: "2.1.0", MaxClaudeCodeVersion: "3.0.0", AllowUngroupedKeyScheduling: true}
	require.NoError(t, svc.UpdateSettings(context.Background(), settings))
	require.Equal(t, "2.1.0", repo.updates[SettingKeyMinClaudeCodeVersion])
	require.Equal(t, "3.0.0", repo.updates[SettingKeyMaxClaudeCodeVersion])
	require.Equal(t, "true", repo.updates[SettingKeyAllowUngroupedKeyScheduling])
	for _, key := range removedGatewaySettingsTestKeys() {
		require.NotContains(t, repo.updates, key)
	}
}

func TestGatewaySettings_LegacyRowsDoNotBecomeLiveControls(t *testing.T) {
	legacy := map[string]string{SettingKeyMinClaudeCodeVersion: "2.1.0"}
	for _, key := range removedGatewaySettingsTestKeys() {
		legacy[key] = "true"
	}
	svc := NewSettingService(&settingGetAllRepoStub{values: legacy}, &config.Config{})
	settings, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "2.1.0", settings.MinClaudeCodeVersion)
	settingsType := reflect.TypeOf(*settings)
	for _, field := range []string{
		"EnableFingerprintUnification", "EnableMetadataPassthrough", "EnableCCHSigning",
		"EnableClaudeOAuthSystemPromptInjection", "ClaudeOAuthSystemPrompt", "ClaudeOAuthSystemPromptBlocks",
		"EnableAnthropicCacheTTL1hInjection", "RewriteMessageCacheControl", "EnableClientDatelineNormalization",
		"AntigravityUserAgentVersion", "OpenAICodexUserAgent", "OpenAICodexClientVersion",
		"OpenAICodexClientVersionSynced", "OpenAICodexVersionAutoSyncEnabled", "MinCodexVersion", "MaxCodexVersion",
		"CodexCLIOnlyBlacklist", "CodexCLIOnlyWhitelist", "CodexCLIOnlyAllowAppServerClients", "CodexCLIOnlyEngineFingerprintSignals",
	} {
		_, exists := settingsType.FieldByName(field)
		require.False(t, exists, "removed control %s must not return as a live settings field", field)
	}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	var exposed map[string]any
	require.NoError(t, json.Unmarshal(raw, &exposed))
	for _, key := range removedGatewaySettingsTestKeys() {
		require.NotContains(t, exposed, key)
	}
}

func removedGatewaySettingsTestKeys() []string {
	return []string{
		"enable_fingerprint_unification", "enable_metadata_passthrough", "enable_cch_signing",
		"enable_claude_oauth_system_prompt_injection", "claude_oauth_system_prompt", "claude_oauth_system_prompt_blocks",
		"enable_anthropic_cache_ttl_1h_injection", "rewrite_message_cache_control", "enable_client_dateline_normalization",
		"antigravity_user_agent_version", "openai_codex_user_agent", "openai_codex_client_version",
		"openai_codex_client_version_synced", "openai_codex_version_auto_sync_enabled", "min_codex_version", "max_codex_version",
		"codex_cli_only_blacklist", "codex_cli_only_whitelist", "codex_cli_only_allow_app_server_clients", "codex_cli_only_engine_fingerprint_signals",
	}
}
