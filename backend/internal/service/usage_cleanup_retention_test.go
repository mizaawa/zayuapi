package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type retentionSettingRepoStub struct {
	SettingRepository
	raw    string
	err    error
	writes int
}

func (r *retentionSettingRepoStub) GetValue(context.Context, string) (string, error) {
	if r.raw == "" {
		return "", ErrSettingNotFound
	}
	return r.raw, nil
}

func (r *retentionSettingRepoStub) Set(_ context.Context, _ string, value string) error {
	if r.err != nil {
		return r.err
	}
	r.raw = value
	r.writes++
	return nil
}

func newRetentionSettingRepoStub(t *testing.T, settings UsageCleanupScheduleSettings) *retentionSettingRepoStub {
	t.Helper()
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	return &retentionSettingRepoStub{raw: string(raw)}
}

func newRetentionCleanupService(repo UsageCleanupRepository) *UsageCleanupService {
	svc := NewUsageCleanupService(repo, nil, nil, &config.Config{
		UsageCleanup: config.UsageCleanupConfig{Enabled: true, MaxRangeDays: 1, BatchSize: 2},
	})
	// Tests inspect enqueued tasks separately from execution.
	atomic.StoreInt32(&svc.running, 1)
	return svc
}

func TestUsageCleanupRetentionDaysValidation(t *testing.T) {
	for _, days := range []int{-1, 0, 3651} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			repo := &cleanupRepoStub{}
			svc := newRetentionCleanupService(repo)
			svc.SetSettingsRepository(&retentionSettingRepoStub{})

			_, err := svc.CreateRetentionTask(context.Background(), days, false, 7)
			require.Equal(t, "USAGE_CLEANUP_INVALID_RETENTION_DAYS", infraerrors.Reason(err))
			_, err = svc.UpdateScheduleSettings(context.Background(), true, 1, days, false, 7)
			require.Equal(t, "USAGE_CLEANUP_INVALID_RETENTION_DAYS", infraerrors.Reason(err))
			require.Empty(t, repo.created)
		})
	}
}

func TestUsageCleanupRetentionTaskUsesAgeCutoffBeyondRangeLimit(t *testing.T) {
	repo := &cleanupRepoStub{}
	svc := newRetentionCleanupService(repo)
	before := time.Now().UTC().AddDate(0, 0, -90)

	task, err := svc.CreateRetentionTask(context.Background(), 90, false, 7)
	require.NoError(t, err)
	require.NotNil(t, task.Filters.RetentionCutoff)
	require.WithinDuration(t, before, *task.Filters.RetentionCutoff, time.Second)
	require.True(t, task.Filters.StartTime.IsZero())
	require.True(t, task.Filters.EndTime.IsZero())
	require.Equal(t, UsageCleanupStatusPending, task.Status)
	require.Equal(t, int64(7), task.CreatedBy)
	require.Len(t, repo.created, 1)
}

func TestUsageCleanupScheduleDefaultsDisabled(t *testing.T) {
	svc := newRetentionCleanupService(&cleanupRepoStub{})
	svc.SetSettingsRepository(&retentionSettingRepoStub{})
	settings, err := svc.GetScheduleSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 1, settings.IntervalDays)
	require.Equal(t, 90, settings.RetentionDays)
}

func TestUsageCleanupScheduleIntervalValidation(t *testing.T) {
	for _, days := range []int{-1, 0, 366} {
		svc := newRetentionCleanupService(&cleanupRepoStub{})
		svc.SetSettingsRepository(&retentionSettingRepoStub{})
		_, err := svc.UpdateScheduleSettings(context.Background(), true, days, 90, false, 7)
		require.Equal(t, "USAGE_CLEANUP_INVALID_INTERVAL_DAYS", infraerrors.Reason(err))
	}
}

func TestUsageCleanupScheduleHonorsConfiguredInterval(t *testing.T) {
	lastRun := time.Now().UTC().Add(-2 * 24 * time.Hour)
	repo := &cleanupRepoStub{}
	svc := newRetentionCleanupService(repo)
	settingsRepo := newRetentionSettingRepoStub(t, UsageCleanupScheduleSettings{
		Enabled: true, IntervalDays: 7, RetentionDays: 14, UpdatedBy: 7, LastRunAt: &lastRun,
	})
	svc.SetSettingsRepository(settingsRepo)

	svc.runScheduledCleanup()
	require.Empty(t, repo.created)
	require.Zero(t, settingsRepo.writes)

	settings, err := svc.UpdateScheduleSettings(context.Background(), true, 1, 14, false, 7)
	require.NoError(t, err)
	require.Equal(t, 1, settings.IntervalDays)
	require.Equal(t, lastRun, *settings.LastRunAt)
	svc.runScheduledCleanup()
	require.Len(t, repo.created, 1)
}

