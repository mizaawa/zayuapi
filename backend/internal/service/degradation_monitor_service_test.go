//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDegradationConfigValidateAndNormalize(t *testing.T) {
	cfg := DefaultDegradationConfig()
	require.NoError(t, cfg.Validate())
	require.Equal(t, 25, cfg.MaxDisplayPerGroup)
	require.Equal(t, "high", cfg.DefaultReasoningEffort)

	for _, broken := range []DegradationConfig{
		{DefaultIntervalMinutes: 4, TimeoutMinutes: 10, MaxRetries: 1, MaxDisplayPerGroup: 25, DefaultReasoningEffort: "high"},
		{DefaultIntervalMinutes: 60, TimeoutMinutes: 61, MaxRetries: 1, MaxDisplayPerGroup: 25, DefaultReasoningEffort: "high"},
		{DefaultIntervalMinutes: 60, TimeoutMinutes: 10, MaxRetries: 6, MaxDisplayPerGroup: 25, DefaultReasoningEffort: "high"},
		{DefaultIntervalMinutes: 60, TimeoutMinutes: 10, MaxRetries: 1, MaxDisplayPerGroup: 101, DefaultReasoningEffort: "high"},
		{DefaultIntervalMinutes: 60, TimeoutMinutes: 10, MaxRetries: 1, MaxDisplayPerGroup: 25, DefaultReasoningEffort: "turbo"},
	} {
		require.Error(t, broken.Validate(), "%+v", broken)
	}

	repaired := DegradationConfig{DefaultIntervalMinutes: 1, TimeoutMinutes: 0, MaxRetries: -1, MaxDisplayPerGroup: 500, DefaultReasoningEffort: " XHIGH ", DefaultPromptID: -3}.normalized()
	require.Equal(t, DegradationDefaultIntervalMinutes, repaired.DefaultIntervalMinutes)
	require.Equal(t, DegradationDefaultTimeoutMinutes, repaired.TimeoutMinutes)
	require.Equal(t, DegradationDefaultRetries, repaired.MaxRetries)
	require.Equal(t, DegradationDefaultDisplayPerGroup, repaired.MaxDisplayPerGroup)
	require.Equal(t, "xhigh", repaired.DefaultReasoningEffort)
	require.Zero(t, repaired.DefaultPromptID)
}

func TestDegradationServiceConfigRoundTrip(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()

	require.Equal(t, DefaultDegradationConfig(), env.svc.GetConfig(ctx))

	// Partially stored JSON keeps defaults for missing fields.
	env.settings.values[SettingKeyDegradationMonitorConfig] = `{"timeout_minutes":30}`
	cfg := env.svc.GetConfig(ctx)
	require.Equal(t, 30, cfg.TimeoutMinutes)
	require.Equal(t, DegradationDefaultIntervalMinutes, cfg.DefaultIntervalMinutes)

	env.settings.values[SettingKeyDegradationMonitorConfig] = `not json`
	require.Equal(t, DefaultDegradationConfig(), env.svc.GetConfig(ctx))

	_, err := env.svc.UpdateConfig(ctx, DegradationConfig{DefaultIntervalMinutes: 30, TimeoutMinutes: 5, MaxRetries: 2, MaxDisplayPerGroup: 40, DefaultReasoningEffort: "medium", DefaultPromptID: 999})
	require.ErrorIs(t, err, ErrDegradationPromptNotFound)

	prompt := env.repo.addPrompt("Clock", "draw a clock", 10)
	saved, err := env.svc.UpdateConfig(ctx, DegradationConfig{DefaultIntervalMinutes: 30, TimeoutMinutes: 5, MaxRetries: 2, MaxDisplayPerGroup: 40, DefaultReasoningEffort: " Medium ", DefaultPromptID: prompt.ID})
	require.NoError(t, err)
	require.Equal(t, "medium", saved.DefaultReasoningEffort)
	require.Equal(t, saved, env.svc.GetConfig(ctx))
	require.Equal(t, 1, env.scheduler.kicks)

	var stored DegradationConfig
	require.NoError(t, json.Unmarshal([]byte(env.settings.values[SettingKeyDegradationMonitorConfig]), &stored))
	require.Equal(t, saved, stored)

	// Deleting the default preset resets the default.
	require.NoError(t, env.svc.DeletePrompt(ctx, prompt.ID))
	require.Zero(t, env.svc.GetConfig(ctx).DefaultPromptID)
}

