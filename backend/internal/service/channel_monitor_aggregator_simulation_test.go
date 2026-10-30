package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildUserViewSimulationOverridesCurrentStatusAndPreservesTimeline(t *testing.T) {
	latency := 123
	checkedAt := time.Now().Add(-time.Hour)
	history := []*ChannelMonitorHistoryEntry{{
		Model:         "primary",
		Status:        MonitorStatusFailed,
		LatencyMs:     &latency,
		PingLatencyMs: &latency,
		CheckedAt:     checkedAt,
	}}
	for _, tt := range []struct {
		name    string
		summary MonitorStatusSummary
		latest  *ChannelMonitorLatest
	}{
		{name: "no history"},
		{
			name: "failed history",
			summary: MonitorStatusSummary{
				PrimaryStatus:    MonitorStatusFailed,
				PrimaryLatencyMs: &latency,
				Availability7d:   25,
				ExtraModels:      []ExtraModelStatus{{Model: "secondary", Status: MonitorStatusError}},
			},
			latest: &ChannelMonitorLatest{Model: "primary", Status: MonitorStatusFailed, PingLatencyMs: &latency},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &ChannelMonitor{ID: 7, PrimaryModel: "primary", ExtraModels: []string{"secondary"}, SimulateRequests: true}
			view := buildUserViewFromSummary(monitor, tt.summary, tt.latest, history)
			require.Equal(t, MonitorStatusOperational, view.PrimaryStatus)
			requireSimulatedFirstTokenLatency(t, view.PrimaryLatencyMs)
			require.NotNil(t, view.PrimaryPingLatencyMs)
			require.Zero(t, *view.PrimaryPingLatencyMs)
			require.Equal(t, float64(100), view.Availability7d)
			require.Len(t, view.ExtraModels, 1)
			require.Equal(t, "secondary", view.ExtraModels[0].Model)
			require.Equal(t, MonitorStatusOperational, view.ExtraModels[0].Status)
			requireSimulatedFirstTokenLatency(t, view.ExtraModels[0].LatencyMs)
			require.Equal(t, []UserMonitorTimelinePoint{{
				Status: MonitorStatusFailed, LatencyMs: &latency, PingLatencyMs: &latency, CheckedAt: checkedAt,
			}}, view.Timeline)
			require.Equal(t, MonitorStatusFailed, history[0].Status)

			again := buildUserViewFromSummary(monitor, tt.summary, tt.latest, history)
			require.Equal(t, *view.PrimaryLatencyMs, *again.PrimaryLatencyMs)
			require.Equal(t, *view.ExtraModels[0].LatencyMs, *again.ExtraModels[0].LatencyMs)
		})
	}
}

func TestMergeModelDetailsSimulationOverridesAllAvailabilityWindows(t *testing.T) {
	latency := 123
	monitor := &ChannelMonitor{PrimaryModel: "primary", ExtraModels: []string{"secondary"}, SimulateRequests: true}
	latest := []*ChannelMonitorLatest{
		{Model: "primary", Status: MonitorStatusFailed, LatencyMs: &latency},
		{Model: "secondary", Status: MonitorStatusError, LatencyMs: &latency},
	}
	availability := map[int]map[string]*ChannelMonitorAvailability{}
	for _, window := range []int{monitorAvailability7Days, monitorAvailability15Days, monitorAvailability30Days} {
		availability[window] = map[string]*ChannelMonitorAvailability{
			"primary":   {Model: "primary", AvailabilityPct: 25, AvgLatencyMs: &latency},
			"secondary": {Model: "secondary", AvailabilityPct: 50, AvgLatencyMs: &latency},
		}
	}

	details := mergeModelDetails(monitor, latest, availability)
	require.Len(t, details, 2)
	for _, detail := range details {
		requireSimulatedModelDetail(t, detail)
	}
	require.Equal(t, MonitorStatusFailed, latest[0].Status)
	require.Equal(t, float64(25), availability[monitorAvailability7Days]["primary"].AvailabilityPct)
}

type simulatedMonitorDetailRepo struct {
	ChannelMonitorRepository
	monitor *ChannelMonitor
}

func (r *simulatedMonitorDetailRepo) GetByID(context.Context, int64) (*ChannelMonitor, error) {
	return r.monitor, nil
}

func TestGetUserDetailSimulationNeedsNoHistoryQueries(t *testing.T) {
	monitor := &ChannelMonitor{
		ID: 7, Name: "simulated", PrimaryModel: "primary", ExtraModels: []string{"secondary"},
		Enabled: true, SimulateRequests: true,
	}
	svc := NewChannelMonitorService(&simulatedMonitorDetailRepo{monitor: monitor}, nil)
	detail, err := svc.GetUserDetail(context.Background(), monitor.ID)
	require.NoError(t, err)
	require.Equal(t, monitor.ID, detail.ID)
	require.Len(t, detail.Models, 2)
	require.Equal(t, "primary", detail.Models[0].Model)
	require.Equal(t, "secondary", detail.Models[1].Model)
	for _, model := range detail.Models {
		requireSimulatedModelDetail(t, model)
	}

	monitor.Enabled = false
	_, err = svc.GetUserDetail(context.Background(), monitor.ID)
	require.ErrorIs(t, err, ErrChannelMonitorNotFound)
}

