package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 角色提升为管理员的 step-up 门控条件测试。
// 测试环境不注入认证上下文，因此门控一旦触发会以 401 中止；
// 借此区分「触发了 step-up 校验」与「直接放行到业务层（200）」。
func setupRoleStepUpRouter(t *testing.T) (*gin.Engine, *stubAdminService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	// 追加一个已是管理员的目标用户，验证「目标已是 admin 不触发门控」。
	adminSvc.users = append(adminSvc.users, service.User{
		ID:     2,
		Email:  "admin@example.com",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	})

	h := NewUserHandler(adminSvc, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/users", h.Create)
	router.PUT("/api/v1/admin/users/:id", h.Update)
	router.POST("/api/v1/admin/users/:id/auth-identities", h.BindAuthIdentity)
	return router, adminSvc
}

func doJSON(t *testing.T, router *gin.Engine, method, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestUpdateUserPromoteToAdminRequiresStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)

	rec := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/1", map[string]any{"role": "admin"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUpdateUserKeepAdminRoleSkipsStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)

	rec := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/2", map[string]any{"role": "admin"})
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestUpdateUserRegularRoleSkipsStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)

	rec := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/1", map[string]any{"role": "user", "email": "u@example.com"})
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestCreateAdminUserRequiresStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/admin/users", map[string]any{
		"email": "new-admin@example.com", "password": "pass123", "role": "admin",
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateRegularUserSkipsStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/admin/users", map[string]any{
		"email": "new-user@example.com", "password": "pass123", "role": "user",
	})
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestExistingAdminCredentialChangesRequireStepUp(t *testing.T) {
	for _, payload := range []map[string]any{
		{"password": "replacement-password"},
		{"role": "admin", "password": "replacement-password"},
		{"email": "new-admin@example.com"},
		{"role": "user", "password": "replacement-password"},
		{"role": "user"},
	} {
		t.Run(fmt.Sprint(payload), func(t *testing.T) {
			router, svc := setupRoleStepUpRouter(t)
			before, err := svc.GetUser(context.Background(), 2)
			require.NoError(t, err)
			original := *before
			rec := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/2", payload)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			after, err := svc.GetUser(context.Background(), 2)
			require.NoError(t, err)
			require.Equal(t, original, *after)
		})
	}
}

func TestExistingAdminUnchangedEmailSkipsStepUp(t *testing.T) {
	router, _ := setupRoleStepUpRouter(t)
	rec := doJSON(t, router, http.MethodPut, "/api/v1/admin/users/2", map[string]any{
		"role": "admin", "email": "admin@example.com", "notes": "routine edit",
	})
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestAdminIdentityBindingRequiresStepUp(t *testing.T) {
	router, svc := setupRoleStepUpRouter(t)
	rec := doJSON(t, router, http.MethodPost, "/api/v1/admin/users/2/auth-identities", map[string]any{
		"provider_type": "oidc", "provider_key": "oidc", "provider_subject": "new-subject",
	})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Zero(t, svc.boundAuthIdentityFor)
}

func TestRegularUserIdentityBindingSkipsStepUp(t *testing.T) {
	router, svc := setupRoleStepUpRouter(t)
	rec := doJSON(t, router, http.MethodPost, "/api/v1/admin/users/1/auth-identities", map[string]any{
		"provider_type": "oidc", "provider_key": "oidc", "provider_subject": "new-subject",
	})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(1), svc.boundAuthIdentityFor)
}

func TestMachineAdminKeyCannotChangeExistingAdminCredentialsWithStepUpEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	svc.users = append(svc.users, service.User{ID: 2, Role: service.RoleAdmin, Email: "admin@example.com"})
	h := NewUserHandler(svc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
		c.Next()
	})
	router.PUT("/users/:id", h.Update)
	router.POST("/users/:id/auth-identities", h.BindAuthIdentity)
	for _, tc := range []struct {
		method  string
		path    string
		payload map[string]any
	}{
		{http.MethodPut, "/users/2", map[string]any{"password": "replacement-password"}},
		{http.MethodPut, "/users/2", map[string]any{"email": "replacement@example.com"}},
		{http.MethodPost, "/users/2/auth-identities", map[string]any{"provider_type": "oidc", "provider_key": "oidc", "provider_subject": "replacement"}},
	} {
		rec := doJSON(t, router, tc.method, tc.path, tc.payload)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
	}
	require.Zero(t, svc.boundAuthIdentityFor)
}
