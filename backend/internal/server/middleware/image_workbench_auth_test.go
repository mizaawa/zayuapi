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

type queuedKeyLookupFunc func(context.Context, string) (*service.APIKey, error)

func (f queuedKeyLookupFunc) GetByKeyFresh(ctx context.Context, key string) (*service.APIKey, error) {
	return f(ctx, key)
}

func queuedAuthKey() *service.APIKey {
	groupID := int64(3)
	return &service.APIKey{
		ID: 9, UserID: 7, Key: "sk-fixture", Status: service.StatusActive, GroupID: &groupID,
		User:  &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Platform: service.PlatformOpenAI, Hydrated: true, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
	}
}

func TestRefreshWorkbenchAuthRevocationAndCurrentContexts(t *testing.T) {
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
		{name: "removed declaration", change: func(key *service.APIKey) { key.Group.ImageWorkbenchEnabled = false }},
		{name: "removed image access", change: func(key *service.APIKey) { key.Group.AllowImageGeneration = false }},
		{name: "group changed", change: func(key *service.APIKey) { key.Group.ID++ }},
		{name: "exclusive group access revoked", change: func(key *service.APIKey) { key.Group.IsExclusive = true }},
		{name: "quota consumed", change: func(key *service.APIKey) { key.Quota = 1; key.QuotaUsed = 1 }},
		{name: "balance consumed", change: func(key *service.APIKey) { key.User.Balance = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := queuedAuthKey()
			fresh := queuedAuthKey()
			if tc.change != nil {
				tc.change(fresh)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			c.Set(string(ContextKeyAPIKey), previous)
			lookup := queuedKeyLookupFunc(func(_ context.Context, raw string) (*service.APIKey, error) {
				require.Equal(t, previous.Key, raw)
				if tc.deleted {
					return nil, service.ErrAPIKeyNotFound
				}
				return fresh, nil
			})
			require.ErrorIs(t, RefreshImageWorkbenchAuth(c, lookup, nil, &config.Config{}), service.ErrImageTaskForbidden)
		})
	}
	previous, fresh := queuedAuthKey(), queuedAuthKey()
	previous.User.Role = service.RoleAdmin
	fresh.Group.ImageRateMultiplier = 2.5
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(context.WithValue(context.Background(), ctxkey.Group, previous.Group))
	c.Set(string(ContextKeyAPIKey), previous)
	c.Set(string(ContextKeySubscription), &service.UserSubscription{ID: 99})
	require.NoError(t, RefreshImageWorkbenchAuth(c, queuedKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return fresh, nil }), nil, &config.Config{}))
	key, ok := GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Same(t, fresh, key)
	role, ok := GetUserRoleFromContext(c)
	require.True(t, ok)
	require.Equal(t, service.RoleUser, role)
	require.Same(t, fresh.Group, c.Request.Context().Value(ctxkey.Group))
	subscription, _ := GetSubscriptionFromContext(c)
	require.Nil(t, subscription)
	subject, ok := GetAuthSubjectFromContext(c)
	require.True(t, ok)
	require.Equal(t, fresh.User.Concurrency, subject.Concurrency)
}
