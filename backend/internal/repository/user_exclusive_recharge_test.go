package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserExclusiveRechargeFilteringPaginationAndRevocation(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		_, err := client.User.Create().SetEmail(fmt.Sprintf("grant%02d@example.com", i)).SetPasswordHash("hash").
			SetExclusiveRechargeEnabled(i < 11).Save(ctx)
		require.NoError(t, err)
	}
	grant, loadSubscriptions := true, false
	filters := service.UserListFilters{ExclusiveRechargeEnabled: &grant, IncludeSubscriptions: &loadSubscriptions}
	params := pagination.PaginationParams{Page: 1, PageSize: 10, SortBy: "id", SortOrder: "asc"}
	users, page, err := repo.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.Len(t, users, 10)
	require.EqualValues(t, 11, page.Total)
	params.Page = 2
	users, page, err = repo.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.EqualValues(t, 11, page.Total)
	user := users[0]
	require.True(t, user.ExclusiveRechargeEnabled)
	user.ExclusiveRechargeEnabled = false
	require.NoError(t, repo.Update(ctx, &user, service.UserUpdateFields{ExclusiveRechargeEnabled: true}))
	users, page, err = repo.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.Empty(t, users)
	require.EqualValues(t, 10, page.Total)
	stored, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, stored.ExclusiveRechargeEnabled)
	params.Page = 1
	filters.Email = "GRANT03@"
	users, page, err = repo.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, "grant03@example.com", users[0].Email)
	require.EqualValues(t, 1, page.Total)
	filters.Email = "absent@example.com"
	users, page, err = repo.ListWithFilters(ctx, params, filters)
	require.NoError(t, err)
	require.Empty(t, users)
	require.Zero(t, page.Total)
}

func TestUnrelatedUserUpdatePreservesConcurrentRechargeGrant(t *testing.T) {
	repo, client := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{Email: "stale-grant@example.com", PasswordHash: "hash", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, user))
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	user.Notes = "changed note"
	require.NoError(t, repo.Update(ctx, user, service.UserUpdateFields{Notes: true}))
	stored, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, stored.ExclusiveRechargeEnabled)
	require.Equal(t, "changed note", stored.Notes)
}