func TestDegradationServicePromptValidation(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()

	created, err := env.svc.CreatePrompt(ctx, "  ", "  创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。\nsecond line")
	require.NoError(t, err)
	require.Equal(t, "创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的…", created.Name)

	_, err = env.svc.CreatePrompt(ctx, "name", "   ")
	require.ErrorIs(t, err, ErrDegradationInvalidPrompt)

	long := make([]byte, DegradationPromptMaxBytes+1)
	for i := range long {
		long[i] = 'a'
	}
	_, err = env.svc.CreatePrompt(ctx, "name", string(long))
	require.ErrorIs(t, err, ErrDegradationInvalidPrompt)

	tooLongName := ""
	for i := 0; i < DegradationNameMaxRunes+1; i++ {
		tooLongName += "鹈"
	}
	_, err = env.svc.CreatePrompt(ctx, tooLongName, "prompt")
	require.ErrorIs(t, err, ErrDegradationInvalidPromptName)

	updated, err := env.svc.UpdatePrompt(ctx, created.ID, "Pelican", " new prompt ")
	require.NoError(t, err)
	require.Equal(t, "Pelican", updated.Name)
	require.Equal(t, "new prompt", updated.Prompt)

	_, err = env.svc.UpdatePrompt(ctx, 404, "x", "y")
	require.ErrorIs(t, err, ErrDegradationPromptNotFound)
}

func TestDegradationServiceCreateMonitorDefaultsProtocolAndCreatesManagedKey(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()

	openAI, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: " gpt-5.5 ", Enabled: true}, 7)
	require.NoError(t, err)
	require.Equal(t, DegradationProtocolOpenAIResponses, openAI.Protocol)
	require.Equal(t, "gpt-5.5", openAI.Model)
	require.Equal(t, "OpenAI Pro", openAI.GroupName)
	key, err := env.keys.GetManagedMonitorKey(ctx, openAI.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, int64(1), *key.GroupID)
	require.Equal(t, int64(7), key.UserID)
	require.Equal(t, "OpenAI Pro", key.Name)

	claude, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{Name: "Opus", GroupID: 2, Model: "claude-opus", ReasoningEffort: "MAX", IntervalMinutes: 30, Enabled: true}, 7)
	require.NoError(t, err)
	require.Equal(t, DegradationProtocolAnthropic, claude.Protocol)
	require.Equal(t, "max", claude.ReasoningEffort)
	require.Equal(t, 30, claude.IntervalMinutes)

	chat, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 2, Model: "claude", Protocol: DegradationProtocolOpenAIChat, Enabled: true}, 7)
	require.NoError(t, err)
	require.Equal(t, DegradationProtocolOpenAIChat, chat.Protocol)
	require.Equal(t, 3, env.scheduler.kicks)
}

