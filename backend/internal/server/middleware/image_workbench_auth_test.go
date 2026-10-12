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

func TestRefreshWorkbenchPrepaidTaskKeepsAuthorizationChecks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*service.APIKey)
		allowed bool
	}{
		{"funded exhausted key", func(k *service.APIKey) {
			k.User.Balance = 0
			k.Quota = 1
			k.QuotaUsed = 1
			k.Status = service.StatusAPIKeyQuotaExhausted
		}, true},
		{"disabled key", func(k *service.APIKey) { k.Status = service.StatusDisabled }, false},
		{"expired key", func(k *service.APIKey) { v := time.Now().Add(-time.Second); k.ExpiresAt = &v }, false},
		{"disabled user", func(k *service.APIKey) { k.User.Status = service.StatusDisabled }, false},
		{"revoked group", func(k *service.APIKey) { k.Group.ImageWorkbenchEnabled = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous, fresh := queuedAuthKey(), queuedAuthKey()
			tc.change(fresh)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			r := &service.MediaBillingReservation{ID: "media:server-receipt", WorkbenchKind: "image", GroupID: *previous.GroupID, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{UserID: previous.UserID, APIKeyID: previous.ID}}}
			c.Request = c.Request.WithContext(service.WithMediaWorkbenchPrepayment(c.Request.Context(), r))
			c.Set(string(ContextKeyAPIKey), previous)
			err := RefreshImageWorkbenchAuth(c, queuedKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return fresh, nil }), nil, &config.Config{})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	c.Request.Header.Set("X-Media-Prepaid", "true")
	c.Set("media_precharge", &service.MediaBillingReservation{ID: "forged"})
	previous, fresh := queuedAuthKey(), queuedAuthKey()
	fresh.User.Balance = 0
	c.Set(string(ContextKeyAPIKey), previous)
	require.Error(t, RefreshImageWorkbenchAuth(c, queuedKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) { return fresh, nil }), nil, &config.Config{}))
}
