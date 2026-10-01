package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryFailoverCooldownDoesNotExtendAndReleaseWins(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "failover-repo@test.com")
	primary, err := client.Group.Create().SetName("primary").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	fallback, err := client.Group.Create().SetName("fallback").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{
		UserID: user.ID, Key: "sk-failover-repo", Name: "Failover", Status: service.StatusActive,
		GroupID: &primary.ID, FailoverEnabled: true, FailoverGroupID: &fallback.ID,
		QuotaUsed: 42, Usage5h: 15,
	}
	require.NoError(t, repo.Create(ctx, key))
	require.Equal(t, 3, key.FailoverMaxRetries)
	require.Equal(t, 300, key.FailoverCooldownSeconds)
	auth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.True(t, auth.FailoverEnabled)
	require.Equal(t, fallback.ID, *auth.FailoverGroupID)
	require.Equal(t, 3, auth.FailoverMaxRetries)

	now := time.Now()
	until, err := repo.StartAPIKeyFailoverCooldown(ctx, key, now)
	require.NoError(t, err)
	require.WithinDuration(t, now.Add(300*time.Second), *until, time.Millisecond)
	again, err := repo.StartAPIKeyFailoverCooldown(ctx, key, now.Add(30*time.Second))
	require.NoError(t, err)
	require.Equal(t, *until, *again)
	key.FailoverCooldownUntil = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{FailoverCooldown: true}))
	stale := *key
	stale.FailoverRevision--
	_, err = repo.StartAPIKeyFailoverCooldown(ctx, &stale, now.Add(time.Second))
	require.ErrorIs(t, err, service.ErrAPIKeyFailoverChanged)
	current, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, current.FailoverCooldownUntil)
	require.Equal(t, primary.ID, *current.GroupID)
	require.Equal(t, float64(42), current.QuotaUsed)
	require.True(t, current.FailoverEnabled)

	_, err = repo.StartAPIKeyFailoverCooldown(ctx, key, now.Add(time.Second))
	require.NoError(t, err)
	key.FailoverEnabled = false
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{FailoverConfig: true, FailoverCooldown: true}))
	_, err = repo.StartAPIKeyFailoverCooldown(ctx, key, now.Add(2*time.Second))
	require.ErrorIs(t, err, service.ErrAPIKeyFailoverChanged)
}
