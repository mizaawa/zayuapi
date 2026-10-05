package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryCustomSystemPromptPersistenceAndAuthProjection(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "custom-prompt@test.com")
	key := &service.APIKey{
		UserID: user.ID, Key: "sk-custom-prompt", Name: "prompt", Status: service.StatusActive,
		CustomSystemPromptEnabled: true, CustomSystemPromptForce: true, CustomSystemPrompt: "  Project instructions\n",
	}
	require.NoError(t, repo.Create(ctx, key))
	for _, get := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
		func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
	} {
		got, err := get()
		require.NoError(t, err)
		require.True(t, got.CustomSystemPromptEnabled)
		require.True(t, got.CustomSystemPromptForce)
		require.Equal(t, key.CustomSystemPrompt, got.CustomSystemPrompt)
	}
}

func TestAPIKeyRepositoryCustomSystemPromptDefaults(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "custom-prompt-defaults@test.com")
	key, err := client.APIKey.Create().SetUserID(user.ID).SetKey("sk-custom-prompt-defaults").SetName("defaults").Save(ctx)
	require.NoError(t, err)
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.False(t, got.CustomSystemPromptEnabled)
	require.False(t, got.CustomSystemPromptForce)
	require.Empty(t, got.CustomSystemPrompt)
}

func TestAPIKeyRepositoryPromptOnlyUpdatePreservesConcurrentBilling(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "prompt-billing@test.com")
	key := &service.APIKey{UserID: user.ID, Key: "sk-prompt-billing", Name: "prompt", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))
	stale, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	window := time.Now().UTC().Truncate(time.Second)
	_, err = client.APIKey.Update().Where(apikey.IDEQ(key.ID)).
		SetQuotaUsed(30).SetUsage5h(12).SetUsage1d(18).SetUsage7d(24).
		SetWindow5hStart(window).SetWindow1dStart(window).SetWindow7dStart(window).Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(user.ID).SetBalance(73).Save(ctx)
	require.NoError(t, err)
	stale.CustomSystemPromptEnabled = true
	stale.CustomSystemPromptForce = true
	stale.CustomSystemPrompt = "Project instructions"
	require.NoError(t, repo.Update(ctx, stale, service.APIKeyUpdateFields{
		CustomSystemPromptEnabled: true, CustomSystemPromptForce: true, CustomSystemPrompt: true,
	}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.True(t, got.CustomSystemPromptEnabled)
	require.True(t, got.CustomSystemPromptForce)
	require.Equal(t, stale.CustomSystemPrompt, got.CustomSystemPrompt)
	require.Equal(t, float64(30), got.QuotaUsed)
	require.Equal(t, float64(12), got.Usage5h)
	require.Equal(t, float64(18), got.Usage1d)
	require.Equal(t, float64(24), got.Usage7d)
	require.True(t, got.Window5hStart.Equal(window))
	require.True(t, got.Window1dStart.Equal(window))
	require.True(t, got.Window7dStart.Equal(window))
	require.Equal(t, float64(73), got.User.Balance)

	got.CustomSystemPromptEnabled = false
	require.NoError(t, repo.Update(ctx, got, service.APIKeyUpdateFields{CustomSystemPromptEnabled: true}))
	disabled, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.False(t, disabled.CustomSystemPromptEnabled)
	require.True(t, disabled.CustomSystemPromptForce)
	require.Equal(t, stale.CustomSystemPrompt, disabled.CustomSystemPrompt)
}
