package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type userDashboardRepoStub struct {
	UsageLogRepository
	load func(context.Context, int64, bool, bool) (*usagestats.UserDashboardStats, error)
}

func (r *userDashboardRepoStub) GetUserDashboardStatsWithOptions(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
	return r.load(ctx, id, totals, hidden)
}

func TestUserDashboardCacheScopesAndCopies(t *testing.T) {
	var calls atomic.Int32
	svc := NewUsageService(&userDashboardRepoStub{load: func(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
		calls.Add(1)
		return &usagestats.UserDashboardStats{TotalRequests: id, ByPlatform: []usagestats.PlatformDashboardStats{{Platform: "openai", TotalRequests: id}}}, nil
	}}, nil, nil, nil)
	ctx := context.Background()
	first, err := svc.loadUserDashboardStats(ctx, 1, true, false)
	require.NoError(t, err)
	first.TotalRequests, first.ByPlatform[0].TotalRequests = 99, 99
	second, err := svc.loadUserDashboardStats(ctx, 1, true, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, second.TotalRequests)
	require.EqualValues(t, 1, second.ByPlatform[0].TotalRequests)
	require.NotEmpty(t, second.TotalsUpdatedAt)
	other, err := svc.loadUserDashboardStats(ctx, 2, true, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, other.TotalRequests)
	_, err = svc.loadUserDashboardStats(ctx, 1, true, true)
	require.NoError(t, err)
	today, err := svc.loadUserDashboardStats(ctx, 1, false, false)
	require.NoError(t, err)
	require.True(t, today.TotalsPending)
	require.EqualValues(t, 4, calls.Load())
	_, err = svc.GetUserDashboardStatsToday(ctx, 0)
	require.Error(t, err)
}

func TestUserDashboardCacheCoalescesAndSurvivesCallerCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc := NewUsageService(&userDashboardRepoStub{load: func(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
		calls.Add(1)
		close(started)
		_, hasDeadline := ctx.Deadline()
		if !hasDeadline {
			return nil, errors.New("missing query deadline")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return &usagestats.UserDashboardStats{TotalRequests: 7}, nil
		}
	}}, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := svc.loadUserDashboardStats(ctx, 7, true, false); first <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	var wg sync.WaitGroup
	results := make(chan *usagestats.UserDashboardStats, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats, err := svc.loadUserDashboardStats(context.Background(), 7, true, false)
			results <- stats
			errs <- err
		}()
	}
	close(release)
	wg.Wait()
	for i := 0; i < 20; i++ {
		require.NoError(t, <-errs)
		require.EqualValues(t, 7, (<-results).TotalRequests)
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestUserDashboardCacheLimitsConcurrentHistoryAndDoesNotCacheErrors(t *testing.T) {
	started, release := make(chan struct{}, 2), make(chan struct{})
	svc := NewUsageService(&userDashboardRepoStub{load: func(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
		started <- struct{}{}
		<-release
		return nil, errors.New("database failed")
	}}, nil, nil, nil)
	errs := make(chan error, 2)
	for i := int64(1); i <= 2; i++ {
		go func(id int64) {
			_, err := svc.loadUserDashboardStats(context.Background(), id, true, false)
			errs <- err
		}(i)
	}
	<-started
	<-started
	_, err := svc.loadUserDashboardStats(context.Background(), 3, true, false)
	require.ErrorIs(t, err, errUserDashboardBusy)
	close(release)
	require.Error(t, <-errs)
	require.Error(t, <-errs)
	require.Empty(t, svc.userDashboardCache.entries)
}

func TestUserDashboardCacheKeepsCurrentDayIndependentOfHistory(t *testing.T) {
	svc := NewUsageService(&userDashboardRepoStub{load: func(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
		if totals {
			return &usagestats.UserDashboardStats{TotalRequests: 100, TodayRequests: 1, ByPlatform: []usagestats.PlatformDashboardStats{{Platform: "openai", TotalRequests: 100, TodayRequests: 1}}}, nil
		}
		return &usagestats.UserDashboardStats{TodayRequests: 5, Rpm: 2, TotalAPIKeys: 3, ByPlatform: []usagestats.PlatformDashboardStats{{Platform: "openai", TodayRequests: 5}, {Platform: "gemini", TodayRequests: 1}}}, nil
	}}, nil, nil, nil)
	stats, err := svc.GetUserDashboardStats(context.Background(), 7)
	require.NoError(t, err)
	require.EqualValues(t, 100, stats.TotalRequests)
	require.EqualValues(t, 5, stats.TodayRequests)
	require.EqualValues(t, 2, stats.Rpm)
	require.EqualValues(t, 3, stats.TotalAPIKeys)
	require.Len(t, stats.ByPlatform, 2)
	require.EqualValues(t, 5, stats.ByPlatform[0].TodayRequests)

	cache := svc.userDashboardCache
	cache.set("expired", &usagestats.UserDashboardStats{}, -time.Second)
	require.Nil(t, cache.get("expired"))
	for i := 0; i < userDashboardCacheLimit+50; i++ {
		cache.set(time.Unix(int64(i), 0).String(), &usagestats.UserDashboardStats{}, time.Minute)
	}
	require.LessOrEqual(t, len(cache.entries), userDashboardCacheLimit)
}
