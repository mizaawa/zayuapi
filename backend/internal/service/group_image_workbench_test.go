//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminGroupImageWorkbenchDeclaration(t *testing.T) {
	ctx := context.Background()
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.CreateGroup(ctx, &CreateGroupInput{
		Name: "image-group", Platform: PlatformOpenAI, RateMultiplier: 1, AllowImageGeneration: true,
	})
	require.NoError(t, err)
	require.False(t, group.ImageWorkbenchEnabled)
	repo.getByID = group
	for _, enabled := range []bool{true, false} {
		updated, err := svc.UpdateGroup(ctx, group.ID, &UpdateGroupInput{ImageWorkbenchEnabled: &enabled})
		require.NoError(t, err)
		require.Equal(t, enabled, updated.ImageWorkbenchEnabled)
		require.Equal(t, enabled, repo.updated.ImageWorkbenchEnabled)
		require.True(t, updated.AllowImageGeneration)
		preserved, err := svc.UpdateGroup(ctx, group.ID, &UpdateGroupInput{})
		require.NoError(t, err)
		require.Equal(t, enabled, preserved.ImageWorkbenchEnabled)
	}
	created, err := svc.CreateGroup(ctx, &CreateGroupInput{
		Name: "declared-group", Platform: PlatformGrok, RateMultiplier: 1, ImageWorkbenchEnabled: true,
	})
	require.NoError(t, err)
	require.True(t, created.ImageWorkbenchEnabled)
	require.True(t, repo.created.ImageWorkbenchEnabled)
}

func TestAPIKeyAuthSnapshotImageWorkbenchRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	for _, enabled := range []bool{true, false} {
		apiKey := &APIKey{ID: 1, User: &User{ID: 2}, Group: &Group{ID: 3, ImageWorkbenchEnabled: enabled}}
		snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
		payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
		require.NoError(t, err)
		var restored APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(payload, &restored))
		materialized, used, err := svc.applyAuthCacheEntry("test-key", &restored)
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, enabled, materialized.Group.ImageWorkbenchEnabled)
		snapshot.Version = 23
		_, used, err = svc.applyAuthCacheEntry("old-key", &APIKeyAuthCacheEntry{Snapshot: snapshot})
		require.NoError(t, err)
		require.False(t, used)
	}
}
