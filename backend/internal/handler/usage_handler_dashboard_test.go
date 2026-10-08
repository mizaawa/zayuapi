package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dashboardUsageRepoStub struct {
	service.UsageLogRepository
	userID   int64
	totals   bool
	params   pagination.PaginationParams
	filters  usagestats.UsageLogFilters
	deadline bool
}

func (r *dashboardUsageRepoStub) GetUserDashboardStatsWithOptions(ctx context.Context, id int64, totals, hidden bool) (*usagestats.UserDashboardStats, error) {
	r.userID, r.totals = id, totals
	return &usagestats.UserDashboardStats{TodayRequests: id}, nil
}

func (r *dashboardUsageRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	r.params, r.filters = params, filters
	_, r.deadline = ctx.Deadline()
	return nil, &pagination.PaginationResult{}, nil
}

func dashboardTestRouter(repo *dashboardUsageRepoStub, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	if authenticated {
		router.Use(func(c *gin.Context) { c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7}) })
	}
	router.GET("/usage/dashboard/stats", h.DashboardStats)
	router.GET("/usage/dashboard/recent", h.DashboardRecent)
	return router
}

func TestUserDashboardHandlerRequiresAuthAndIgnoresClientUserID(t *testing.T) {
	for _, path := range []string{"/usage/dashboard/stats?include_totals=false&user_id=99", "/usage/dashboard/recent?user_id=99&page_size=1000&sort_by=model"} {
		repo := &dashboardUsageRepoStub{}
		rec := httptest.NewRecorder()
		dashboardTestRouter(repo, false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Zero(t, repo.userID)
		rec = httptest.NewRecorder()
		dashboardTestRouter(repo, true).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		if repo.userID != 0 {
			require.EqualValues(t, 7, repo.userID)
			require.False(t, repo.totals)
		} else {
			require.EqualValues(t, 7, repo.filters.UserID)
			require.Equal(t, 5, repo.params.PageSize)
			require.Equal(t, "created_at", repo.params.SortBy)
			require.True(t, repo.filters.SkipTotal)
			require.True(t, repo.deadline)
			require.InDelta(t, 7*24, repo.filters.EndTime.Sub(*repo.filters.StartTime).Hours(), 1)
		}
	}
}

func TestUserDashboardStatsValidatesOptionsAndDefaultsToFullStats(t *testing.T) {
	repo := &dashboardUsageRepoStub{}
	router := dashboardTestRouter(repo, true)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/stats?include_totals=invalid", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, repo.userID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/dashboard/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.totals)
	var result struct {
		Data usagestats.UserDashboardStats `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.False(t, result.Data.TotalsPending)
	_, err := time.Parse(time.RFC3339, result.Data.TotalsUpdatedAt)
	require.NoError(t, err)
}

func TestUserDashboardRejectsInvalidSubjectBeforeQuery(t *testing.T) {
	for _, id := range []int64{0, -1} {
		repo := &dashboardUsageRepoStub{}
		h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
		for _, handler := range []gin.HandlerFunc{h.DashboardStats, h.DashboardRecent} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			ctx.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: id})
			handler(ctx)
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.Zero(t, repo.userID)
			require.Zero(t, repo.params.PageSize)
		}
	}
}
