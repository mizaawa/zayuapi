package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type channelMonitorTimelineRepoStub struct {
	ChannelMonitorRepository
	limit int
}

func (r *channelMonitorTimelineRepoStub) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return []*ChannelMonitor{{ID: 7, PrimaryModel: "test-model", Enabled: true}}, nil
}

func (r *channelMonitorTimelineRepoStub) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return map[int64][]*ChannelMonitorLatest{}, nil
}

func (r *channelMonitorTimelineRepoStub) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*ChannelMonitorAvailability, error) {
	return map[int64][]*ChannelMonitorAvailability{}, nil
}

func (r *channelMonitorTimelineRepoStub) ListRecentHistoryForMonitors(
	_ context.Context,
	_ []int64,
	_ map[int64]string,
	limit int,
) (map[int64][]*ChannelMonitorHistoryEntry, error) {
	r.limit = limit
	return map[int64][]*ChannelMonitorHistoryEntry{}, nil
}

func TestChannelMonitorUserTimelineRequestsSeventyFivePoints(t *testing.T) {
	repo := &channelMonitorTimelineRepoStub{}
	service := NewChannelMonitorService(repo, nil)

	_, err := service.ListUserView(context.Background())

	require.NoError(t, err)
	require.Equal(t, 75, repo.limit)
}
