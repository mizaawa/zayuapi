package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageVisibilitySettingsRepo struct {
	service.SettingRepository
	hidden bool
	err    error
}

func (r *usageVisibilitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyChannelMonitorHideUsageLogs && r.err != nil {
		return "", r.err
	}
	if key == service.SettingKeyChannelMonitorHideUsageLogs && r.hidden {
		return "true", nil
	}
	return "false", nil
}

func TestChannelMonitorUsageVisibility_AdminListAndStatsCache(t *testing.T) {
	usageStatsCache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() { usageStatsCache = newSnapshotCache(30 * time.Second) })
	settings := &usageVisibilitySettingsRepo{hidden: true}
	repo := &adminUsageRepoCapture{}
	svc := service.ProvideUsageService(repo, nil, nil, nil, service.NewSettingService(settings, nil))
	h := NewUsageHandler(svc, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/usage", h.List)
	router.GET("/stats", h.Stats)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/usage?exact_total=true", nil))
	require.Equal(t, http.StatusOK, list.Code)
	require.True(t, repo.listFilters.HideChannelMonitorLogs)
	require.True(t, repo.listFilters.ExactTotal)
	for _, hidden := range []bool{true, false} {
		settings.hidden = hidden
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats?start_date=2026-10-01&end_date=2026-10-02", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "miss", rec.Header().Get("X-Usage-Stats-Cache"))
		require.Equal(t, hidden, repo.statsFilters.HideChannelMonitorLogs)
	}
}

type dashboardMonitorVisibilityRepo struct {
	service.UsageLogRepository
}

func monitorVisibilityRequestCount(filters usagestats.UsageLogFilters) int64 {
	if filters.HideChannelMonitorLogs {
		return 1
	}
	return 2
}

type usageStatsVisibilityRepo struct {
	adminUsageRepoCapture
	statsCalls int
}

func (r *usageStatsVisibilityRepo) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.statsCalls++
	r.statsFilters = filters
	return &usagestats.UsageStats{TotalRequests: monitorVisibilityRequestCount(filters)}, nil
}

func TestChannelMonitorUsageVisibility_AdminListAndStatsFailClosed(t *testing.T) {
	usageStatsCache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() { usageStatsCache = newSnapshotCache(30 * time.Second) })
	settings := &usageVisibilitySettingsRepo{}
	repo := &usageStatsVisibilityRepo{}
	svc := service.ProvideUsageService(repo, nil, nil, nil, service.NewSettingService(settings, nil))
	h := NewUsageHandler(svc, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/usage", h.List)
	router.GET("/stats", h.Stats)

	for _, test := range []struct {
		name          string
		err           error
		wantHidden    bool
		wantRequests  int64
		wantCache     string
		wantRepoCalls int
	}{
		{name: "visible", wantRequests: 2, wantCache: "miss", wantRepoCalls: 1},
		{name: "read failure", err: errors.New("settings unavailable"), wantHidden: true, wantRequests: 1, wantCache: "miss", wantRepoCalls: 2},
		{name: "failure cached", err: errors.New("settings unavailable"), wantHidden: true, wantRequests: 1, wantCache: "hit", wantRepoCalls: 2},
		{name: "recovered", wantRequests: 2, wantCache: "hit", wantRepoCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings.err = test.err
			list := httptest.NewRecorder()
			router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/usage", nil))
			require.Equal(t, http.StatusOK, list.Code, list.Body.String())
			require.Equal(t, test.wantHidden, repo.listFilters.HideChannelMonitorLogs)

			stats := httptest.NewRecorder()
			router.ServeHTTP(stats, httptest.NewRequest(http.MethodGet, "/stats?start_date=2026-10-01&end_date=2026-10-02", nil))
			require.Equal(t, http.StatusOK, stats.Code, stats.Body.String())
			require.Equal(t, test.wantCache, stats.Header().Get("X-Usage-Stats-Cache"))
			require.Equal(t, test.wantRepoCalls, repo.statsCalls)
			var response struct {
				Data usagestats.UsageStats `json:"data"`
			}
			require.NoError(t, json.Unmarshal(stats.Body.Bytes(), &response))
			require.Equal(t, test.wantRequests, response.Data.TotalRequests)
		})
	}
}