func TestDegradationServiceCreateMonitorRejectsInvalidInput(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	cases := []struct {
		input DegradationMonitorInput
		err   error
	}{
		{DegradationMonitorInput{GroupID: 0, Model: "m"}, ErrDegradationMissingGroup},
		{DegradationMonitorInput{GroupID: 3, Model: "m"}, ErrDegradationGroupInactive},
		{DegradationMonitorInput{GroupID: 4, Model: "m"}, ErrDegradationGroupPlatform},
		{DegradationMonitorInput{GroupID: 1, Model: " "}, ErrDegradationInvalidModel},
		{DegradationMonitorInput{GroupID: 1, Model: "m", ReasoningEffort: "turbo"}, ErrDegradationInvalidEffort},
		{DegradationMonitorInput{GroupID: 1, Model: "m", Protocol: "gemini"}, ErrDegradationInvalidProtocol},
		{DegradationMonitorInput{GroupID: 1, Model: "m", IntervalMinutes: 4}, ErrDegradationInvalidInterval},
		{DegradationMonitorInput{GroupID: 1, Model: "m", IntervalMinutes: 10081}, ErrDegradationInvalidInterval},
	}
	missingPrompt := int64(404)
	cases = append(cases, struct {
		input DegradationMonitorInput
		err   error
	}{DegradationMonitorInput{GroupID: 1, Model: "m", PromptID: &missingPrompt}, ErrDegradationPromptNotFound})
	for _, tc := range cases {
		_, err := env.svc.CreateMonitor(ctx, tc.input, 7)
		require.ErrorIs(t, err, tc.err, "%+v", tc.input)
	}
	require.Empty(t, env.keys.keys, "rejected monitors must not leave managed keys behind")

	unavailable := newDegradationTestEnv(true, "")
	unavailable.svc.SetManagedGateway("", nil)
	_, err := unavailable.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "m"}, 7)
	require.ErrorIs(t, err, ErrDegradationGatewayUnavailable)
}

func TestDegradationServiceUpdateRotatesKeyOnGroupChangeAndDeleteCleansUp(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	originalKey := monitor.APIKeyID

	sameGroup, err := env.svc.UpdateMonitor(ctx, monitor.ID, DegradationMonitorInput{GroupID: 1, Model: "gpt-mini", Enabled: false})
	require.NoError(t, err)
	require.Equal(t, originalKey, sameGroup.APIKeyID)
	require.False(t, sameGroup.Enabled)
	require.Empty(t, env.keys.deleted)

	moved, err := env.svc.UpdateMonitor(ctx, monitor.ID, DegradationMonitorInput{GroupID: 2, Model: "claude", Protocol: DegradationProtocolAnthropic, Enabled: true})
	require.NoError(t, err)
	require.NotEqual(t, originalKey, moved.APIKeyID)
	require.Equal(t, []int64{originalKey}, env.keys.deleted)
	newKey, err := env.keys.GetManagedMonitorKey(ctx, moved.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, int64(2), *newKey.GroupID)

	require.NoError(t, env.svc.DeleteMonitor(ctx, monitor.ID))
	require.Equal(t, []int64{originalKey, moved.APIKeyID}, env.keys.deleted)
	_, err = env.svc.GetMonitor(ctx, monitor.ID)
	require.ErrorIs(t, err, ErrDegradationMonitorNotFound)
	require.ErrorIs(t, env.svc.DeleteMonitor(ctx, monitor.ID), ErrDegradationMonitorNotFound)
}

func TestDegradationServiceResolvePromptOrder(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	cfg := DefaultDegradationConfig()
	monitor := &DegradationMonitor{}

	id, name, prompt := env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Nil(t, id)
	require.Equal(t, DegradationPelicanPromptName, name)
	require.Equal(t, DegradationPelicanPrompt, prompt)

	first := env.repo.addPrompt("first", "first prompt", 10)
	second := env.repo.addPrompt("second", "second prompt", 20)
	third := env.repo.addPrompt("third", "third prompt", 30)

	id, _, _ = env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Equal(t, first.ID, *id)

	cfg.DefaultPromptID = second.ID
	id, _, _ = env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Equal(t, second.ID, *id)

	monitor.PromptID = &third.ID
	id, name, prompt = env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Equal(t, third.ID, *id)
	require.Equal(t, "third", name)
	require.Equal(t, "third prompt", prompt)

	missing := int64(999)
	monitor.PromptID = &missing
	id, _, _ = env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Equal(t, second.ID, *id, "a deleted preset falls back to the default")

	monitor.RandomPrompt = true
	env.svc.randomIndex = func(n int) int {
		require.Equal(t, 3, n)
		return 2
	}
	id, _, _ = env.svc.resolvePrompt(ctx, monitor, cfg)
	require.Equal(t, third.ID, *id)
}