func TestUsageCleanupScheduleEnqueuesOnlyWhenEnabledAndDue(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-23 * time.Hour)
	due := now.Add(-25 * time.Hour)
	cases := []struct {
		name      string
		enabled   bool
		owner     int64
		lastRun   *time.Time
		wantTasks int
	}{
		{name: "disabled", owner: 7},
		{name: "missing operator", enabled: true},
		{name: "not yet due", enabled: true, owner: 7, lastRun: &recent},
		{name: "first run", enabled: true, owner: 7, wantTasks: 1},
		{name: "due run", enabled: true, owner: 7, lastRun: &due, wantTasks: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cleanupRepoStub{}
			svc := newRetentionCleanupService(repo)
			settingsRepo := newRetentionSettingRepoStub(t, UsageCleanupScheduleSettings{
				Enabled: tc.enabled, IntervalDays: 1, RetentionDays: 14, UpdatedBy: tc.owner, LastRunAt: tc.lastRun,
			})
			svc.SetSettingsRepository(settingsRepo)

			svc.runScheduledCleanup()
			require.Len(t, repo.created, tc.wantTasks)
			require.Equal(t, tc.wantTasks, settingsRepo.writes)
			if tc.wantTasks > 0 {
				require.NotNil(t, repo.created[0].Filters.RetentionCutoff)
				require.WithinDuration(t, now.AddDate(0, 0, -14), *repo.created[0].Filters.RetentionCutoff, time.Second)
				settings, err := svc.GetScheduleSettings(context.Background())
				require.NoError(t, err)
				require.NotNil(t, settings.LastRunAt)
				require.WithinDuration(t, now, *settings.LastRunAt, time.Second)
			}
		})
	}
}

func TestUsageCleanupScheduleEnqueueFailureRestoresLastRun(t *testing.T) {
	lastRun := time.Now().UTC().Add(-25 * time.Hour)
	repo := &cleanupRepoStub{createErr: errors.New("database unavailable")}
	svc := newRetentionCleanupService(repo)
	settingsRepo := newRetentionSettingRepoStub(t, UsageCleanupScheduleSettings{
		Enabled: true, IntervalDays: 1, RetentionDays: 14, UpdatedBy: 7, LastRunAt: &lastRun,
	})
	svc.SetSettingsRepository(settingsRepo)

	svc.runScheduledCleanup()
	settings, err := svc.GetScheduleSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, lastRun, *settings.LastRunAt)
	require.Equal(t, 2, settingsRepo.writes)
	require.Empty(t, repo.created)
}

type retentionMaintenanceRepoStub struct {
	*cleanupRepoStub
	dropped    []time.Time
	cutoffs    []time.Time
	limits     []int
	results    []int64
	maxID      int64
	maxIDErr   error
	throughIDs []int64
}

func (r *retentionMaintenanceRepoStub) GetUsageLogsStorageStats(context.Context) (*UsageLogsStorageStats, error) {
	return &UsageLogsStorageStats{TableBytes: 100, IndexBytes: 40, TotalBytes: 160}, nil
}

func (r *retentionMaintenanceRepoStub) GetUsageLogsMaxID(context.Context) (int64, error) {
	return r.maxID, r.maxIDErr
}

func (r *retentionMaintenanceRepoStub) DeleteUsageLogsThroughIDBatch(_ context.Context, maxID int64, limit int) (int64, error) {
	r.throughIDs = append(r.throughIDs, maxID)
	r.limits = append(r.limits, limit)
	deleted := r.results[0]
	r.results = r.results[1:]
	return deleted, nil
}

func (r *retentionMaintenanceRepoStub) DropUsageLogsPartitionsBefore(_ context.Context, cutoff time.Time) error {
	r.dropped = append(r.dropped, cutoff)
	return nil
}

func (r *retentionMaintenanceRepoStub) DeleteUsageLogsBeforeBatch(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	r.cutoffs = append(r.cutoffs, cutoff)
	r.limits = append(r.limits, limit)
	deleted := r.results[0]
	r.results = r.results[1:]
	return deleted, nil
}

type retentionDashboardSpy struct {
	DashboardAggregationRepository
	recomputed chan [2]time.Time
}

func (r *retentionDashboardSpy) RecomputeRange(_ context.Context, start, end time.Time) error {
	r.recomputed <- [2]time.Time{start, end}
	return nil
}

