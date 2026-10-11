package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func queuedVideoAuthKey() *service.APIKey {
	key := queuedAuthKey()
	key.Group.Platform = service.PlatformComposite
	key.Group.VideoWorkbenchEnabled = true
	return key
}

func TestRefreshVideoWorkbenchAuthRevocationAndCurrentContexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		change  func(*service.APIKey)
		deleted bool
	}{
		{name: "deleted key", deleted: true},
		{name: "disabled key", change: func(key *service.APIKey) { key.Status = service.StatusDisabled }},
		{name: "expired key", change: func(key *service.APIKey) { expired := time.Now().Add(-time.Second); key.ExpiresAt = &expired }},
		{name: "disabled user", change: func(key *service.APIKey) { key.User.Status = service.StatusDisabled }},
		{name: "disabled group", change: func(key *service.APIKey) { key.Group.Status = service.StatusDisabled }},
		{name: "removed declaration", change: func(key *service.APIKey) { key.Group.VideoWorkbenchEnabled = false }},
		{name: "non custom group", change: func(key *service.APIKey) { key.Group.Platform = service.PlatformOpenAI }},
		{name: "group changed", change: func(key *service.APIKey) { key.Group.ID++ }},
		{name: "user changed", change: func(key *service.APIKey) { key.UserID++ }},
		{name: "exclusive group access revoked", change: func(key *service.APIKey) { key.Group.IsExclusive = true }},
		{name: "quota consumed", change: func(key *service.APIKey) { key.Quota = 1; key.QuotaUsed = 1 }},
		{name: "balance consumed", change: func(key *service.APIKey) { key.User.Balance = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous, fresh := queuedVideoAuthKey(), queuedVideoAuthKey()
			if tc.change != nil {
				tc.change(fresh)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video-workbench/tasks", nil)
			c.Set(string(ContextKeyAPIKey), previous)
			lookup := queuedKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) {
				if tc.deleted {
					return nil, service.ErrAPIKeyNotFound
				}
				return fresh, nil
			})
			require.ErrorIs(t, RefreshVideoWorkbenchAuth(c, lookup, nil, &config.Config{}), service.ErrVideoTaskForbidden)
		})
	}
	previous, fresh := queuedVideoAuthKey(), queuedVideoAuthKey()
	previous.User.Role = service.RoleAdmin
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video-workbench/tasks", nil).WithContext(context.WithValue(context.Background(), ctxkey.Group, previous.Group))
	c.Set(string(ContextKeyAPIKey), previous)
	c.Set(string(ContextKeySubscription), &service.UserSubscription{ID: 99})
	require.NoError(t, RefreshVideoWorkbenchAuth(c, queuedKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return fresh, nil }), nil, &config.Config{}))
	key, ok := GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Same(t, fresh, key)
	role, _ := GetUserRoleFromContext(c)
	require.Equal(t, service.RoleUser, role)
	require.Same(t, fresh.Group, c.Request.Context().Value(ctxkey.Group))
	subscription, _ := GetSubscriptionFromContext(c)
	require.Nil(t, subscription)
}