func TestDegradationServiceDueMonitorsAndClaim(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	now := time.Now()

	fresh, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "a", Enabled: true}, 7)
	require.NoError(t, err)
	recent, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "b", IntervalMinutes: 30, Enabled: true}, 7)
	require.NoError(t, err)
	overdue, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "c", Enabled: true}, 7)
	require.NoError(t, err)
	disabled, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "d", Enabled: false}, 7)
	require.NoError(t, err)
	paused, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "e", Enabled: true}, 7)
	require.NoError(t, err)

	require.NoError(t, env.repo.MarkMonitorRun(ctx, recent.ID, now.Add(-10*time.Minute)))
	require.NoError(t, env.repo.MarkMonitorRun(ctx, overdue.ID, now.Add(-61*time.Minute)))
	env.groups.groups[1].Status = StatusActive
	env.repo.monitors[paused.ID].GroupID = 3 // inactive group: skipped

	due, err := env.svc.DueMonitorIDs(ctx, now)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{fresh.ID, overdue.ID}, due)
	require.NotContains(t, due, disabled.ID)

	claimed, err := env.svc.ClaimScheduledRun(ctx, overdue.ID, now)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = env.svc.ClaimScheduledRun(ctx, overdue.ID, now)
	require.NoError(t, err)
	require.False(t, claimed, "a second scheduler must not claim the same slot")
}

func TestDegradationServiceRunNowRequiresFeatureAndScheduler(t *testing.T) {
	env := newDegradationTestEnv(false, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	require.ErrorIs(t, env.svc.RunNow(ctx, monitor.ID), ErrDegradationDisabled)

	env.settings.values[SettingKeyDegradationMonitorEnabled] = "true"
	require.NoError(t, env.svc.RunNow(ctx, monitor.ID))
	require.Equal(t, []int64{monitor.ID}, env.scheduler.triggers)

	env.scheduler.err = ErrDegradationRunInProgress
	require.ErrorIs(t, env.svc.RunNow(ctx, monitor.ID), ErrDegradationRunInProgress)
	require.ErrorIs(t, env.svc.RunNow(ctx, 404), ErrDegradationMonitorNotFound)
}

func TestDegradationServiceOverviewHidesDisabledAndFeatureOff(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	visible, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{Name: "Shown", GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	hidden, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 2, Model: "claude", Enabled: false}, 7)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		run := &DegradationRun{MonitorID: visible.ID, Status: DegradationRunStatusSuccess, Output: "<svg></svg>", StartedAt: time.Now()}
		require.NoError(t, env.repo.CreateRun(ctx, run))
	}
	hiddenRun := &DegradationRun{MonitorID: hidden.ID, Status: DegradationRunStatusSuccess, Output: "<svg/>", StartedAt: time.Now()}
	require.NoError(t, env.repo.CreateRun(ctx, hiddenRun))

	_, err = env.svc.UpdateConfig(ctx, DegradationConfig{DefaultIntervalMinutes: 60, TimeoutMinutes: 10, MaxRetries: 1, MaxDisplayPerGroup: 2, DefaultReasoningEffort: "low"})
	require.NoError(t, err)

	overview, err := env.svc.Overview(ctx)
	require.NoError(t, err)
	require.True(t, overview.Enabled)
	require.Equal(t, 2, overview.DisplayLimit)
	require.Len(t, overview.Monitors, 1)
	require.Equal(t, "Shown", overview.Monitors[0].Name)
	require.Equal(t, "low", overview.Monitors[0].EffectiveReasoningEffort)
	require.Equal(t, PlatformOpenAI, overview.Monitors[0].Platform)
	require.Len(t, overview.Monitors[0].Runs, 2)
	require.Empty(t, overview.Monitors[0].Runs[0].Output, "list views never carry document bodies")

	_, err = env.svc.GetRunForUser(ctx, hiddenRun.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
	detail, err := env.svc.GetRunForUser(ctx, overview.Monitors[0].Runs[0].ID)
	require.NoError(t, err)
	require.Equal(t, "<svg></svg>", detail.Output)

	env.settings.values[SettingKeyDegradationMonitorEnabled] = "false"
	overview, err = env.svc.Overview(ctx)
	require.NoError(t, err)
	require.False(t, overview.Enabled)
	require.Empty(t, overview.Monitors)
	_, err = env.svc.GetRunForUser(ctx, detail.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
}

func TestDegradationServiceOverviewWaitsForFinishedOutput(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{Name: "Shown", GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	started := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Minute)
	done := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusSuccess, Output: "<svg></svg>", StartedAt: started, FinishedAt: &finished}
	require.NoError(t, env.repo.CreateRun(ctx, done))
	failed := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusFailed, ErrorMessage: "upstream HTTP 502", StartedAt: started.Add(time.Hour)}
	require.NoError(t, env.repo.CreateRun(ctx, failed))
	running := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusRunning, StartedAt: started.Add(2 * time.Hour)}
	require.NoError(t, env.repo.CreateRun(ctx, running))

	overview, err := env.svc.Overview(ctx)
	require.NoError(t, err)
	require.Len(t, overview.Monitors, 1)
	runs := overview.Monitors[0].Runs
	require.Len(t, runs, 2, "a check that is still generating is not listed")
	require.Equal(t, []int64{failed.ID, done.ID}, []int64{runs[0].ID, runs[1].ID})
	require.NotNil(t, overview.Monitors[0].LastRunAt)
	require.True(t, overview.Monitors[0].LastRunAt.Equal(failed.StartedAt), "the latest check is the newest finished one")

	_, err = env.svc.GetRunForUser(ctx, running.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
	detail, err := env.svc.GetRunForUser(ctx, done.ID)
	require.NoError(t, err)
	require.Equal(t, "<svg></svg>", detail.Output)
}

