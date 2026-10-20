//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type frozenRecoveryRepoStub struct {
	UsageBillingRepository
	cutoff  time.Time
	limit   int
	users   []int64
	err     error
	started chan struct{}
}

func (r *frozenRecoveryRepoStub) RecoverExpiredBalanceHolds(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	r.cutoff, r.limit = cutoff, limit
	if r.started != nil {
		close(r.started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return r.users, r.err
}

type frozenRecoveryCacheStub struct {
	billingCacheWorkerStub
	users      []int64
	err        error
	contextErr error
}

func (c *frozenRecoveryCacheStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	c.users = append(c.users, userID)
	c.contextErr = ctx.Err()
	return c.err
}

func TestFrozenBalanceRecoveryUsesCurrentSettingAndInvalidatesPartialResults(t *testing.T) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	recoveryErr, cacheErr := errors.New("retry another row"), errors.New("cache unavailable")
	repo := &frozenRecoveryRepoStub{users: []int64{7, 7, 0, -1, 8}, err: recoveryErr}
	cache := &frozenRecoveryCacheStub{err: cacheErr}
	settings := &settingRepoStub{values: map[string]string{}}
	svc := &OpenAIGatewayService{
		settingService:      NewSettingService(settings, nil),
		billingCacheService: &BillingCacheService{cache: cache},
	}
	err := svc.recoverFrozenBalanceOnce(context.Background(), repo, now)
	require.ErrorIs(t, err, recoveryErr)
	require.ErrorIs(t, err, cacheErr)
	require.Equal(t, now.Add(-time.Hour), repo.cutoff)
	require.Equal(t, 100, repo.limit)
	require.Equal(t, []int64{7, 8}, cache.users)

	settings.values[SettingKeyFrozenBalanceReleaseHours] = "6"
	repo.users, repo.err, cache.err = nil, nil, nil
	require.NoError(t, svc.recoverFrozenBalanceOnce(context.Background(), repo, now))
	require.Equal(t, now.Add(-6*time.Hour), repo.cutoff)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo.users = []int64{7}
	require.NoError(t, svc.recoverFrozenBalanceOnce(ctx, repo, now))
	require.NoError(t, cache.contextErr, "committed refunds invalidate caches even after a scan timeout")
}

func TestFrozenBalanceRecoveryRunsWithoutBatchQueueAndStops(t *testing.T) {
	repo := &frozenRecoveryRepoStub{started: make(chan struct{})}
	svc := &OpenAIGatewayService{usageBillingRepo: repo}
	svc.SetMediaBillingAPIKeyService(nil)
	require.Nil(t, svc.frozenBalanceCancel, "wait until auth cache invalidation is available")
	svc.SetMediaBillingAPIKeyService(&APIKeyService{})
	select {
	case <-repo.started:
	case <-time.After(3 * time.Second):
		t.Fatal("recovery did not run at startup without a batch queue or media provider")
	}
	svc.SetMediaBillingAPIKeyService(&APIKeyService{}) // Rewiring must not start a second worker.
	closed := make(chan struct{})
	go func() { svc.CloseOpenAIWSPool(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel the in-flight recovery")
	}
	select {
	case <-svc.frozenBalanceDone:
	default:
		t.Fatal("worker was not joined at shutdown")
	}
}
