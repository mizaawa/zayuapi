//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOperationsDashboardFoldsDeletedGroups(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "operations-deleted-groups@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-operations-deleted-groups", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "operations-deleted-groups"})
	live := mustCreateGroup(t, client, &service.Group{Name: "operations-live-" + uniqueNano(), RateMultiplier: 1})
	deleted := mustCreateGroup(t, client, &service.Group{Name: "operations-deleted-" + uniqueNano(), RateMultiplier: 1})
	idle := mustCreateGroup(t, client, &service.Group{Name: "operations-idle-" + uniqueNano(), RateMultiplier: 1})

	// The shared integration database keeps other tests' usage around; a day
	// nobody else writes to keeps the range sums exact.
	at := time.Date(2001, 2, 3, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		groupID int64
		model   string
		cost    float64
	}{{live.ID, "operations-live-model", 1.5}, {deleted.ID, "operations-deleted-model", 2.25}} {
		groupID := entry.groupID
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID, GroupID: &groupID,
			Model: entry.model, InputTokens: 1, OutputTokens: 1,
			TotalCost: entry.cost, ActualCost: entry.cost, CreatedAt: at,
		})
		require.NoError(t, err)
	}
	require.NoError(t, client.Group.DeleteOneID(deleted.ID).Exec(ctx))
	require.NoError(t, client.Group.DeleteOneID(idle.ID).Exec(ctx))

	start, end := at.Add(-time.Hour), at.Add(time.Hour)
	data, err := repo.GetOperationsDashboardData(ctx, start, end, start, end, start, end, "UTC", -2)
	require.NoError(t, err)
	groups := make(map[int64]service.OperationsGroupProfitStat, len(data.Groups))
	for _, group := range data.Groups {
		groups[group.GroupID] = group
	}
	require.NotContains(t, groups, deleted.ID)
	require.NotContains(t, groups, idle.ID)
	require.Contains(t, groups, live.ID)
	require.InDelta(t, 1.5, groups[live.ID].UserCost, 1e-9)
	require.Contains(t, groups, int64(-2))
	require.InDelta(t, 2.25, groups[-2].UserCost, 1e-9)
	require.InDelta(t, 2.25, groups[-2].UpstreamCost, 1e-9)

	require.Len(t, data.Models, 1)
	require.Equal(t, "operations-deleted-model", data.Models[0].Model)
	require.InDelta(t, 2.25, data.Models[0].UserCost, 1e-9)
}
