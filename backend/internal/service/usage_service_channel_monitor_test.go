//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type monitorUsageVisibilityRepo struct {
	UsageLogRepository
	filters []usagestats.UsageLogFilters
}

func (r *monitorUsageVisibilityRepo) ListWithFilters(_ context.Context, _ pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]UsageLog, *pagination.PaginationResult, error) {
	r.filters = append(r.filters, filters)
	return nil, &pagination.PaginationResult{}, nil
}

func (r *monitorUsageVisibilityRepo) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.filters = append(r.filters, filters)
	return &usagestats.UsageStats{}, nil
}

func (r *monitorUsageVisibilityRepo) GetByIDWithFilters(_ context.Context, _ int64, filters usagestats.UsageLogFilters) (*UsageLog, error) {
	r.filters = append(r.filters, filters)
	return nil, ErrUsageLogNotFound
}

func (r *monitorUsageVisibilityRepo) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, filters usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.filters = append(r.filters, filters)
	return nil, nil
}

func (r *monitorUsageVisibilityRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	r.filters = append(r.filters, filters)
	return nil, nil
}

func (r *monitorUsageVisibilityRepo) GetGroupStatsWithUsageFilters(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.filters = append(r.filters, filters)
	return nil, nil
}

func TestChannelMonitorUsageVisibility_AllDisplayQueriesAndToggle(t *testing.T) {
	ctx := context.Background()
	settings := &settingRepoStub{values: map[string]string{SettingKeyChannelMonitorHideUsageLogs: "true"}}
	repo := &monitorUsageVisibilityRepo{}
	s := ProvideUsageService(repo, nil, nil, nil, NewSettingService(settings, nil))
	params := pagination.PaginationParams{Page: 1, PageSize: 20}
	filters := usagestats.UsageLogFilters{UserID: 9, ExactTotal: true}

	_, _, err := s.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	_, _, err = s.ListWithFilters(ctx, params, usagestats.UsageLogFilters{})
	require.NoError(t, err)
	_, err = s.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	_, err = s.GetByID(ctx, 1)
	require.ErrorIs(t, err, ErrUsageLogNotFound)
	_, err = s.GetUsageTrendWithFilters(ctx, time.Time{}, time.Time{}, "day", filters)
	require.NoError(t, err)
	_, err = s.GetModelStatsWithFiltersBySource(ctx, time.Time{}, time.Time{}, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	_, err = s.GetGroupStatsWithFilters(ctx, time.Time{}, time.Time{}, filters)
	require.NoError(t, err)
	for _, captured := range repo.filters {
		require.True(t, captured.HideChannelMonitorLogs)
	}
	require.Equal(t, int64(9), repo.filters[0].UserID)
	require.True(t, repo.filters[0].ExactTotal)

	settings.values[SettingKeyChannelMonitorHideUsageLogs] = "false"
	_, _, err = s.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.False(t, repo.filters[len(repo.filters)-1].HideChannelMonitorLogs)
}

func TestChannelMonitorUsageVisibility_CacheSnapshotAndDefault(t *testing.T) {
	ctx := context.Background()
	settings := &settingRepoStub{values: map[string]string{SettingKeyChannelMonitorHideUsageLogs: "true"}}
	repo := &monitorUsageVisibilityRepo{}
	s := ProvideUsageService(repo, nil, nil, nil, NewSettingService(settings, nil))
	snapshotCtx, filters := s.PrepareLogFilters(ctx, usagestats.UsageLogFilters{})
	require.True(t, filters.HideChannelMonitorLogs)
	settings.values[SettingKeyChannelMonitorHideUsageLogs] = "false"
	_, err := s.GetStatsWithFilters(snapshotCtx, filters)
	require.NoError(t, err)
	require.True(t, repo.filters[0].HideChannelMonitorLogs)
	require.Equal(t, 1, settings.getValueCalls)
	_, latest := s.PrepareLogFilters(ctx, filters)
	require.False(t, latest.HideChannelMonitorLogs)

	legacy := NewUsageService(repo, nil, nil, nil)
	_, defaultFilters := legacy.PrepareLogFilters(ctx, filters)
	require.False(t, defaultFilters.HideChannelMonitorLogs)
}

func TestChannelMonitorUsageVisibility_SettingReadFailureAndRecovery(t *testing.T) {
	ctx := context.Background()
	settings := &channelMonitorUsageLogSettingRepo{err: errors.New("settings unavailable")}
	repo := &monitorUsageVisibilityRepo{}
	s := ProvideUsageService(repo, nil, nil, nil, NewSettingService(settings, nil))
	params := pagination.PaginationParams{Page: 1, PageSize: 20}
	filters := usagestats.UsageLogFilters{UserID: 9, ExactTotal: true}

	_, _, err := s.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	_, err = s.GetByID(ctx, 1)
	require.ErrorIs(t, err, ErrUsageLogNotFound)
	snapshotCtx, prepared := s.PrepareLogFilters(ctx, filters)
	require.True(t, prepared.HideChannelMonitorLogs)

	settings.err = nil
	settings.value = "false"
	_, err = s.GetStatsWithFilters(snapshotCtx, prepared)
	require.NoError(t, err)
	for _, captured := range repo.filters {
		require.True(t, captured.HideChannelMonitorLogs)
	}
	require.Equal(t, int64(9), repo.filters[0].UserID)
	require.True(t, repo.filters[0].ExactTotal)

	_, err = s.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.False(t, repo.filters[len(repo.filters)-1].HideChannelMonitorLogs)
}