func (r *dashboardMonitorVisibilityRepo) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, filters usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{{Requests: monitorVisibilityRequestCount(filters)}}, nil
}

func (r *dashboardMonitorVisibilityRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	return []usagestats.ModelStat{{Requests: monitorVisibilityRequestCount(filters)}}, nil
}

func (r *dashboardMonitorVisibilityRepo) GetGroupStatsWithUsageFilters(_ context.Context, _, _ time.Time, filters usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	return []usagestats.GroupStat{{Requests: monitorVisibilityRequestCount(filters)}}, nil
}

func TestChannelMonitorUsageVisibility_AdminUsageChartsKeepDashboardTotals(t *testing.T) {
	for _, route := range []string{"trend", "models", "groups", "snapshot"} {
		t.Run(route, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			t.Cleanup(resetDashboardReadCachesForTest)
			settings := &usageVisibilitySettingsRepo{hidden: true}
			svc := service.NewDashboardService(&dashboardMonitorVisibilityRepo{}, nil, nil, nil)
			h := ProvideDashboardHandler(svc, nil, service.NewSettingService(settings, nil))
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/trend", h.GetUsageTrend)
			router.GET("/models", h.GetModelStats)
			router.GET("/groups", h.GetGroupStats)
			router.GET("/snapshot", h.GetSnapshotV2)
			path := "/" + route + "?start_date=2026-10-01&end_date=2026-10-02&include_stats=false&include_group_stats=true"
			request := func(query string, expectedRequests string) {
				t.Helper()
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, query, nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), `"requests":`+expectedRequests)
			}
			request(path, "2")
			request(path+"&usage_view=true", "1")
			request(path, "2")
			settings.hidden = false
			request(path+"&usage_view=true", "2")
		})
	}
}

func TestChannelMonitorUsageVisibility_AdminUsageChartsFailClosed(t *testing.T) {
	for _, route := range []string{"trend", "models", "groups", "snapshot"} {
		t.Run(route, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			t.Cleanup(resetDashboardReadCachesForTest)
			settings := &usageVisibilitySettingsRepo{}
			svc := service.NewDashboardService(&dashboardMonitorVisibilityRepo{}, nil, nil, nil)
			h := ProvideDashboardHandler(svc, nil, service.NewSettingService(settings, nil))
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/trend", h.GetUsageTrend)
			router.GET("/models", h.GetModelStats)
			router.GET("/groups", h.GetGroupStats)
			router.GET("/snapshot", h.GetSnapshotV2)
			path := "/" + route + "?start_date=2026-10-01&end_date=2026-10-02&include_stats=false&include_group_stats=true"
			request := func(usageView bool, wantRequests int64, wantCache string) {
				t.Helper()
				query := path
				if usageView {
					query += "&usage_view=true"
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, query, nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Equal(t, wantCache, rec.Header().Get("X-Snapshot-Cache"))
				var response struct {
					Data dashboardSnapshotV2Response `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				var counts []int64
				for _, point := range response.Data.Trend {
					counts = append(counts, point.Requests)
				}
				for _, model := range response.Data.Models {
					counts = append(counts, model.Requests)
				}
				for _, group := range response.Data.Groups {
					counts = append(counts, group.Requests)
				}
				require.NotEmpty(t, counts)
				for _, count := range counts {
					require.Equal(t, wantRequests, count)
				}
			}

			request(true, 2, "miss")
			settings.err = errors.New("settings unavailable")
			request(true, 1, "miss")
			request(true, 1, "hit")
			request(false, 2, "hit")
			settings.err = nil
			request(true, 2, "hit")
		})
	}
}
