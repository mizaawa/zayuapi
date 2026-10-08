package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"golang.org/x/sync/singleflight"
)

const userDashboardCacheLimit = 1024

var errUserDashboardBusy = infraerrors.New(http.StatusServiceUnavailable, "DASHBOARD_BUSY", "Dashboard statistics are busy, please retry later")

type userDashboardReader interface {
	GetUserDashboardStatsWithOptions(context.Context, int64, bool, bool) (*usagestats.UserDashboardStats, error)
}

type userDashboardCacheEntry struct {
	stats     *usagestats.UserDashboardStats
	expiresAt time.Time
}

type userDashboardCache struct {
	mu      sync.Mutex
	entries map[string]userDashboardCacheEntry
	flight  singleflight.Group
	today   chan struct{}
	totals  chan struct{}
}

func newUserDashboardCache() *userDashboardCache {
	return &userDashboardCache{
		entries: make(map[string]userDashboardCacheEntry),
		today:   make(chan struct{}, 8), totals: make(chan struct{}, 2),
	}
}

func cloneUserDashboardStats(stats *usagestats.UserDashboardStats) *usagestats.UserDashboardStats {
	copy := *stats
	copy.ByPlatform = append([]usagestats.PlatformDashboardStats(nil), stats.ByPlatform...)
	return &copy
}

func (cache *userDashboardCache) get(key string) *usagestats.UserDashboardStats {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok {
		return nil
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(cache.entries, key)
		return nil
	}
	return cloneUserDashboardStats(entry.stats)
}

func (cache *userDashboardCache) set(key string, stats *usagestats.UserDashboardStats, ttl time.Duration) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	if len(cache.entries) >= userDashboardCacheLimit {
		for k, entry := range cache.entries {
			if !now.Before(entry.expiresAt) {
				delete(cache.entries, k)
			}
		}
		if len(cache.entries) >= userDashboardCacheLimit {
			for k := range cache.entries {
				delete(cache.entries, k)
				break
			}
		}
	}
	cache.entries[key] = userDashboardCacheEntry{cloneUserDashboardStats(stats), now.Add(ttl)}
}

func (s *UsageService) GetUserDashboardStatsToday(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, error) {
	return s.getUserDashboardStatsCached(ctx, userID, false)
}

func (s *UsageService) getUserDashboardStatsCached(ctx context.Context, userID int64, includeTotals bool) (*usagestats.UserDashboardStats, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "Invalid user ID")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, filters := s.PrepareLogFilters(ctx, usagestats.UsageLogFilters{UserID: userID})
	if !includeTotals {
		return s.loadUserDashboardStats(ctx, userID, false, filters.HideChannelMonitorLogs)
	}
	// Refresh the small current-day query independently of the historical cache.
	today, err := s.loadUserDashboardStats(ctx, userID, false, filters.HideChannelMonitorLogs)
	if err != nil {
		return nil, err
	}
	stats, err := s.loadUserDashboardStats(ctx, userID, true, filters.HideChannelMonitorLogs)
	if err != nil {
		return nil, err
	}
	stats.TotalAPIKeys, stats.ActiveAPIKeys = today.TotalAPIKeys, today.ActiveAPIKeys
	stats.TodayRequests = today.TodayRequests
	stats.TodayInputTokens, stats.TodayOutputTokens = today.TodayInputTokens, today.TodayOutputTokens
	stats.TodayCacheCreationTokens, stats.TodayCacheReadTokens = today.TodayCacheCreationTokens, today.TodayCacheReadTokens
	stats.TodayTokens, stats.TodayCost, stats.TodayActualCost = today.TodayTokens, today.TodayCost, today.TodayActualCost
	stats.Rpm, stats.Tpm = today.Rpm, today.Tpm
	platforms := make(map[string]usagestats.PlatformDashboardStats, len(today.ByPlatform))
	for _, p := range today.ByPlatform {
		platforms[p.Platform] = p
	}
	for i := range stats.ByPlatform {
		p := &stats.ByPlatform[i]
		current := platforms[p.Platform]
		p.TodayRequests, p.TodayTokens, p.TodayActualCost = current.TodayRequests, current.TodayTokens, current.TodayActualCost
		delete(platforms, p.Platform)
	}
	for _, p := range platforms {
		p.TotalRequests, p.TotalTokens, p.TotalActualCost = 0, 0, 0
		stats.ByPlatform = append(stats.ByPlatform, p)
	}
	return stats, nil
}

func (s *UsageService) loadUserDashboardStats(ctx context.Context, userID int64, includeTotals, hideMonitor bool) (*usagestats.UserDashboardStats, error) {
	cache := s.userDashboardCache
	if cache == nil {
		return nil, errUserDashboardBusy
	}
	// The day and visibility are part of the key to prevent midnight/setting changes
	// from returning statistics with a different scope.
	key := fmt.Sprintf("%d:%t:%t:%d", userID, includeTotals, hideMonitor, timezone.Today().Unix())
	if stats := cache.get(key); stats != nil {
		return stats, nil
	}
	result := cache.flight.DoChan(key, func() (any, error) {
		if stats := cache.get(key); stats != nil {
			return stats, nil
		}
		slots, timeout, ttl := cache.today, 5*time.Second, 15*time.Second
		if includeTotals {
			slots, timeout, ttl = cache.totals, 12*time.Second, 5*time.Minute
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			return nil, errUserDashboardBusy
		}
		// One caller disconnecting must not cancel work shared by other callers.
		// Detached work has a deadline and never waits for a database worker slot.
		queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		reader, ok := s.usageRepo.(userDashboardReader)
		if !ok {
			return nil, errUserDashboardBusy
		}
		stats, err := reader.GetUserDashboardStatsWithOptions(queryCtx, userID, includeTotals, hideMonitor)
		if err != nil {
			return nil, fmt.Errorf("get user dashboard stats: %w", err)
		}
		if stats == nil {
			return nil, errUserDashboardBusy
		}
		stats.TotalsPending = !includeTotals
		if includeTotals {
			stats.TotalsUpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		cache.set(key, stats, ttl)
		return stats, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		stats, ok := loaded.Val.(*usagestats.UserDashboardStats)
		if !ok || stats == nil {
			return nil, errUserDashboardBusy
		}
		return cloneUserDashboardStats(stats), nil
	}
}
