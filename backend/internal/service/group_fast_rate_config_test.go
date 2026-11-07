//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminGroupFastRateDefaultsAndUpdates(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 1}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "openai-fast", Platform: PlatformOpenAI, RateMultiplier: 1,
	})
	require.NoError(t, err)
	require.False(t, group.FastRateEnabled)
	require.Equal(t, 2.0, group.FastRateMultiplier)

	repo.getByID = group
	enabled, factor := true, 3.5
	updated, err := svc.UpdateGroup(context.Background(), group.ID, &UpdateGroupInput{
		FastRateEnabled: &enabled, FastRateMultiplier: &factor,
	})
	require.NoError(t, err)
	require.True(t, updated.FastRateEnabled)
	require.Equal(t, 3.5, updated.FastRateMultiplier)
	require.Equal(t, updated, repo.updated)

	enabled = false
	updated, err = svc.UpdateGroup(context.Background(), group.ID, &UpdateGroupInput{FastRateEnabled: &enabled})
	require.NoError(t, err)
	require.False(t, updated.FastRateEnabled)
	require.Equal(t, 3.5, updated.FastRateMultiplier, "disabling keeps the entered preset")
}

func TestAdminGroupFastRateRejectsInvalidMultiplier(t *testing.T) {
	for _, factor := range []float64{0, -1, 100.01, math.NaN(), math.Inf(1)} {
		t.Run("invalid", func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name: "openai-fast", Platform: PlatformOpenAI, RateMultiplier: 1,
				FastRateEnabled: true, FastRateMultiplier: &factor,
			})
			require.ErrorContains(t, err, "fast_rate_multiplier")
			require.Nil(t, repo.created)
		})
	}
	otherEnabled, factor := true, 8.0
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "anthropic", Platform: PlatformAnthropic, RateMultiplier: 1,
		FastRateEnabled: otherEnabled, FastRateMultiplier: &factor,
	})
	require.NoError(t, err)
	require.False(t, group.FastRateEnabled)
	require.Equal(t, 2.0, group.FastRateMultiplier)
}