func TestUsageCleanupRetentionExecutionPreservesDashboardAggregates(t *testing.T) {
	repo := &retentionMaintenanceRepoStub{cleanupRepoStub: &cleanupRepoStub{}, results: []int64{2, 1}}
	dashboardRepo := &retentionDashboardSpy{recomputed: make(chan [2]time.Time, 1)}
	svc := newRetentionCleanupService(repo)
	svc.dashboard = NewDashboardAggregationService(dashboardRepo, nil, &config.Config{
		DashboardAgg: config.DashboardAggregationConfig{Enabled: true},
	})
	cutoff := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	task := &UsageCleanupTask{ID: 7, DeletedRows: 5, Filters: UsageCleanupFilters{
		RetentionCutoff: &cutoff, StartTime: cutoff.Add(-time.Hour), EndTime: cutoff,
	}}

	svc.executeTask(context.Background(), task)
	require.Equal(t, []time.Time{cutoff, cutoff}, repo.cutoffs)
	require.Equal(t, []int{2, 2}, repo.limits)
	require.Empty(t, repo.deleteCalls)
	require.Equal(t, []cleanupMarkCall{{taskID: 7, deletedRows: 7}, {taskID: 7, deletedRows: 8}}, repo.progressCalls)
	require.Equal(t, []cleanupMarkCall{{taskID: 7, deletedRows: 8}}, repo.markSucceeded)
	require.Empty(t, repo.markFailed)
	select {
	case <-dashboardRepo.recomputed:
		t.Fatal("retention cleanup must preserve historical dashboard aggregates")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestUsageCleanupDeleteAllTaskCapturesBoundary(t *testing.T) {
	for _, maxID := range []int64{0, 42} {
		t.Run(time.Duration(maxID).String(), func(t *testing.T) {
			repo := &retentionMaintenanceRepoStub{cleanupRepoStub: &cleanupRepoStub{}, maxID: maxID}
			svc := newRetentionCleanupService(repo)

			task, err := svc.CreateRetentionTask(context.Background(), 0, true, 7)
			require.NoError(t, err)
			require.NotNil(t, task.Filters.DeleteThroughID)
			require.Equal(t, maxID, *task.Filters.DeleteThroughID)
			require.Nil(t, task.Filters.RetentionCutoff)
			require.Len(t, repo.created, 1)
			require.Equal(t, maxID, *repo.created[0].Filters.DeleteThroughID)
		})
	}
}

func TestUsageCleanupDeleteAllTaskBoundaryFailureDoesNotEnqueue(t *testing.T) {
	repo := &retentionMaintenanceRepoStub{cleanupRepoStub: &cleanupRepoStub{}, maxIDErr: errors.New("query failed")}
	svc := newRetentionCleanupService(repo)

	_, err := svc.CreateRetentionTask(context.Background(), 0, true, 7)
	require.ErrorContains(t, err, "snapshot usage log cleanup boundary")
	require.Empty(t, repo.created)
}

func TestUsageCleanupDeleteAllSchedulePersistsAndEnqueuesBoundedTask(t *testing.T) {
	repo := &retentionMaintenanceRepoStub{cleanupRepoStub: &cleanupRepoStub{}, maxID: 42}
	svc := newRetentionCleanupService(repo)
	svc.SetSettingsRepository(&retentionSettingRepoStub{})

	settings, err := svc.UpdateScheduleSettings(context.Background(), true, 3, 90, true, 7)
	require.NoError(t, err)
	require.True(t, settings.DeleteAll)
	loaded, err := svc.GetScheduleSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, settings, loaded)

	svc.runScheduledCleanup()
	require.Len(t, repo.created, 1)
	require.NotNil(t, repo.created[0].Filters.DeleteThroughID)
	require.Equal(t, int64(42), *repo.created[0].Filters.DeleteThroughID)
	require.Nil(t, repo.created[0].Filters.RetentionCutoff)
}

func TestUsageCleanupDeleteAllExecutionKeepsBoundaryAndPreservesAggregates(t *testing.T) {
	repo := &retentionMaintenanceRepoStub{cleanupRepoStub: &cleanupRepoStub{}, maxID: 42, results: []int64{2, 1}}
	dashboardRepo := &retentionDashboardSpy{recomputed: make(chan [2]time.Time, 1)}
	svc := newRetentionCleanupService(repo)
	svc.dashboard = NewDashboardAggregationService(dashboardRepo, nil, &config.Config{
		DashboardAgg: config.DashboardAggregationConfig{Enabled: true},
	})
	task, err := svc.CreateRetentionTask(context.Background(), 90, true, 7)
	require.NoError(t, err)

	// New requests receive larger IDs while the cleanup task remains bounded.
	repo.maxID = 100
	svc.executeTask(context.Background(), task)
	require.Equal(t, []int64{42, 42}, repo.throughIDs)
	require.Equal(t, []int{2, 2}, repo.limits)
	require.Empty(t, repo.cutoffs)
	require.Empty(t, repo.deleteCalls)
	require.Equal(t, []cleanupMarkCall{{taskID: task.ID, deletedRows: 2}, {taskID: task.ID, deletedRows: 3}}, repo.progressCalls)
	require.Equal(t, []cleanupMarkCall{{taskID: task.ID, deletedRows: 3}}, repo.markSucceeded)
	require.Empty(t, repo.markFailed)
	select {
	case <-dashboardRepo.recomputed:
		t.Fatal("delete-all cleanup must preserve historical dashboard aggregates")
	case <-time.After(25 * time.Millisecond):
	}
}
