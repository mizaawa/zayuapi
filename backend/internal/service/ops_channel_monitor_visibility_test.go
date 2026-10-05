//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsChannelMonitorUsageVisibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		err   error
		hide  bool
	}{
		{name: "enabled", value: "true", hide: true},
		{name: "disabled", value: "false"},
		{name: "unset", err: ErrSettingNotFound},
		{name: "read failure", value: "true", err: errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubOpsRepoForUserErr{}
			svc := &OpsService{opsRepo: repo, settingRepo: &channelMonitorUsageLogSettingRepo{value: tc.value, err: tc.err}}
			filter := &OpsErrorLogFilter{UsageView: true, Page: 2, PageSize: 10}
			_, err := svc.GetErrorLogs(context.Background(), filter)
			require.NoError(t, err)
			require.Equal(t, tc.hide, repo.gotFilter.ExcludeChannelMonitor)
			require.False(t, filter.ExcludeChannelMonitor, "do not mutate the caller's filter")
			require.Equal(t, 2, repo.gotFilter.Page)

			_, err = svc.GetErrorLogs(context.Background(), &OpsErrorLogFilter{})
			require.NoError(t, err)
			require.False(t, repo.gotFilter.ExcludeChannelMonitor, "operational diagnostics remain visible")

			_, err = svc.ListUserErrorRequests(context.Background(), 42, &OpsErrorLogFilter{})
			require.NoError(t, err)
			require.Equal(t, tc.hide, repo.gotFilter.ExcludeChannelMonitor)
			require.Equal(t, int64(42), *repo.gotFilter.UserID)
		})
	}
}
