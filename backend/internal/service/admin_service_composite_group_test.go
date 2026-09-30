//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type accountRepoStubForCompositeModelsList struct {
	accountRepoStub
	accounts []Account
}

func (s *accountRepoStubForCompositeModelsList) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	return s.accounts, nil
}

func TestAdminService_CreateCustomGroupCopiesAccountsFromCustomGroups(t *testing.T) {
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		createID: 99,
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformComposite},
			20: {ID: 20, Platform: PlatformComposite},
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{101, 202}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	accountRepo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
		{ID: 101, Platform: PlatformCustom, Type: AccountTypeAPIKey},
		{ID: 202, Platform: PlatformCustom, Type: AccountTypeAPIKey},
	}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: accountRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:               "Custom",
		Platform:           PlatformCustom,
		RateMultiplier:     1,
		MaxReasoningEffort: "medium",
		ReasoningEffortMappings: []ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
		RequireOAuthOnly:         true,
		CopyAccountsFromGroupIDs: []int64{10, 20, 10},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, groupRepo.created.Platform)
	require.False(t, groupRepo.created.RequireOAuthOnly)
	require.Empty(t, groupRepo.created.MaxReasoningEffort)
	require.Empty(t, groupRepo.created.ReasoningEffortMappings)
	require.Equal(t, int64(99), group.ID)
	require.Equal(t, int64(2), group.AccountCount)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{101, 202}, boundAccountIDs)
}

func TestAdminService_CreateCustomGroupRejectsLegacyNonCustomAccounts(t *testing.T) {
	groupRepo := &groupRepoStubForAdmin{
		createID: 99,
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformComposite},
		},
		getAccountIDsByGroupIDsFn: func(_ []int64) ([]int64, error) {
			return []int64{101}, nil
		},
	}
	accountRepo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
		{ID: 101, Platform: PlatformOpenAI},
	}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: accountRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "Custom copy",
		Platform:                 PlatformComposite,
		RateMultiplier:           1,
		CopyAccountsFromGroupIDs: []int64{10},
	})

	require.Nil(t, group)
	require.Equal(t, "CUSTOM_GROUP_ACCOUNT_PLATFORM_MISMATCH", infraerrors.Reason(err))
	require.Nil(t, groupRepo.created)
}

func TestAdminService_UpdateCustomGroupCopiesAccountsFromCustomGroups(t *testing.T) {
	var clearedGroupID int64
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformComposite},
			20: {ID: 20, Platform: PlatformComposite},
			99: {ID: 99, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeStandard, RequireOAuthOnly: true},
		},
		deleteAccountGroupsByGroupIDFn: func(groupID int64) (int64, error) {
			clearedGroupID = groupID
			return 2, nil
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{301, 302}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	accountRepo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
		{ID: 301, Platform: PlatformCustom},
		{ID: 302, Platform: PlatformCustom},
	}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: accountRepo}
	maxReasoningEffort := "low"
	reasoningEffortMappings := []ReasoningEffortMapping{{From: "max", To: "high"}}

	group, err := svc.UpdateGroup(context.Background(), 99, &UpdateGroupInput{
		MaxReasoningEffort:       &maxReasoningEffort,
		ReasoningEffortMappings:  &reasoningEffortMappings,
		CopyAccountsFromGroupIDs: []int64{10, 20},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, group.Platform)
	require.False(t, group.RequireOAuthOnly)
	require.Empty(t, group.MaxReasoningEffort)
	require.Empty(t, group.ReasoningEffortMappings)
	require.Equal(t, int64(99), clearedGroupID)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{301, 302}, boundAccountIDs)
}

