//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// degradationKeyRepoStub extends the channel monitor key stub with ID lookups.
type degradationKeyRepoStub struct {
	channelMonitorAPIKeyRepoStub
}

func (r *degradationKeyRepoStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if r.created == nil || r.created.ID != id {
		return nil, ErrAPIKeyNotFound
	}
	cloned := *r.created
	cloned.GroupID = cloneInt64Pointer(r.created.GroupID)
	return &cloned, nil
}

func TestAPIKeyServiceManagesDegradationMonitorKeysByID(t *testing.T) {
	repo := &degradationKeyRepoStub{}
	userRepo := &channelMonitorKeyUserRepoStub{user: &User{ID: 9, Role: RoleAdmin, Status: StatusActive}}
	groupRepo := &channelMonitorKeyGroupRepoStub{group: &Group{ID: 7, Status: StatusActive}}
	svc := NewAPIKeyService(repo, userRepo, groupRepo, nil, nil, nil, nil)
	ctx := context.Background()

	key, err := svc.CreateDegradationMonitorKey(ctx, 9, 7, "  <Pelican>  ")
	require.NoError(t, err)
	require.Equal(t, APIKeyPurposeChannelMonitor, key.Purpose, "degradation keys share the managed probe purpose")
	require.True(t, strings.HasPrefix(key.Name, degradationMonitorAPIKeyNamePrefix))
	require.NotContains(t, key.Name, "<")
	require.Equal(t, int64(7), *key.GroupID)

	loaded, err := svc.GetManagedMonitorKey(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, key.Key, loaded.Key)

	_, err = svc.GetManagedMonitorKey(ctx, 0)
	require.ErrorIs(t, err, ErrAPIKeyNotFound)

	require.NoError(t, svc.DeleteManagedMonitorKeyByID(ctx, key.ID, 9))
	require.Equal(t, []int64{key.ID}, repo.deleted)
	require.NoError(t, svc.DeleteManagedMonitorKeyByID(ctx, 12345, 9), "a missing key is already retired")
}

func TestAPIKeyServiceRefusesOrdinaryKeysAsManagedMonitorKeys(t *testing.T) {
	repo := &degradationKeyRepoStub{}
	repo.created = &APIKey{ID: 41, UserID: 9, Key: "ordinary-user-key"}
	svc := NewAPIKeyService(repo, &channelMonitorKeyUserRepoStub{}, &channelMonitorKeyGroupRepoStub{}, nil, nil, nil, nil)
	ctx := context.Background()

	_, err := svc.GetManagedMonitorKey(ctx, 41)
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.ErrorIs(t, svc.DeleteManagedMonitorKeyByID(ctx, 41, 9), ErrInsufficientPerms)
	require.Empty(t, repo.deleted)
}

func TestAPIKeyServiceRefusesDegradationKeyForOrdinaryUser(t *testing.T) {
	repo := &degradationKeyRepoStub{}
	userRepo := &channelMonitorKeyUserRepoStub{user: &User{ID: 9, Role: RoleUser, Status: StatusActive}}
	groupRepo := &channelMonitorKeyGroupRepoStub{group: &Group{ID: 7, Status: StatusActive}}
	svc := NewAPIKeyService(repo, userRepo, groupRepo, nil, nil, nil, nil)

	key, err := svc.CreateDegradationMonitorKey(context.Background(), 9, 7, "monitor")
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Nil(t, key)
}
