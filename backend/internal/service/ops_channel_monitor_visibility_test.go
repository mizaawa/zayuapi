//go:build unit

package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type monitorErrorDetailRepo struct {
	stubOpsRepoForUserErr
	monitor bool
}

func (r *monitorErrorDetailRepo) GetErrorLogByIDWithVisibility(ctx context.Context, id int64, hidden bool) (*OpsErrorLogDetail, error) {
	if hidden && r.monitor {
		return nil, sql.ErrNoRows
	}
	return r.GetErrorLogByID(ctx, id)
}

func TestOpsChannelMonitorErrorDetailVisibility(t *testing.T) {
	ctx := context.Background()
	owner := int64(42)
	for _, hidden := range []bool{true, false} {
		for _, monitor := range []bool{true, false} {
			value := "false"
			if hidden {
				value = "true"
			}
			repo := &monitorErrorDetailRepo{monitor: monitor, stubOpsRepoForUserErr: stubOpsRepoForUserErr{
				detailToReturn: &OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{ID: 7, UserID: &owner, StatusCode: 500}},
			}}
			svc := &OpsService{opsRepo: repo, settingRepo: &channelMonitorUsageLogSettingRepo{value: value}}
			detail, err := svc.GetUserErrorRequestDetail(ctx, owner, 7)
			if hidden && monitor {
				require.True(t, infraerrors.IsNotFound(err))
				require.Nil(t, detail)
			} else {
				require.NoError(t, err)
				require.NotNil(t, detail)
			}
			_, err = svc.GetUserErrorRequestDetail(ctx, owner+1, 7)
			require.True(t, infraerrors.IsNotFound(err))
			opsDetail, err := svc.GetErrorLogByID(ctx, 7)
			require.NoError(t, err)
			require.NotNil(t, opsDetail)
		}
	}
}

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
