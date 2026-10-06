//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type channelMonitorUsageLogSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *channelMonitorUsageLogSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyChannelMonitorHideUsageLogs {
		return "", ErrSettingNotFound
	}
	return r.value, r.err
}

func TestSettingService_ChannelMonitorUsageLogsHidden(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		err   error
		want  bool
	}{
		{name: "enabled", value: "true", want: true},
		{name: "disabled", value: "false"},
		{name: "missing", err: ErrSettingNotFound},
		{name: "read failure", err: errors.New("read failed"), want: true},
		{name: "canceled read", err: context.Canceled, want: true},
		{name: "timed out read", err: context.DeadlineExceeded, want: true},
		{name: "invalid", value: "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := NewSettingService(&channelMonitorUsageLogSettingRepo{value: test.value, err: test.err}, &config.Config{})
			require.Equal(t, test.want, svc.IsChannelMonitorUsageLogsHidden(context.Background()))
		})
	}
	var nilService *SettingService
	require.False(t, nilService.IsChannelMonitorUsageLogsHidden(context.Background()))
	require.False(t, (&SettingService{}).IsChannelMonitorUsageLogsHidden(context.Background()))
}

func TestSettingService_ChannelMonitorUsageLogsSetting(t *testing.T) {
	for _, value := range []string{"", "true", "false"} {
		t.Run("stored="+value, func(t *testing.T) {
			svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{
				SettingKeyChannelMonitorHideUsageLogs: value,
			}}, &config.Config{})
			settings, err := svc.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, value == "true", settings.ChannelMonitorHideUsageLogs)
		})
	}
	for _, enabled := range []bool{true, false} {
		repo := &settingUpdateRepoStub{}
		svc := NewSettingService(repo, &config.Config{})
		err := svc.UpdateSettings(context.Background(), &SystemSettings{ChannelMonitorHideUsageLogs: enabled})
		require.NoError(t, err)
		require.Equal(t, enabled, repo.updates[SettingKeyChannelMonitorHideUsageLogs] == "true")
		require.Contains(t, repo.updates, SettingKeyChannelMonitorHideUsageLogs)
	}
}

func TestSettingService_ChannelMonitorUsageLogsSettingIsAdminOnly(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
		SettingKeyChannelMonitorHideUsageLogs: "true",
	}}, &config.Config{})

	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	injected, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	for _, value := range []any{settings, injected} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(body), SettingKeyChannelMonitorHideUsageLogs)
		require.NotContains(t, string(body), "ChannelMonitorHideUsageLogs")
	}
}