func TestAdminService_CreateCustomAccountAllowsCustomGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "Custom account",
		Platform:              PlatformCustom,
		Type:                  AccountTypeAPIKey,
		Credentials:           map[string]any{"api_key": "sk-custom", "base_url": "https://custom.example/v1"},
		Concurrency:           1,
		GroupIDs:              []int64{99},
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Equal(t, PlatformCustom, accountRepo.createAccount.Platform)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_CreateCustomAccountBindsCustomDefaultGroup(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	queriedPlatform := ""
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Name: "custom-default", Platform: PlatformComposite},
		},
		listActiveByPlatformFn: func(platform string) ([]Group, error) {
			queriedPlatform = platform
			return []Group{{ID: 99, Name: "custom-default", Platform: PlatformComposite}}, nil
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "Custom account",
		Platform:              PlatformCustom,
		Type:                  AccountTypeAPIKey,
		Credentials:           map[string]any{"api_key": "sk-custom", "base_url": "https://custom.example/v1"},
		Concurrency:           1,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, queriedPlatform)
	require.Equal(t, int64(7), account.ID)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_UpdateCustomAccountAllowsCustomGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			7: {
				ID:       7,
				Platform: PlatformCustom,
				Type:     AccountTypeAPIKey,
				Status:   StatusActive,
				Credentials: map[string]any{
					"api_key":  "sk-custom",
					"base_url": "https://custom.example/v1",
				},
				Extra: map[string]any{},
			},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}
	groupIDs := []int64{99}

	account, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Len(t, accountRepo.updatedAccounts, 1)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_RejectsNonCustomAccountInCustomGroup(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	groupRepo := &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{
		99: {ID: 99, Platform: PlatformComposite},
	}}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "OpenAI account",
		Platform:              PlatformOpenAI,
		Type:                  AccountTypeAPIKey,
		Credentials:           map[string]any{"api_key": "sk-openai"},
		Concurrency:           1,
		GroupIDs:              []int64{99},
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, account)
	require.Equal(t, "CUSTOM_GROUP_ACCOUNT_PLATFORM_MISMATCH", infraerrors.Reason(err))
	require.Nil(t, accountRepo.createAccount)
}

func TestAccountService_CreateRejectsInvalidCustomCredentialType(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{}
	svc := NewAccountService(accountRepo, nil)

	account, err := svc.Create(context.Background(), CreateAccountRequest{
		Name:        "invalid Custom",
		Platform:    PlatformCustom,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"api_key": "sk-custom", "base_url": "https://custom.example/v1"},
	})

	require.Nil(t, account)
	require.Equal(t, "CUSTOM_ACCOUNT_TYPE_INVALID", infraerrors.Reason(err))
	require.Nil(t, accountRepo.createAccount)
}

func TestAccountService_UpdateRejectsIncompleteCustomCredentials(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{
		7: {
			ID:       7,
			Platform: PlatformCustom,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key":  "sk-custom",
				"base_url": "https://custom.example/v1",
			},
		},
	}}
	svc := NewAccountService(accountRepo, nil)
	incomplete := map[string]any{"api_key": "sk-rotated"}

	account, err := svc.Update(context.Background(), 7, UpdateAccountRequest{Credentials: &incomplete})

	require.Nil(t, account)
	require.Equal(t, "CUSTOM_BASE_URL_REQUIRED", infraerrors.Reason(err))
	require.Empty(t, accountRepo.updatedAccounts)
}

func TestAdminService_CompositeModelsListCandidatesIncludeConcreteAccountMappings(t *testing.T) {
	accountRepo := &accountRepoStubForCompositeModelsList{
		accounts: []Account{
			{
				ID:       1,
				Platform: PlatformCustom,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"custom-chat": "gpt-5"},
				},
			},
			{
				ID:       2,
				Platform: PlatformGemini,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-custom": "gemini-2.5-flash"},
				},
			},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	candidates, err := svc.GetGroupModelsListCandidates(context.Background(), 99, PlatformComposite)

	require.NoError(t, err)
	require.Contains(t, candidates, "custom-chat")
	require.NotContains(t, candidates, "gemini-custom")
	require.NotContains(t, candidates, "gpt-5.5")
	require.NotContains(t, candidates, "gemini-2.5-flash")
}
