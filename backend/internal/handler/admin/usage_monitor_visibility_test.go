package admin

import (
	"context"
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
}

func (r *usageVisibilitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
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
