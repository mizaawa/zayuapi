//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDegradationMonitorRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	// Everything runs in one rolled-back transaction so other suites see an untouched database.
	tx := testEntTx(t)
	client := tx.Client()
	repo := newDegradationMonitorRepositoryWithSQL(tx)
	suffix := time.Now().UnixNano()

	builtins, err := repo.ListPrompts(ctx)
	require.NoError(t, err)
	builtinKeys := map[string]bool{}
	for _, prompt := range builtins {
		builtinKeys[prompt.BuiltinKey] = true
	}
	for _, key := range []string{"pelican", "polarbear", "ultraman", "octopus", "giraffe", "mooncat", "clock", "solar", "balls", "gears", "city", "strokes"} {
		require.True(t, builtinKeys[key], "migration seeds the %s preset", key)
	}

	custom := &service.DegradationPrompt{Name: "custom", Prompt: "draw a custom animation"}
	require.NoError(t, repo.CreatePrompt(ctx, custom))
	require.Positive(t, custom.ID)
	require.Empty(t, custom.BuiltinKey)
	custom.Name = "renamed"
	require.NoError(t, repo.UpdatePrompt(ctx, custom))
	loadedPrompt, err := repo.GetPrompt(ctx, custom.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", loadedPrompt.Name)

	group := mustCreateGroup(t, client, &service.Group{Name: fmt.Sprintf("degradation-%d", suffix), Platform: service.PlatformOpenAI})
	monitor := &service.DegradationMonitor{
		GroupID: group.ID, Model: "gpt-5.5", Protocol: service.DegradationProtocolOpenAIResponses,
		PromptID: &custom.ID, Enabled: true, APIKeyID: 77, CreatedBy: 1, IntervalMinutes: 30,
	}
	require.NoError(t, repo.CreateMonitor(ctx, monitor))

	loaded, err := repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, group.Name, loaded.GroupName)
	require.Equal(t, service.PlatformOpenAI, loaded.GroupPlatform)
	require.Equal(t, service.StatusActive, loaded.GroupStatus)
	require.False(t, loaded.GroupDeleted)
	require.Equal(t, "renamed", loaded.PromptName)
	require.Equal(t, 30, loaded.IntervalMinutes)
	require.Nil(t, loaded.LastRunAt)

	now := time.Now().UTC().Truncate(time.Microsecond)
	claimed, err := repo.ClaimMonitorRun(ctx, monitor.ID, now, now.Add(-30*time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimMonitorRun(ctx, monitor.ID, now.Add(time.Second), now.Add(-30*time.Minute))
	require.NoError(t, err)
	require.False(t, claimed, "a fresh claim blocks concurrent schedulers")

	var runIDs []int64
	for i := 0; i < 3; i++ {
		run := &service.DegradationRun{
			MonitorID: monitor.ID, Status: service.DegradationRunStatusRunning, TriggerSource: service.DegradationTriggerSchedule,
			Model: "gpt-5.5", Protocol: service.DegradationProtocolOpenAIResponses, ReasoningEffort: "high",
			PromptID: &custom.ID, PromptName: "renamed", Prompt: "draw", StartedAt: now,
		}
		require.NoError(t, repo.CreateRun(ctx, run))
		firstToken, duration := 1200+i, 5000+i
		finished := now.Add(time.Minute)
		run.Status = service.DegradationRunStatusSuccess
		run.Output = fmt.Sprintf("<svg>%d</svg>", i)
		run.ResponseModel = "gpt-5.5-2026"
		run.Attempts = 1
		run.FirstTokenMs = &firstToken
		run.DurationMs = &duration
		run.InputTokens = 10
		run.OutputTokens = 20
		run.FinishedAt = &finished
		require.NoError(t, repo.FinishRun(ctx, run))
		runIDs = append(runIDs, run.ID)
	}
	cost := 0.042
	require.NoError(t, repo.UpdateRunUsage(ctx, &service.DegradationRun{ID: runIDs[2], InputTokens: 11, OutputTokens: 22, CacheReadTokens: 33, CacheCreationTokens: 4, Cost: &cost, UsageSynced: true}))

	recent, err := repo.ListRecentRuns(ctx, []int64{monitor.ID}, 2)
	require.NoError(t, err)
	require.Len(t, recent[monitor.ID], 2)
	require.Equal(t, runIDs[2], recent[monitor.ID][0].ID)
	require.Empty(t, recent[monitor.ID][0].Output, "list reads leave the document out")
	require.Equal(t, int64(len("<svg>2</svg>")), recent[monitor.ID][0].OutputBytes)
	require.True(t, recent[monitor.ID][0].UsageSynced)
	require.InDelta(t, cost, *recent[monitor.ID][0].Cost, 1e-9)

	detail, err := repo.GetRun(ctx, runIDs[2])
	require.NoError(t, err)
	require.Equal(t, "<svg>2</svg>", detail.Output)
	require.Equal(t, "draw", detail.Prompt)
	require.Equal(t, 1202, *detail.FirstTokenMs)
	require.Equal(t, int64(33), detail.CacheReadTokens)
	require.NotNil(t, detail.FinishedAt)
	_, err = repo.GetRun(ctx, -1)
	require.ErrorIs(t, err, service.ErrDegradationRunNotFound)

	stats, err := repo.MonitorStats(ctx, []int64{monitor.ID})
	require.NoError(t, err)
	require.Equal(t, int64(3), stats[monitor.ID].StoredRuns)
	require.Equal(t, service.DegradationRunStatusSuccess, stats[monitor.ID].LastRunStatus)
	storage, err := repo.StorageStats(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, int64(3), storage.Count)
	require.Equal(t, int64(3*len("<svg>0</svg>")+3*len("draw")), storage.Bytes)

	stale := &service.DegradationRun{MonitorID: monitor.ID, Status: service.DegradationRunStatusRunning, StartedAt: now.Add(-2 * time.Hour)}
	require.NoError(t, repo.CreateRun(ctx, stale))
	failed, err := repo.FailStaleRuns(ctx, now.Add(-time.Hour), "interrupted")
	require.NoError(t, err)
	require.GreaterOrEqual(t, failed, int64(1))
	staleRun, err := repo.GetRun(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, service.DegradationRunStatusFailed, staleRun.Status)

	pruned, err := repo.PruneRuns(ctx, monitor.ID, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), pruned)
	running := &service.DegradationRun{MonitorID: monitor.ID, Status: service.DegradationRunStatusRunning, StartedAt: now}
	require.NoError(t, repo.CreateRun(ctx, running))
	deleted, err := repo.DeleteRuns(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted, "an in-progress check is not released")

	require.NoError(t, repo.UpdateMonitorSortOrders(ctx, []service.DegradationSortOrderUpdate{{ID: monitor.ID, SortOrder: 999}}))
	require.NoError(t, repo.UpdateMonitorAPIKey(ctx, monitor.ID, 88))
	loaded, err = repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, 999, loaded.SortOrder)
	require.Equal(t, int64(88), loaded.APIKeyID)
	require.NotNil(t, loaded.LastRunAt)

	require.NoError(t, repo.DeletePrompt(ctx, custom.ID))
	loaded, err = repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.Nil(t, loaded.PromptID, "deleting a preset unlinks monitors")
	require.ErrorIs(t, repo.DeletePrompt(ctx, custom.ID), service.ErrDegradationPromptNotFound)

	require.NoError(t, repo.DeleteMonitor(ctx, monitor.ID))
	_, err = repo.GetMonitor(ctx, monitor.ID)
	require.ErrorIs(t, err, service.ErrDegradationMonitorNotFound)
	_, err = repo.GetRun(ctx, running.ID)
	require.ErrorIs(t, err, service.ErrDegradationRunNotFound, "results are removed with their monitor")
}

