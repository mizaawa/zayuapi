//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAdminService_CreateGroup_StoresCyberAutoCharge(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 5}
	svc := &adminServiceImpl{groupRepo: repo}
	amount := 0.5

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                   "cyber-charge",
		Platform:               PlatformOpenAI,
		RateMultiplier:         1,
		CyberAutoChargeEnabled: true,
		CyberAutoChargeAmount:  &amount,
	})

	require.NoError(t, err)
	require.True(t, repo.created.CyberAutoChargeEnabled)
	require.Equal(t, 0.5, repo.created.CyberAutoChargeAmount)
}

func TestAdminService_CreateGroup_RejectsCyberAutoChargeWithoutAmount(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                   "cyber-charge",
		Platform:               PlatformOpenAI,
		RateMultiplier:         1,
		CyberAutoChargeEnabled: true,
	})

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Nil(t, repo.created, "an invalid charge must not be saved")
}

func TestAdminService_UpdateGroup_MergesCyberAutoCharge(t *testing.T) {
	existing := &Group{ID: 1, Name: "existing", Platform: PlatformOpenAI, Status: StatusActive, CyberAutoChargeAmount: 0.3}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}
	enabled := true

	// Turning the charge on keeps the amount saved earlier.
	_, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CyberAutoChargeEnabled: &enabled})
	require.NoError(t, err)
	require.True(t, repo.updated.CyberAutoChargeEnabled)
	require.Equal(t, 0.3, repo.updated.CyberAutoChargeAmount)

	negative := -1.0
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CyberAutoChargeAmount: &negative})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
}
