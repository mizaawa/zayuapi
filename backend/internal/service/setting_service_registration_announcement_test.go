//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_RegistrationAnnouncementDefaults(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "false", repo.values[SettingKeyRegistrationAnnouncementEnabled])
	require.Equal(t, "", repo.values[SettingKeyRegistrationAnnouncementContent])

	settings := svc.parseSettings(map[string]string{})
	require.False(t, settings.RegistrationAnnouncementEnabled)
	require.Empty(t, settings.RegistrationAnnouncementContent)
}

func TestSettingService_RegistrationAnnouncementPersists(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		RegistrationAnnouncementEnabled: true,
		RegistrationAnnouncementContent: "  Welcome\nPlease use a valid email.  ",
	})
	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyRegistrationAnnouncementEnabled])
	require.Equal(t, "Welcome\nPlease use a valid email.", repo.updates[SettingKeyRegistrationAnnouncementContent])

	settings, err := NewSettingService(&settingGetAllRepoStub{values: repo.updates}, &config.Config{}).
		GetAllSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.RegistrationAnnouncementEnabled)
	require.Equal(t, "Welcome\nPlease use a valid email.", settings.RegistrationAnnouncementContent)
}

func TestSettingService_RegistrationAnnouncementPublicAndInjected(t *testing.T) {
	for _, tt := range []struct {
		name    string
		values  map[string]string
		enabled bool
		content string
	}{
		{name: "missing", values: map[string]string{}},
		{
			name: "enabled plain text",
			values: map[string]string{
				SettingKeyRegistrationAnnouncementEnabled: "true",
				SettingKeyRegistrationAnnouncementContent: "  Welcome\n<b>plain text</b>  ",
			},
			enabled: true,
			content: "Welcome\n<b>plain text</b>",
		},
		{
			name: "disabled with saved content",
			values: map[string]string{
				SettingKeyRegistrationAnnouncementEnabled: "false",
				SettingKeyRegistrationAnnouncementContent: "Saved notice",
			},
			content: "Saved notice",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewSettingService(&settingPublicRepoStub{values: tt.values}, &config.Config{})
			settings, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.enabled, settings.RegistrationAnnouncementEnabled)
			require.Equal(t, tt.content, settings.RegistrationAnnouncementContent)

			injected, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			payload, ok := injected.(*PublicSettingsInjectionPayload)
			require.True(t, ok)
			require.Equal(t, settings.RegistrationAnnouncementEnabled, payload.RegistrationAnnouncementEnabled)
			require.Equal(t, settings.RegistrationAnnouncementContent, payload.RegistrationAnnouncementContent)
		})
	}
}
