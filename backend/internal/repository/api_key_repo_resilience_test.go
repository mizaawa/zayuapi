package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryListUserKeysSurvivesLastUsedIPFailure(t *testing.T) {
	for _, listAll := range []bool{false, true} {
		name := "paginated"
		if listAll {
			name = "all"
		}
		t.Run(name, func(t *testing.T) {
			repo, client := newAPIKeyRepoSQLite(t)
			ctx := context.Background()
			owner := mustCreateAPIKeyRepoUser(t, ctx, client, "owner@test.com")
			other := mustCreateAPIKeyRepoUser(t, ctx, client, "other@test.com")
			ownedKey := &service.APIKey{UserID: owner.ID, Key: "sk-owned", Name: "Owned", Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, ownedKey))
			require.NoError(t, repo.Create(ctx, &service.APIKey{UserID: other.ID, Key: "sk-other", Name: "Other", Status: service.StatusActive}))
			require.NoError(t, repo.Create(ctx, &service.APIKey{UserID: owner.ID, Key: "sk-monitor", Name: "Monitor", Purpose: "channel_monitor", Status: service.StatusActive}))
			deletedKey := &service.APIKey{UserID: owner.ID, Key: "sk-deleted", Name: "Deleted", Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, deletedKey))
			require.NoError(t, repo.Delete(ctx, deletedKey.ID))

			// Only the optional IP lookup uses this failing executor; the real
			// Ent connection still reads the user's keys successfully.
			lookupDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = lookupDB.Close() })
			mock.ExpectQuery("SELECT api_key_id, ip_address").WithArgs(ownedKey.ID).
				WillReturnError(errors.New("last-used IP lookup unavailable"))
			repo.sql = lookupDB

			var keys []service.APIKey
			if listAll {
				keys, err = repo.ListAllByUserID(ctx, owner.ID, service.APIKeyListFilters{})
			} else {
				var page *pagination.PaginationResult
				keys, page, err = repo.ListByUserID(ctx, owner.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
				if err == nil {
					require.EqualValues(t, 1, page.Total)
				}
			}
			t.Logf("IP lookup unavailable: list=%s returned_keys=%d error=%v", name, len(keys), err)
			require.NoError(t, mock.ExpectationsWereMet())
			require.NoError(t, err)
			require.Len(t, keys, 1)
			require.Equal(t, ownedKey.ID, keys[0].ID)
			require.Nil(t, keys[0].LastUsedIP)
		})
	}
}

func TestAPIKeyRepositoryListUserKeysBoundsLastUsedIPLookup(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	owner := mustCreateAPIKeyRepoUser(t, ctx, client, "slow-lookup@test.com")
	key := &service.APIKey{UserID: owner.ID, Key: "sk-slow-lookup", Name: "Slow lookup", Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))

	lookupDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = lookupDB.Close() })
	mock.ExpectQuery("SELECT api_key_id, ip_address").WithArgs(key.ID).
		WillDelayFor(5 * time.Second).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "ip_address"}).AddRow(key.ID, "198.51.100.8"))
	repo.sql = lookupDB

	started := time.Now()
	keys, page, err := repo.ListByUserID(ctx, owner.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	elapsed := time.Since(started)
	t.Logf("Delayed IP lookup: returned_keys=%d elapsed=%v error=%v request_context_error=%v", len(keys), elapsed, err, ctx.Err())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.EqualValues(t, 1, page.Total)
	require.Nil(t, keys[0].LastUsedIP)
	require.Less(t, elapsed, 3*time.Second, "optional metadata must not hold up key management")
	require.NoError(t, ctx.Err(), "the independent timeout must not cancel the request")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyRepositoryListUserKeysPreservesPrimaryQueryFailure(t *testing.T) {
	for _, listAll := range []bool{false, true} {
		name := "paginated"
		if listAll {
			name = "all"
		}
		t.Run(name, func(t *testing.T) {
			repo, client := newAPIKeyRepoSQLite(t)
			lookupDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = lookupDB.Close() })
			repo.sql = lookupDB
			require.NoError(t, client.Close())

			var keys []service.APIKey
			if listAll {
				keys, err = repo.ListAllByUserID(context.Background(), 1, service.APIKeyListFilters{})
			} else {
				keys, _, err = repo.ListByUserID(context.Background(), 1, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
			}
			t.Logf("Primary query unavailable: list=%s returned_keys=%d error=%v", name, len(keys), err)
			require.Error(t, err)
			require.Nil(t, keys)
			require.NoError(t, mock.ExpectationsWereMet(), "optional lookup must not run after the primary query fails")
		})
	}
}
