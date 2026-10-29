//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type opsStatsAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (r *opsStatsAccountRepoStub) ListOpsAccountsForStats(_ context.Context, _ string, _ *int64) ([]Account, error) {
	return r.accounts, nil
}

func TestGetConcurrencyStats_LoadIgnoresUnlimitedAccounts(t *testing.T) {
	mixed := &Group{ID: 7, Name: "mixed", Platform: PlatformAnthropic}
	repo := &opsStatsAccountRepoStub{accounts: []Account{
		{ID: 1, Platform: PlatformAnthropic, Concurrency: 10, Groups: []*Group{mixed}},
		{ID: 2, Platform: PlatformAnthropic, Concurrency: 0, Groups: []*Group{mixed}},
	}}
	cache := &stubConcurrencyCacheForTest{loadBatch: map[int64]*AccountLoadInfo{
		1: {AccountID: 1, CurrentConcurrency: 2},
		2: {AccountID: 2, CurrentConcurrency: 30},
	}}
	svc := &OpsService{accountRepo: repo, concurrencyService: NewConcurrencyService(cache)}

	platform, group, account, _, err := svc.GetConcurrencyStats(context.Background(), "", nil)
	require.NoError(t, err)

	// In-flight requests on the unlimited account are still reported...
	require.Equal(t, int64(32), platform[PlatformAnthropic].CurrentInUse)
	require.Equal(t, int64(32), group[7].CurrentInUse)
	require.Equal(t, int64(30), account[2].CurrentInUse)
	// ...but the load only compares limited accounts with their capacity.
	require.InDelta(t, 20.0, platform[PlatformAnthropic].LoadPercentage, 1e-9)
	require.InDelta(t, 20.0, group[7].LoadPercentage, 1e-9)
	require.Zero(t, account[2].LoadPercentage)
}
