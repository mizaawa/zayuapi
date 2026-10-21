package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// listUsersFilterStub 捕获传入 ListUsers 的 filters，其余 AdminService 方法走 baseline stub。
type listUsersFilterStub struct {
	service.AdminService
	captured service.UserListFilters
}

func (s *listUsersFilterStub) ListUsers(_ context.Context, _, _ int, filters service.UserListFilters, _, _ string) ([]service.User, int64, error) {
	s.captured = filters
	return []service.User{}, 0, nil
}

func TestAdminUserList_ParsesAPIKeyGroupID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name  string
		query string
		want  int64
	}{
		{"valid id", "?api_key_group_id=42", 42},
		{"missing", "", 0},
		{"zero ignored", "?api_key_group_id=0", 0},
		{"negative ignored", "?api_key_group_id=-3", 0},
		{"non-numeric ignored", "?api_key_group_id=abc", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &listUsersFilterStub{AdminService: newStubAdminService()}
			r := gin.New()
			h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
			r.GET("/admin/users", h.List)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/admin/users"+tc.query, nil)
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, tc.want, stub.captured.APIKeyGroupID)
		})
	}
}

func TestAdminUserList_ParsesExclusiveRechargeAndEmailFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []string{"true", "false"} {
		t.Run(enabled, func(t *testing.T) {
			stub := &listUsersFilterStub{AdminService: newStubAdminService()}
			h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.GET("/admin/users", h.List)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/users?exclusive_recharge_enabled="+enabled+"&email=%20user%40example.com%20&page_size=10", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotNil(t, stub.captured.ExclusiveRechargeEnabled)
			require.Equal(t, enabled == "true", *stub.captured.ExclusiveRechargeEnabled)
			require.Equal(t, "user@example.com", stub.captured.Email)
		})
	}
}
