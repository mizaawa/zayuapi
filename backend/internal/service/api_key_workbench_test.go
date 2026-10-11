package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type freshWorkbenchKeyRepository struct {
	APIKeyRepository
	key   *APIKey
	calls int
}

func (r *freshWorkbenchKeyRepository) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	r.calls++
	if r.key == nil {
		return nil, ErrAPIKeyNotFound
	}
	copy := *r.key
	return &copy, nil
}

func TestWorkbenchFreshKeyLookupAlwaysReadsCurrentRepository(t *testing.T) {
	repo := &freshWorkbenchKeyRepository{key: &APIKey{ID: 9, UserID: 7, Status: StatusActive}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	key, err := svc.GetByKeyFresh(context.Background(), "sk-fixture")
	require.NoError(t, err)
	require.Equal(t, "sk-fixture", key.Key)
	repo.key.Status = StatusDisabled
	key, err = svc.GetByKeyFresh(context.Background(), "sk-fixture")
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, key.Status)
	repo.key = nil
	_, err = svc.GetByKeyFresh(context.Background(), "sk-fixture")
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	require.Equal(t, 3, repo.calls)
}
