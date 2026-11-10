package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type operationsUsersRepoCapture struct {
	service.UsageLogRepository
	start, end time.Time
	search     string
	userID     int64
}

type operationsDashboardTimezoneCapture struct {
	service.UsageLogRepository
	timezone string
	groupID  int64
}

func (r *operationsDashboardTimezoneCapture) GetOperationsDashboardData(_ context.Context, _, _, _, _, _, _ time.Time, timezone string, groupID int64) (*service.OperationsDashboardData, error) {
	r.timezone = timezone
	r.groupID = groupID
	return &service.OperationsDashboardData{}, nil
}

func TestGetOperationsDashboardAcceptsDeletedGroupsFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &operationsDashboardTimezoneCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/operations", h.GetOperationsDashboard)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations?group_id=-2", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(-2), repo.groupID)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations?group_id=-3", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetOperationsDashboardDefaultsEmptyTimezone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &operationsDashboardTimezoneCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/operations", h.GetOperationsDashboard)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, repo.timezone)
	require.NotEqual(t, "Local", repo.timezone)
}

type operationsRechargeRankingCapture struct {
	service.UsageLogRepository
	start, end time.Time
	limit      int
}

func (r *operationsRechargeRankingCapture) GetOperationsRechargeRanking(_ context.Context, start, end time.Time, limit int) (*service.OperationsRechargeRankingData, error) {
	r.start, r.end, r.limit = start, end, limit
	return &service.OperationsRechargeRankingData{
		Users:       []service.OperationsRechargeRankingEntry{{UserID: 9, Email: "payer@example.test", Amount: 88.5, RechargeCount: 2}},
		TotalAmount: 120, TotalUsers: 3,
	}, nil
}

func TestGetOperationsRechargeRankingCountsFromSiteMidnight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &operationsRechargeRankingCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/operations/recharge-ranking", h.GetOperationsRechargeRanking)

	before := timezone.StartOfDay(timezone.Now())
	rec := httptest.NewRecorder()
	// A browser timezone must not move the site's day boundaries.
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations/recharge-ranking?timezone=America/Los_Angeles", nil))
	after := timezone.StartOfDay(timezone.Now())
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 50, repo.limit)
	require.True(t, repo.start.Equal(before) || repo.start.Equal(after), "the window starts at today's 00:00, not 24 hours ago")
	local := repo.start.In(timezone.Location())
	require.Equal(t, [3]int{0, 0, 0}, [3]int{local.Hour(), local.Minute(), local.Second()})
	require.True(t, repo.end.Equal(repo.start.AddDate(0, 0, 1)))

	var result struct {
		Data service.OperationsRechargeRankingData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "payer@example.test", result.Data.Users[0].Email)
	require.Equal(t, 88.5, result.Data.Users[0].Amount)
	require.Equal(t, int64(2), result.Data.Users[0].RechargeCount)
	require.Equal(t, 120.0, result.Data.TotalAmount)
	require.Equal(t, int64(3), result.Data.TotalUsers)
	require.Equal(t, local.Format("2006-01-02"), result.Data.Date)
	require.True(t, result.Data.StartTime.Equal(repo.start))
	require.True(t, result.Data.EndTime.Equal(repo.end))
	require.NotEmpty(t, result.Data.SiteTimezone)
	require.NotEqual(t, "Local", result.Data.SiteTimezone)
}

func TestGetOperationsDashboardReportsTodayWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &operationsDashboardTimezoneCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/operations", h.GetOperationsDashboard)
	before := timezone.StartOfDay(timezone.Now())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations?timezone=UTC", nil))
	after := timezone.StartOfDay(timezone.Now())
	require.Equal(t, http.StatusOK, rec.Code)

	var result struct {
		Data service.OperationsDashboardData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.Data.TodayStart.Equal(before) || result.Data.TodayStart.Equal(after))
	require.Equal(t, result.Data.TodayStart.In(timezone.Location()).Format("2006-01-02"), result.Data.TodayDate)
	require.NotEmpty(t, result.Data.SiteTimezone)
}

func (r *operationsUsersRepoCapture) GetOperationsUsersData(_ context.Context, start, end time.Time, search string, userID int64) (*service.OperationsUsersData, error) {
	r.start, r.end, r.search, r.userID = start, end, search, userID
	return &service.OperationsUsersData{Users: []service.OperationsUserSpendStat{{UserID: 7, Email: "user@example.test", UserCost: 10, UpstreamCost: 4}}}, nil
}

func TestGetOperationsUsersFiltersAndValidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &operationsUsersRepoCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/operations/users", h.GetOperationsUsers)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations/users?start_date=2026-10-20&end_date=2026-10-26&timezone=UTC&search=%20USER%40&user_id=7", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "USER@", repo.search)
	require.Equal(t, int64(7), repo.userID)
	require.Equal(t, "2026-10-20", repo.start.Format("2006-01-02"))
	require.Equal(t, "2026-10-27", repo.end.Format("2006-01-02"))
	var result struct {
		Data service.OperationsUsersData `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "user@example.test", result.Data.Users[0].Email)

	bad := httptest.NewRecorder()
	router.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/admin/dashboard/operations/users?user_id=-2", nil))
	require.Equal(t, http.StatusBadRequest, bad.Code)
}
