//go:build unit

package middleware

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverRebindsBillingContext(t *testing.T) {
	for _, test := range []struct {
		name     string
		primary  string
		fallback string
		invalid  string
	}{
		{"balance_to_balance", service.SubscriptionTypeStandard, service.SubscriptionTypeStandard, ""},
		{"subscription_to_balance", service.SubscriptionTypeSubscription, service.SubscriptionTypeStandard, ""},
		{"balance_to_subscription", service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription, ""},
		{"subscription_to_subscription", service.SubscriptionTypeSubscription, service.SubscriptionTypeSubscription, ""},
		{"missing_subscription", service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription, "missing"},
		{"expired_subscription", service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription, "expired"},
		{"exhausted_subscription", service.SubscriptionTypeStandard, service.SubscriptionTypeSubscription, "limit"},
		{"insufficient_balance", service.SubscriptionTypeSubscription, service.SubscriptionTypeStandard, "balance"},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := failoverTestKey()
			key.FailoverMaxRetries = 1
			key.Group.SubscriptionType = test.primary
			if test.invalid == "balance" {
				key.User.Balance = 0
			}
			stub := &failoverRoutingStub{target: &service.Group{
				ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive,
				SubscriptionType: test.fallback, RateMultiplier: 4,
			}}
			now := time.Now()
			primarySub := &service.UserSubscription{ID: 100, UserID: key.UserID, GroupID: 1}
			fallbackSub := &service.UserSubscription{
				ID: 200, UserID: key.UserID, GroupID: 2, Status: service.SubscriptionStatusActive,
				ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now,
				WeeklyWindowStart: &now, MonthlyWindowStart: &now,
			}
			if test.invalid == "expired" {
				fallbackSub.ExpiresAt = now.Add(-time.Hour)
			}
			if test.invalid == "limit" {
				limit := float64(1)
				stub.target.DailyLimitUSD = &limit
				fallbackSub.DailyUsageUSD = 2
			}
			subscriptionLookups := 0
			repo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
				subscriptionLookups++
				require.Equal(t, key.UserID, userID)
				require.Equal(t, int64(2), groupID)
				if test.invalid == "missing" {
					return nil, service.ErrSubscriptionNotFound
				}
				return fallbackSub, nil
			}}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			subscriptions := service.NewSubscriptionService(nil, repo, nil, nil, cfg)
			t.Cleanup(subscriptions.Stop)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), key)
				if test.primary == service.SubscriptionTypeSubscription {
					c.Set(string(ContextKeySubscription), primarySub)
				}
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
				c.Next()
			})
			var groups []int64
			router.POST("/v1/responses", APIKeyFailover(stub, subscriptions, cfg), func(c *gin.Context) {
				current, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				groups = append(groups, *current.GroupID)
				if *current.GroupID == 1 {
					markFailoverTestFailure(c, http.StatusServiceUnavailable, `{"error":"upstream unavailable"}`)
					return
				}
				sub, _ := GetSubscriptionFromContext(c)
				if test.fallback == service.SubscriptionTypeSubscription {
					require.Equal(t, fallbackSub, sub)
				} else {
					require.Nil(t, sub)
				}
				group, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group)
				require.True(t, ok)
				require.Same(t, stub.target, group)
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})
			response := failoverTestResponse(router)
			if test.invalid != "" {
				require.Equal(t, http.StatusServiceUnavailable, response.Code)
				require.Equal(t, []int64{1}, groups)
				require.Zero(t, stub.activations)
			} else {
				require.Equal(t, http.StatusOK, response.Code)
				require.Equal(t, []int64{1, 2}, groups)
				require.Equal(t, 1, stub.activations)
			}
			if test.fallback == service.SubscriptionTypeSubscription {
				require.Equal(t, 1, subscriptionLookups)
			} else {
				require.Zero(t, subscriptionLookups)
			}
			require.Equal(t, int64(1), *key.GroupID)
		})
	}
}
