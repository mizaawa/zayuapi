//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingService_FrozenBalanceReleaseHoursDefaultsAndBounds(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{name: "existing installation without key", want: 1},
		{name: "configured", raw: "6", want: 6},
		{name: "whitespace", raw: " 12 ", want: 12},
		{name: "zero cannot disable release", raw: "0", want: 1},
		{name: "negative", raw: "-2", want: 1},
		{name: "fractional", raw: "1.5", want: 1},
		{name: "invalid", raw: "invalid", want: 1},
		{name: "maximum", raw: "720", want: 720},
		{name: "over maximum", raw: "721", want: 720},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{SettingKeyFrozenBalanceReleaseHours: tc.raw}
			svc := NewSettingService(&settingRepoStub{values: values}, &config.Config{})
			require.Equal(t, tc.want, svc.GetFrozenBalanceReleaseHours(ctx))
			require.Equal(t, tc.want, svc.parseSettings(values).FrozenBalanceReleaseHours)
		})
	}

	var nilService *SettingService
	require.Equal(t, 1, nilService.GetFrozenBalanceReleaseHours(ctx))
	require.Equal(t, 1, NewSettingService(nil, nil).GetFrozenBalanceReleaseHours(ctx))
	require.Equal(t, 1, NewSettingService(&settingRepoStub{err: errors.New("unavailable")}, nil).GetFrozenBalanceReleaseHours(ctx))
}

func TestSettingService_FrozenBalanceReleaseHoursPersists(t *testing.T) {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "1", repo.values[SettingKeyFrozenBalanceReleaseHours])

	for _, hours := range []int{0, 1, 6, 720, 721} {
		t.Run(strconv.Itoa(hours), func(t *testing.T) {
			updateRepo := &settingUpdateRepoStub{}
			updateSvc := NewSettingService(updateRepo, &config.Config{})
			require.NoError(t, updateSvc.UpdateSettings(context.Background(), &SystemSettings{
				FrozenBalanceReleaseHours: hours,
			}))
			want := normalizeFrozenBalanceReleaseHours(hours)
			require.Equal(t, strconv.Itoa(want), updateRepo.updates[SettingKeyFrozenBalanceReleaseHours])
			require.Equal(t, want, updateSvc.parseSettings(updateRepo.updates).FrozenBalanceReleaseHours)
		})
	}
}
