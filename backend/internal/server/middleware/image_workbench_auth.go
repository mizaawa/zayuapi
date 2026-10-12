package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type imageWorkbenchKeyLookup interface {
	GetByKeyFresh(context.Context, string) (*service.APIKey, error)
}

func RefreshImageWorkbenchAuth(c *gin.Context, keys imageWorkbenchKeyLookup, subscriptions *service.SubscriptionService, cfg *config.Config) error {
	previous, ok := GetAPIKeyFromContext(c)
	if !ok || previous == nil || keys == nil {
		return service.ErrImageTaskForbidden
	}
	fresh, err := keys.GetByKeyFresh(c.Request.Context(), previous.Key)
	if err != nil {
		return service.ErrImageTaskForbidden
	}
	if fresh == nil || fresh.ID != previous.ID || fresh.UserID != previous.UserID ||
		(!fresh.IsActive() && (!service.MediaWorkbenchPrepaid(c.Request.Context(), fresh) || fresh.Status != service.StatusAPIKeyQuotaExhausted)) ||
		fresh.User == nil || fresh.User.ID != fresh.UserID || !fresh.User.IsActive() || fresh.Group == nil || previous.Group == nil ||
		fresh.Group.ID != previous.Group.ID || fresh.Group.Platform != previous.Group.Platform ||
		!fresh.Group.ImageWorkbenchEnabled || !service.GroupAllowsImageGeneration(fresh.Group) {
		return service.ErrImageTaskForbidden
	}
	if len(fresh.IPWhitelist) > 0 || len(fresh.IPBlacklist) > 0 {
		trustForwarded := cfg != nil && cfg.TrustForwardedIPForAPIKeyACL()
		clientIP := ip.GetSecurityClientIP(c, trustForwarded)
		allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, fresh.CompiledIPWhitelist, fresh.CompiledIPBlacklist)
		if !allowed {
			return service.ErrImageTaskForbidden
		}
	}
	// Reuse the gateway's current group access, quota, expiry, subscription and
	// balance checks. This also clears any stale subscription and group values.
	if err := setAPIKeyFailoverBilling(c, fresh, subscriptions, cfg); err != nil {
		return service.ErrImageTaskForbidden
	}
	c.Set(string(ContextKeyUser), authSubjectForAPIKey(fresh))
	c.Set(string(ContextKeyUserRole), fresh.User.Role)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.UserID, fresh.UserID))
	SetOpsFallbackAPIKey(c, fresh)
	return nil
}
