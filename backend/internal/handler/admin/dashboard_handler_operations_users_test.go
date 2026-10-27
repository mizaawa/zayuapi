package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
}

func (r *operationsDashboardTimezoneCapture) GetOperationsDashboardData(_ context.Context, _, _, _, _, _, _ time.Time, timezone string, _ int64) (*service.OperationsDashboardData, error) {
	r.timezone = timezone
	return &service.OperationsDashboardData{}, nil
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
