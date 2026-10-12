package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUserViewFromSummaryIncludesBoundGroupMultiplier(t *testing.T) {
	rate := 0.1
	groupID := int64(42)
	monitor := &ChannelMonitor{
		ID:                  7,
		Name:                "OpenAI",
		Provider:            MonitorProviderOpenAI,
		GroupID:             &groupID,
		GroupName:           "public",
		GroupRateMultiplier: rate,
		PrimaryModel:        "gpt-4o-mini",
	}

	view := buildUserViewFromSummary(monitor, MonitorStatusSummary{}, nil, nil)

	require.NotNil(t, view.GroupRateMultiplier)
	require.Equal(t, rate, *view.GroupRateMultiplier)
}

func TestBuildUserViewFromSummaryLeavesUnboundGroupMultiplierNil(t *testing.T) {
	monitor := &ChannelMonitor{ID: 7, GroupName: "", PrimaryModel: "gpt-4o-mini"}

	view := buildUserViewFromSummary(monitor, MonitorStatusSummary{}, nil, nil)

	require.Nil(t, view.GroupRateMultiplier)
}

func TestBuildUserViewBlankMonitorNameFollowsRenamedGroup(t *testing.T) {
	m := &ChannelMonitor{ID: 7, GroupName: "Original group", PrimaryModel: "test"}
	require.Equal(t, "Original group", buildUserViewFromSummary(m, MonitorStatusSummary{}, nil, nil).Name)
	m.GroupName = "Renamed group"
	require.Equal(t, "Renamed group", buildUserViewFromSummary(m, MonitorStatusSummary{}, nil, nil).Name)
	require.Empty(t, m.Name, "display fallback never replaces the persisted blank name")
	m.Name = "Custom monitor"
	require.Equal(t, "Custom monitor", buildUserViewFromSummary(m, MonitorStatusSummary{}, nil, nil).Name)
}
