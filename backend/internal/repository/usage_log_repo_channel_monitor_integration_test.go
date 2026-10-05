//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorUsageLogs_HidesHistoricalAndDeletedKeyLogsWithoutDeletingData(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "monitor-log-visibility@test.com"})
	account := mustCreateAccount(t, client, &service.Account{Name: "monitor-log-visibility"})
	normal := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-visible-normal", Name: "channel-monitor"})
	monitor := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-hidden-monitor", Name: "renamed", Purpose: service.APIKeyPurposeChannelMonitor})
	deletedMonitor := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-deleted-monitor", Name: "deleted", Purpose: service.APIKeyPurposeChannelMonitor})
	now := time.Now().UTC()
	endpoint := "/v1/messages"
	var visibleIDs, hiddenIDs []int64
	for i, key := range []*service.APIKey{normal, monitor, normal, deletedMonitor} {
		log := &service.UsageLog{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
			RequestID: fmt.Sprintf("monitor-visibility-%d", i), Model: "gpt-test",
			InputTokens: 2, OutputTokens: 3, TotalCost: 1, ActualCost: 0,
			InboundEndpoint: &endpoint, UpstreamEndpoint: &endpoint, CreatedAt: now,
		}
		_, err := repo.Create(ctx, log)
		require.NoError(t, err)
		if key.ID == normal.ID {
			visibleIDs = append(visibleIDs, log.ID)
		} else {
			hiddenIDs = append(hiddenIDs, log.ID)
		}
	}
	require.NoError(t, client.APIKey.DeleteOneID(deletedMonitor.ID).Exec(ctx))
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	filters := UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end, HideChannelMonitorLogs: true, ExactTotal: true}

	for page := 1; page <= 2; page++ {
		logs, paging, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: 1}, filters)
		require.NoError(t, err)
		require.EqualValues(t, 2, paging.Total)
		require.Len(t, logs, 1)
		require.Equal(t, visibleIDs[2-page], logs[0].ID)
	}
	for _, id := range hiddenIDs {
		_, err := repo.GetByIDWithFilters(ctx, id, filters)
		require.ErrorIs(t, err, service.ErrUsageLogNotFound)
		stored, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, id, stored.ID)
	}
	stats, err := repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.TotalRequests)
	require.Equal(t, int64(10), stats.TotalTokens)
	for _, rows := range [][]usagestats.EndpointStat{stats.Endpoints, stats.UpstreamEndpoints, stats.EndpointPaths} {
		require.Len(t, rows, 1)
		require.Equal(t, int64(2), rows[0].Requests)
	}
	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", filters)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, int64(2), trend[0].Requests)
	models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, int64(2), models[0].Requests)
	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, int64(2), groups[0].Requests)

	filters.HideChannelMonitorLogs = false
	logs, paging, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, filters)
	require.NoError(t, err)
	require.Len(t, logs, 4)
	require.EqualValues(t, 4, paging.Total)
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.Equal(t, int64(4), stats.TotalRequests)
	rawStats, err := repo.GetUserStatsAggregated(ctx, user.ID, start, end)
	require.NoError(t, err)
	require.Equal(t, int64(4), rawStats.TotalRequests)
}