func requireSimulatedModelDetail(t *testing.T, detail ModelDetail) {
	t.Helper()
	require.Equal(t, MonitorStatusOperational, detail.LatestStatus)
	requireSimulatedFirstTokenLatency(t, detail.LatestLatencyMs)
	require.Equal(t, float64(100), detail.Availability7d)
	require.Equal(t, float64(100), detail.Availability15d)
	require.Equal(t, float64(100), detail.Availability30d)
	requireSimulatedFirstTokenLatency(t, detail.AvgLatency7dMs)
}

func requireSimulatedFirstTokenLatency(t *testing.T, latencyMs *int) {
	t.Helper()
	require.NotNil(t, latencyMs)
	require.GreaterOrEqual(t, *latencyMs, 300)
	require.LessOrEqual(t, *latencyMs, 3000)
}

func TestBuildUserViewSimulationGivesSimulatedChecksAFirstTokenLatency(t *testing.T) {
	zero := 0
	measured := 1530
	newest := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	history := []*ChannelMonitorHistoryEntry{
		{Model: "primary", Status: MonitorStatusOperational, LatencyMs: &zero, PingLatencyMs: &zero, CheckedAt: newest},
		{Model: "primary", Status: MonitorStatusOperational, CheckedAt: newest.Add(-time.Minute)},
		{Model: "primary", Status: MonitorStatusOperational, LatencyMs: &measured, CheckedAt: newest.Add(-2 * time.Minute)},
		{Model: "primary", Status: MonitorStatusFailed, CheckedAt: newest.Add(-3 * time.Minute)},
	}
	latest := &ChannelMonitorLatest{Model: "primary", Status: MonitorStatusOperational, LatencyMs: &zero, PingLatencyMs: &zero, CheckedAt: newest}
	monitor := &ChannelMonitor{ID: 42, PrimaryModel: "primary", SimulateRequests: true}

	view := buildUserViewFromSummary(monitor, MonitorStatusSummary{}, latest, history)

	require.Len(t, view.Timeline, 4)
	requireSimulatedFirstTokenLatency(t, view.Timeline[0].LatencyMs)
	requireSimulatedFirstTokenLatency(t, view.Timeline[1].LatencyMs)
	require.Equal(t, *view.PrimaryLatencyMs, *view.Timeline[0].LatencyMs, "the row and the newest bar describe the same check")
	require.Equal(t, &measured, view.Timeline[2].LatencyMs, "real checks keep what they measured")
	require.Nil(t, view.Timeline[3].LatencyMs, "failures keep their own values")
	require.Zero(t, *history[0].LatencyMs, "stored history is never modified")
	require.Nil(t, history[1].LatencyMs)

	again := buildUserViewFromSummary(monitor, MonitorStatusSummary{}, latest, history)
	require.Equal(t, *view.Timeline[1].LatencyMs, *again.Timeline[1].LatencyMs)
}

func TestBuildUserViewSimulationWithoutChecksUsesTheTypicalLatency(t *testing.T) {
	monitor := &ChannelMonitor{ID: 9, PrimaryModel: "primary", SimulateRequests: true}
	view := buildUserViewFromSummary(monitor, MonitorStatusSummary{}, nil, nil)
	require.Equal(t, simulatedFirstTokenBaseMs(9, "primary"), *view.PrimaryLatencyMs)
	require.Empty(t, view.Timeline)
}

func TestBuildUserViewRealMonitorKeepsStoredLatencies(t *testing.T) {
	zero := 0
	checkedAt := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	history := []*ChannelMonitorHistoryEntry{{Model: "primary", Status: MonitorStatusOperational, LatencyMs: &zero, CheckedAt: checkedAt}}
	summary := MonitorStatusSummary{PrimaryStatus: MonitorStatusOperational, PrimaryLatencyMs: &zero}
	view := buildUserViewFromSummary(&ChannelMonitor{ID: 3, PrimaryModel: "primary"}, summary, nil, history)
	require.Zero(t, *view.PrimaryLatencyMs)
	require.Zero(t, *view.Timeline[0].LatencyMs)
}

func TestSimulatedFirstTokenLatencyStaysInRangeAndVaries(t *testing.T) {
	start := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	lowest, highest := 3000, 300
	buckets := map[int]bool{}
	for id := int64(1); id <= 40; id++ {
		for _, model := range []string{"gpt-5-codex", "claude-sonnet-4-5", ""} {
			base := simulatedFirstTokenBaseMs(id, model)
			require.GreaterOrEqual(t, base, 300)
			require.LessOrEqual(t, base, 3000)
			require.Equal(t, base, simulatedFirstTokenLatencyMs(id, model, time.Time{}))
			for i := 0; i < 50; i++ {
				at := start.Add(time.Duration(i) * time.Minute)
				ms := simulatedFirstTokenLatencyMs(id, model, at)
				require.GreaterOrEqual(t, ms, 300)
				require.LessOrEqual(t, ms, 3000)
				require.Equal(t, ms, simulatedFirstTokenLatencyMs(id, model, at))
				lowest = min(lowest, ms)
				highest = max(highest, ms)
				buckets[ms/100] = true
			}
		}
	}
	require.Less(t, lowest, 600)
	require.Greater(t, highest, 2600)
	require.Greater(t, len(buckets), 20)
}
