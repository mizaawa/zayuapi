package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RefreshVideoWorkbenchAuth(c *gin.Context, keys imageWorkbenchKeyLookup, subscriptions *service.SubscriptionService, cfg *config.Config) error {
	previous, ok := GetAPIKeyFromContext(c)
	if !ok || previous == nil || keys == nil {
		return service.ErrVideoTaskForbidden
	}
	fresh, err := keys.GetByKeyFresh(c.Request.Context(), previous.Key)
	if err != nil || fresh == nil || fresh.ID != previous.ID || fresh.UserID != previous.UserID || !fresh.IsActive() ||
		fresh.User == nil || fresh.User.ID != fresh.UserID || !fresh.User.IsActive() || fresh.Group == nil || previous.Group == nil ||
		fresh.Group.ID != previous.Group.ID || !service.GroupAllowsVideoWorkbench(fresh.Group) {
		return service.ErrVideoTaskForbidden
	}
	if len(fresh.IPWhitelist) > 0 || len(fresh.IPBlacklist) > 0 {
		trustForwarded := cfg != nil && cfg.TrustForwardedIPForAPIKeyACL()
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(ip.GetSecurityClientIP(c, trustForwarded), fresh.CompiledIPWhitelist, fresh.CompiledIPBlacklist)
		if !allowed {
			return service.ErrVideoTaskForbidden
		}
	}
	if err := setAPIKeyFailoverBilling(c, fresh, subscriptions, cfg); err != nil {
		return service.ErrVideoTaskForbidden
	}
	c.Set(string(ContextKeyUser), authSubjectForAPIKey(fresh))
	c.Set(string(ContextKeyUserRole), fresh.User.Role)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserID, fresh.UserID))
	SetOpsFallbackAPIKey(c, fresh)
	return nil
}
