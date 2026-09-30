//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_CustomMenuForceNewTabDefaultsDisabled(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	require.False(t, svc.parseSettings(map[string]string{}).CustomMenuForceNewTab)
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "false", repo.values[SettingKeyCustomMenuForceNewTab])
}

func TestSettingService_CustomMenuForceNewTabPersistsAndParses(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			repo := &settingUpdateRepoStub{}
			svc := NewSettingService(repo, &config.Config{})

			require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{
				CustomMenuForceNewTab: enabled,
			}))
			require.Equal(t, strconv.FormatBool(enabled), repo.updates[SettingKeyCustomMenuForceNewTab])
			require.Equal(t, enabled, svc.parseSettings(repo.updates).CustomMenuForceNewTab)
		})
	}
}

func TestSettingService_CustomMenuForceNewTabPublicAndInjection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  string
		enabled bool
	}{
		{name: "missing"},
		{name: "enabled", stored: "true", enabled: true},
		{name: "disabled", stored: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingPublicRepoStub{values: map[string]string{}}
			if tc.stored != "" {
				repo.values[SettingKeyCustomMenuForceNewTab] = tc.stored
			}
			svc := NewSettingService(repo, &config.Config{})

			settings, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.enabled, settings.CustomMenuForceNewTab)

			injected, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			payload, ok := injected.(*PublicSettingsInjectionPayload)
			require.True(t, ok)
			require.Equal(t, tc.enabled, payload.CustomMenuForceNewTab)
		})
	}
}
