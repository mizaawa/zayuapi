package admin

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorSimulationRequestDefaultsAndUpdates(t *testing.T) {
	var create channelMonitorCreateRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &create))
	require.False(t, create.SimulateRequests)
	require.NoError(t, json.Unmarshal([]byte(`{"simulate_requests":true}`), &create))
	require.True(t, create.SimulateRequests)

	for _, tt := range []struct {
		name    string
		payload string
		want    *bool
	}{
		{name: "omitted", payload: `{}`},
		{name: "enable", payload: `{"simulate_requests":true}`, want: monitorSimulationBool(true)},
		{name: "disable", payload: `{"simulate_requests":false}`, want: monitorSimulationBool(false)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var update channelMonitorUpdateRequest
			require.NoError(t, json.Unmarshal([]byte(tt.payload), &update))
			require.Equal(t, tt.want, update.SimulateRequests)
		})
	}
}

func TestBuildListItemResponseSimulationOverridesHistory(t *testing.T) {
	latency := 123
	for _, summary := range []service.MonitorStatusSummary{
		{},
		{
			PrimaryStatus:    service.MonitorStatusFailed,
			PrimaryLatencyMs: &latency,
			Availability7d:   25,
			ExtraModels: []service.ExtraModelStatus{
				{Model: "secondary", Status: service.MonitorStatusError, LatencyMs: &latency},
			},
		},
	} {
		monitor := &service.ChannelMonitor{
			ID:               1,
			PrimaryModel:     "primary",
			ExtraModels:      []string{"secondary"},
			SimulateRequests: true,
		}
		resp := buildListItemResponse(monitor, summary)
		require.True(t, resp.SimulateRequests)
		require.Equal(t, service.MonitorStatusOperational, resp.PrimaryStatus)
		require.NotNil(t, resp.PrimaryLatencyMs)
		require.Zero(t, *resp.PrimaryLatencyMs)
		require.Equal(t, float64(100), resp.Availability7d)
		require.Len(t, resp.ExtraModelsStatus, 1)
		require.Equal(t, "secondary", resp.ExtraModelsStatus[0].Model)
		require.Equal(t, service.MonitorStatusOperational, resp.ExtraModelsStatus[0].Status)
		require.NotNil(t, resp.ExtraModelsStatus[0].LatencyMs)
		require.Zero(t, *resp.ExtraModelsStatus[0].LatencyMs)

		payload, err := json.Marshal(resp)
		require.NoError(t, err)
		require.Contains(t, string(payload), `"simulate_requests":true`)
	}
}

func TestBuildListItemResponseRealMonitorKeepsHistory(t *testing.T) {
	latency := 123
	summary := service.MonitorStatusSummary{
		PrimaryStatus:    service.MonitorStatusFailed,
		PrimaryLatencyMs: &latency,
		Availability7d:   25,
	}
	resp := buildListItemResponse(&service.ChannelMonitor{ID: 1}, summary)
	require.False(t, resp.SimulateRequests)
	require.Equal(t, service.MonitorStatusFailed, resp.PrimaryStatus)
	require.Equal(t, &latency, resp.PrimaryLatencyMs)
	require.Equal(t, float64(25), resp.Availability7d)
}

func monitorSimulationBool(value bool) *bool {
	return &value
}
