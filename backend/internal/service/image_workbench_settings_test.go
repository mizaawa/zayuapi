//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestImageWorkbenchRuntimeDefaultsAndLiveSettings(t *testing.T) {
	repo := &settingPublicRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	ctx := context.Background()
	defaults := svc.GetImageWorkbenchRuntime(ctx)
	require.True(t, defaults.Enabled)
	require.Equal(t, 5, defaults.Limit(false))
	require.Equal(t, 5, defaults.Limit(true))
	require.Equal(t, 15*time.Minute, defaults.Retention())

	repo.values = map[string]string{
		SettingKeyImageWorkbenchEnabled:                "false",
		SettingKeyImageWorkbenchMaxConcurrent:          "8",
		SettingKeyImageWorkbenchAdminExempt:            "true",
		SettingKeyImageWorkbenchCustomRetentionEnabled: "true",
		SettingKeyImageWorkbenchRetentionMinutes:       "30",
		SettingKeyImageWorkbenchTutorialURL:            " https://docs.example.com/images ",
	}
	updated := svc.GetImageWorkbenchRuntime(ctx)
	require.False(t, updated.Enabled)
	require.Equal(t, 8, updated.Limit(false))
	require.Zero(t, updated.Limit(true))
	require.Equal(t, 30*time.Minute, updated.Retention())
	require.Equal(t, "https://docs.example.com/images", updated.TutorialURL)

	repo.values[SettingKeyImageWorkbenchCustomRetentionEnabled] = "false"
	require.Equal(t, 15*time.Minute, svc.GetImageWorkbenchRuntime(ctx).Retention())
	repo.err = errors.New("settings unavailable")
	require.False(t, svc.GetImageWorkbenchRuntime(ctx).Enabled)
}

func TestImageWorkbenchPublicSettingsAndInjection(t *testing.T) {
	repo := &settingPublicRepoStub{values: map[string]string{
		SettingKeyImageWorkbenchEnabled:                "false",
		SettingKeyImageWorkbenchMaxConcurrent:          "9",
		SettingKeyImageWorkbenchAdminExempt:            "true",
		SettingKeyImageWorkbenchCustomRetentionEnabled: "true",
		SettingKeyImageWorkbenchRetentionMinutes:       "25",
		SettingKeyImageWorkbenchTutorialURL:            "https://docs.example.com/images",
	}}
	svc := NewSettingService(repo, &config.Config{})
	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.ImageWorkbenchEnabled)
	require.Equal(t, 9, settings.ImageWorkbenchMaxConcurrent)
	require.True(t, settings.ImageWorkbenchAdminExempt)
	require.True(t, settings.ImageWorkbenchCustomRetentionEnabled)
	require.Equal(t, 25, settings.ImageWorkbenchRetentionMinutes)
	require.Equal(t, "https://docs.example.com/images", settings.ImageWorkbenchTutorialURL)

	injection, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	payload := injection.(*PublicSettingsInjectionPayload)
	require.Equal(t, settings.ImageWorkbenchEnabled, payload.ImageWorkbenchEnabled)
	require.Equal(t, settings.ImageWorkbenchMaxConcurrent, payload.ImageWorkbenchMaxConcurrent)
	require.Equal(t, settings.ImageWorkbenchAdminExempt, payload.ImageWorkbenchAdminExempt)
	require.Equal(t, settings.ImageWorkbenchCustomRetentionEnabled, payload.ImageWorkbenchCustomRetentionEnabled)
	require.Equal(t, settings.ImageWorkbenchRetentionMinutes, payload.ImageWorkbenchRetentionMinutes)
	require.Equal(t, settings.ImageWorkbenchTutorialURL, payload.ImageWorkbenchTutorialURL)
}

func TestImageWorkbenchSettingsRejectInvalidValues(t *testing.T) {
	for _, settings := range []*SystemSettings{
		{ImageWorkbenchMaxConcurrent: -1},
		{ImageWorkbenchMaxConcurrent: 101},
		{ImageWorkbenchRetentionMinutes: -1},
		{ImageWorkbenchRetentionMinutes: 1441},
		{ImageWorkbenchTutorialURL: "javascript:alert(1)"},
		{ImageWorkbenchTutorialURL: "//docs.example.com"},
		{ImageWorkbenchTutorialURL: "https://user:password@docs.example.com"},
	} {
		require.Error(t, ValidateImageWorkbenchSettings(settings))
	}
	require.NoError(t, ValidateImageWorkbenchSettings(&SystemSettings{
		ImageWorkbenchMaxConcurrent: 5, ImageWorkbenchRetentionMinutes: 15,
		ImageWorkbenchTutorialURL: "https://docs.example.com/images",
	}))
}
