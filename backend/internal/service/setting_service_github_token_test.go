//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingServiceGitHubTokenReadsCurrentSetting(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	for _, test := range []struct {
		stored string
		want   string
	}{
		{},
		{stored: "  first-secret  ", want: "first-secret"},
		{stored: "second-secret", want: "second-secret"},
		{},
	} {
		if test.stored != "" {
			repo.values[SettingKeyUpdateGitHubToken] = test.stored
		} else {
			delete(repo.values, SettingKeyUpdateGitHubToken)
		}
		got, err := svc.GetUpdateGitHubToken(context.Background())
		require.NoError(t, err)
		require.Equal(t, test.want, got)
	}
}

func TestSettingServiceGitHubTokenExcludedFromPublicSettings(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
		SettingKeyUpdateGitHubToken: "private-secret",
	}}, &config.Config{})
	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	injected, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	for _, payload := range []any{settings, injected} {
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-secret")
		require.NotContains(t, string(encoded), "update_github_token")
	}
}
