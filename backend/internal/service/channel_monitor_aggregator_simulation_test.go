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
			require.NotNil(t, view.PrimaryLatencyMs)
			require.Zero(t, *view.PrimaryLatencyMs)
			require.NotNil(t, view.PrimaryPingLatencyMs)
			require.Zero(t, *view.PrimaryPingLatencyMs)
			require.Equal(t, float64(100), view.Availability7d)
			require.Len(t, view.ExtraModels, 1)
			require.Equal(t, "secondary", view.ExtraModels[0].Model)
			require.Equal(t, MonitorStatusOperational, view.ExtraModels[0].Status)
			require.NotNil(t, view.ExtraModels[0].LatencyMs)
			require.Zero(t, *view.ExtraModels[0].LatencyMs)
			require.Equal(t, []UserMonitorTimelinePoint{{
				Status: MonitorStatusFailed, LatencyMs: &latency, PingLatencyMs: &latency, CheckedAt: checkedAt,
			}}, view.Timeline)
			require.Equal(t, MonitorStatusFailed, history[0].Status)
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
	require.NotNil(t, detail.LatestLatencyMs)
	require.Zero(t, *detail.LatestLatencyMs)
	require.Equal(t, float64(100), detail.Availability7d)
	require.Equal(t, float64(100), detail.Availability15d)
	require.Equal(t, float64(100), detail.Availability30d)
	require.NotNil(t, detail.AvgLatency7dMs)
	require.Zero(t, *detail.AvgLatency7dMs)
}