func TestDegradationServiceOverviewWithoutFinishedResults(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{Name: "First", GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	require.NoError(t, env.repo.CreateRun(ctx, &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusRunning, StartedAt: time.Now()}))

	overview, err := env.svc.Overview(ctx)
	require.NoError(t, err)
	require.Len(t, overview.Monitors, 1)
	require.Empty(t, overview.Monitors[0].Runs)
	require.Nil(t, overview.Monitors[0].LastRunAt, "an unfinished first check does not count as checked")
}

func TestDegradationServiceGetRunForUserEnforcesDisplayWindow(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	other, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 2, Model: "claude", Enabled: true}, 7)
	require.NoError(t, err)
	addRun := func(monitorID int64, status string) *DegradationRun {
		run := &DegradationRun{MonitorID: monitorID, Status: status, Prompt: "gallery prompt", Output: "<svg/>", StartedAt: time.Now()}
		require.NoError(t, env.repo.CreateRun(ctx, run))
		return run
	}
	archived := addRun(monitor.ID, DegradationRunStatusSuccess)
	boundary := addRun(monitor.ID, DegradationRunStatusSuccess)
	latest := addRun(monitor.ID, DegradationRunStatusFailed)
	running := addRun(monitor.ID, DegradationRunStatusRunning)
	otherRun := addRun(other.ID, DegradationRunStatusSuccess)
	cfg := DefaultDegradationConfig()
	cfg.MaxDisplayPerGroup = 2
	_, err = env.svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)

	for _, visible := range []*DegradationRun{boundary, latest, otherRun} {
		run, err := env.svc.GetRunForUser(ctx, visible.ID)
		require.NoError(t, err, "every displayed run remains readable, including failed runs")
		require.Equal(t, visible.ID, run.ID)
	}
	for _, id := range []int64{archived.ID, running.ID, 99999} {
		run, err := env.svc.GetRunForUser(ctx, id)
		require.ErrorIs(t, err, ErrDegradationRunNotFound)
		require.Nil(t, run)
	}
	adminRun, err := env.svc.GetRun(ctx, archived.ID)
	require.NoError(t, err, "administrator history reads are not restricted to the gallery window")
	require.Equal(t, archived.Output, adminRun.Output)

	// Newly finished checks and a smaller display limit revoke older detail access.
	fresh := addRun(monitor.ID, DegradationRunStatusSuccess)
	_, err = env.svc.GetRunForUser(ctx, boundary.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
	cfg.MaxDisplayPerGroup = 1
	_, err = env.svc.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	_, err = env.svc.GetRunForUser(ctx, latest.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
	_, err = env.svc.GetRunForUser(ctx, fresh.ID)
	require.NoError(t, err)

	// Deleting a group also removes the latest result from the public scope.
	delete(env.groups.groups, 1)
	_, err = env.svc.GetRunForUser(ctx, fresh.ID)
	require.ErrorIs(t, err, ErrDegradationRunNotFound)
}

type degradationVisibilityErrorRepo struct {
	DegradationMonitorRepository
	err error
}

func (r degradationVisibilityErrorRepo) ListRecentFinishedRuns(context.Context, []int64, int) (map[int64][]*DegradationRun, error) {
	return nil, r.err
}

func TestDegradationServiceGetRunForUserFailsClosedOnVisibilityError(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	run := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusSuccess, Prompt: "private", Output: "private"}
	require.NoError(t, env.repo.CreateRun(ctx, run))
	visibilityError := errors.New("visibility query failed")
	env.svc.repo = degradationVisibilityErrorRepo{DegradationMonitorRepository: env.repo, err: visibilityError}

	detail, err := env.svc.GetRunForUser(ctx, run.ID)
	require.ErrorIs(t, err, visibilityError)
	require.Nil(t, detail)
}