func TestDegradationMonitorRepositorySumsManagedKeyUsage(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newDegradationMonitorRepositoryWithSQL(tx)
	usageRepo := newUsageLogRepositoryWithSQL(client, tx)
	suffix := time.Now().UnixNano()

	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("degradation-usage-%d@test.com", suffix), Role: service.RoleAdmin})
	account := mustCreateAccount(t, client, &service.Account{Name: fmt.Sprintf("degradation-usage-%d", suffix)})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: fmt.Sprintf("sk-degradation-%d", suffix), Name: "probe", Purpose: service.APIKeyPurposeChannelMonitor})
	now := time.Now().UTC()
	endpoint := "/v1/responses"
	multiplier := 2.0
	for i, createdAt := range []time.Time{now.Add(-10 * time.Second), now, now.Add(-2 * time.Hour)} {
		log := &service.UsageLog{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: fmt.Sprintf("degradation-usage-%d-%d", suffix, i),
			Model: "gpt-5.5", InputTokens: 100, OutputTokens: 200, CacheReadTokens: 50, CacheCreationTokens: 5,
			TotalCost: 0.25, ActualCost: 0, AccountRateMultiplier: &multiplier,
			InboundEndpoint: &endpoint, UpstreamEndpoint: &endpoint, CreatedAt: createdAt,
		}
		_, err := usageRepo.Create(ctx, log)
		require.NoError(t, err)
	}

	summary, err := repo.SumUsage(ctx, key.ID, now.Add(-time.Minute), now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(2), summary.Requests)
	require.Equal(t, int64(200), summary.InputTokens)
	require.Equal(t, int64(400), summary.OutputTokens)
	require.Equal(t, int64(100), summary.CacheReadTokens)
	require.Equal(t, int64(10), summary.CacheCreationTokens)
	require.InDelta(t, 1.0, summary.Cost, 1e-9, "upstream account cost, not the zero user charge")
}
