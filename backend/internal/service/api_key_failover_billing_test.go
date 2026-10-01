package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type failoverBillingRateRepoStub struct {
	UserGroupRateRepository
	rates    map[int64]*float64
	groupIDs []int64
}

func (r *failoverBillingRateRepoStub) GetByUserAndGroup(_ context.Context, _, groupID int64) (*float64, error) {
	r.groupIDs = append(r.groupIDs, groupID)
	return r.rates[groupID], nil
}

func (r *failoverBillingRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

func TestAPIKeyFailoverBillingUsesEffectiveGroup(t *testing.T) {
	for _, engine := range []string{"OpenAI", "Anthropic"} {
		for _, phase := range []string{"initial_switch", "cooldown_request"} {
			for _, billing := range []struct {
				name     string
				primary  string
				fallback string
			}{
				{"balance_to_balance", SubscriptionTypeStandard, SubscriptionTypeStandard},
				{"subscription_to_balance", SubscriptionTypeSubscription, SubscriptionTypeStandard},
				{"balance_to_subscription", SubscriptionTypeStandard, SubscriptionTypeSubscription},
				{"subscription_to_subscription", SubscriptionTypeSubscription, SubscriptionTypeSubscription},
			} {
				for _, userRate := range []bool{false, true} {
					rateName := "group_rate"
					if userRate {
						rateName = "user_group_rate"
					}
					t.Run(engine+"/"+phase+"/"+billing.name+"/"+rateName, func(t *testing.T) {
						keyService, keys, groups, users := newFailoverTestService()
						if engine == "Anthropic" {
							groups.groups[10].Platform = PlatformAnthropic
							groups.groups[20].Platform = PlatformAnthropic
						}
						groups.groups[10].SubscriptionType = billing.primary
						groups.groups[10].RateMultiplier = 0.1
						groups.groups[20].SubscriptionType = billing.fallback
						groups.groups[20].RateMultiplier = 4
						keys.key.User = users.user
						keys.key.Quota = 100
						keys.key.RateLimit5h = 50
						cached := *keys.key
						ctx := context.WithValue(t.Context(), ctxkey.ClientRequestID, "failover-billing")
						var effective *APIKey
						var err error
						if phase == "initial_switch" {
							effective, err = keyService.PrepareAPIKeyFailover(ctx, &cached)
							require.NoError(t, err)
							require.NoError(t, keyService.StartAPIKeyFailoverCooldown(ctx, effective))
						} else {
							until := time.Now().Add(time.Minute)
							keys.key.FailoverCooldownUntil = &until
							effective, err = keyService.ResolveAPIKeyFailover(ctx, &cached)
							require.NoError(t, err)
						}
						require.True(t, effective.FailoverActive)
						require.Equal(t, int64(20), *effective.GroupID)
						wantMultiplier := float64(4)
						primaryRate, fallbackRate := 0.2, 0.75
						rateRepo := &failoverBillingRateRepoStub{rates: map[int64]*float64{10: &primaryRate}}
						if userRate {
							rateRepo.rates[20] = &fallbackRate
							wantMultiplier = fallbackRate
						}
						usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
						billingRepo := &openAIRecordUsageBillingRepoStub{}
						openai := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo,
							&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, rateRepo)
						accountMultiplier := 1.5
						account := &Account{ID: 30, Type: AccountTypeAPIKey, Platform: effective.Group.Platform,
							RateMultiplier: &accountMultiplier, Extra: map[string]any{"quota_limit": float64(100)}}
						var subscription *UserSubscription
						if billing.fallback == SubscriptionTypeSubscription {
							subscription = &UserSubscription{ID: 40, UserID: effective.UserID, GroupID: 20, Status: StatusActive}
						}
						quotaService := &openAIRecordUsageAPIKeyQuotaStub{}
						if engine == "OpenAI" {
							err = openai.RecordUsage(ctx, &OpenAIRecordUsageInput{
								Result: &OpenAIForwardResult{RequestID: "upstream-fallback", Model: "gpt-5.1",
									Usage: OpenAIUsage{InputTokens: 1200, OutputTokens: 300}},
								APIKey: effective, User: effective.User, Account: account, Subscription: subscription,
								APIKeyService: quotaService, RequestPayloadHash: "same-logical-request",
							})
						} else {
							anthropic := &GatewayService{
								cfg: openai.cfg, billingService: openai.billingService,
								usageLogRepo: usageRepo, usageBillingRepo: billingRepo,
								userRepo: openai.userRepo, userSubRepo: openai.userSubRepo,
								userGroupRateResolver: openai.userGroupRateResolver,
								billingCacheService:   openai.billingCacheService, deferredService: openai.deferredService,
							}
							err = anthropic.RecordUsage(ctx, &RecordUsageInput{
								Result: &ForwardResult{RequestID: "upstream-fallback", Model: "claude-sonnet-4-5",
									Usage: ClaudeUsage{InputTokens: 1200, OutputTokens: 300}},
								APIKey: effective, User: effective.User, Account: account, Subscription: subscription,
								APIKeyService: quotaService, RequestPayloadHash: "same-logical-request",
							})
						}
						require.NoError(t, err)
						require.Equal(t, []int64{20}, rateRepo.groupIDs)
						require.Equal(t, 1, usageRepo.calls)
						require.Equal(t, 1, billingRepo.calls)
						log, cmd := usageRepo.lastLog, billingRepo.lastCmd
						require.NotNil(t, log)
						require.NotNil(t, cmd)
						require.Equal(t, int64(20), *log.GroupID)
						require.Equal(t, wantMultiplier, log.RateMultiplier)
						require.Positive(t, log.TotalCost)
						wantCost := QuantizeUsageBillingAmount(log.TotalCost * wantMultiplier)
						require.InDelta(t, wantCost, log.ActualCost, 1e-12)
						require.Equal(t, cached.ID, cmd.APIKeyID)
						require.Equal(t, cached.UserID, cmd.UserID)
						require.Equal(t, account.ID, cmd.AccountID)
						require.Equal(t, "client:failover-billing", cmd.RequestID)
						require.Equal(t, "same-logical-request", cmd.RequestPayloadHash)
						require.Equal(t, wantCost, cmd.APIKeyQuotaCost)
						require.Equal(t, wantCost, cmd.APIKeyRateLimitCost)
						require.Equal(t, QuantizeUsageBillingAmount(log.TotalCost*accountMultiplier), cmd.AccountQuotaCost)
						if subscription != nil {
							require.Equal(t, BillingTypeSubscription, log.BillingType)
							require.Equal(t, subscription.ID, *log.SubscriptionID)
							require.Equal(t, subscription.ID, *cmd.SubscriptionID)
							require.Equal(t, wantCost, cmd.SubscriptionCost)
							require.Zero(t, cmd.BalanceCost)
						} else {
							require.Equal(t, BillingTypeBalance, log.BillingType)
							require.Nil(t, log.SubscriptionID)
							require.Nil(t, cmd.SubscriptionID)
							require.Equal(t, wantCost, cmd.BalanceCost)
							require.Zero(t, cmd.SubscriptionCost)
						}
						require.Equal(t, int64(10), *cached.GroupID)
						require.Equal(t, int64(10), *keys.key.GroupID)
						require.Equal(t, float64(0.1), cached.Group.RateMultiplier)
						require.False(t, cached.FailoverActive)
					})
				}
			}
		}
	}
}