func TestDegradationServiceStorageStatsAndRelease(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)
	require.NoError(t, env.repo.CreateRun(ctx, &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusSuccess, Output: "12345", Prompt: "abc"}))
	require.NoError(t, env.repo.CreateRun(ctx, &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusRunning, Prompt: "ab"}))

	stats, err := env.svc.StorageStats(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Count)
	require.Equal(t, int64(10), stats.Bytes)

	deleted, err := env.svc.ReleaseStorage(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted, "an in-progress check keeps its row")

	_, err = env.svc.StorageStats(ctx, 404)
	require.ErrorIs(t, err, ErrDegradationMonitorNotFound)
}

func TestDegradationRunHelpers(t *testing.T) {
	run := &DegradationRun{InputTokens: 300, CacheReadTokens: 600, CacheCreationTokens: 100, OutputTokens: 50}
	require.Equal(t, int64(1050), run.TotalTokens())
	require.InDelta(t, 0.6, *run.CacheRate(), 1e-9)
	require.Nil(t, (&DegradationRun{}).CacheRate())

	require.Equal(t, "low", degradationEffortForProtocol(DegradationProtocolAnthropic, "minimal"))
	require.Equal(t, "max", degradationEffortForProtocol(DegradationProtocolAnthropic, "max"))
	require.Equal(t, "xhigh", degradationEffortForProtocol(DegradationProtocolOpenAIResponses, "max"))
	require.Equal(t, "minimal", degradationEffortForProtocol(DegradationProtocolOpenAIChat, " Minimal "))

	require.Equal(t, DegradationProtocolAnthropic, DefaultDegradationProtocol(PlatformAnthropic))
	for _, platform := range []string{PlatformOpenAI, PlatformGemini, PlatformGrok, PlatformComposite} {
		require.Equal(t, DegradationProtocolOpenAIResponses, DefaultDegradationProtocol(platform))
		require.True(t, IsDegradationGroupPlatform(platform))
	}
	require.False(t, IsDegradationGroupPlatform(PlatformAntigravity))

	long := ""
	for len(long) < degradationErrorMaxBytes+10 {
		long += "错"
	}
	truncated := truncateDegradationError(long)
	require.LessOrEqual(t, len(truncated), degradationErrorMaxBytes+len("…"))
	require.True(t, len([]rune(truncated)) > 0)
	require.Equal(t, "short", truncateDegradationError("short"))
}
