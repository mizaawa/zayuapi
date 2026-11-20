package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIGroupFastRateRecordUsage(t *testing.T) {
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 100}
	for _, tc := range []struct {
		name, model, requested, observed string
		enabled, useResolver             bool
		fastRate                         float64
		force                            string
		wantTier                         string
		wantCustomerFactor               float64
		wantLogRate                      float64
	}{
		{name: "ordinary request", model: "gpt-5.4", enabled: true, fastRate: 3, wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "fast alias", model: "gpt-5.4", requested: "fast", enabled: true, fastRate: 3, wantTier: "priority", wantCustomerFactor: 3.75, wantLogRate: 3.75},
		{name: "fast with resolver and explicit priority prices", model: "gpt-5.1-codex", requested: "priority", enabled: true, useResolver: true, fastRate: 2.5, wantTier: "priority", wantCustomerFactor: 3.125, wantLogRate: 3.125},
		{name: "fast switch disabled", model: "gpt-5.4", requested: "priority", fastRate: 3, wantTier: "priority", wantCustomerFactor: 2.5, wantLogRate: 1.25},
		{name: "voluntary UltraFast keeps catalog pricing", model: "gpt-5.4", requested: "ultrafast", wantTier: "ultrafast", wantCustomerFactor: 7.5, wantLogRate: 1.25},
		{name: "forced Fast without surcharge uses standard not catalog priority prices", model: "gpt-5.1-codex", force: "fast", requested: "priority", observed: "priority", fastRate: 4.75, useResolver: true, wantTier: "priority", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "forced Fast without surcharge ignores stored rate without resolver", model: "gpt-5.4", force: "fast", requested: "priority", fastRate: 4.75, wantTier: "priority", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "forced UltraFast without surcharge ignores stored and catalog rates", model: "gpt-5.4", force: "ultrafast", requested: "ultrafast", observed: "ultrafast", fastRate: 4.75, wantTier: "ultrafast", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "forced UltraFast without surcharge after upstream downgrade", model: "gpt-5.4", force: "ultrafast", requested: "ultrafast", observed: "default", useResolver: true, wantTier: "ultrafast", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "forced Fast with independent pricing at one keeps explicit badge", model: "gpt-5.1-codex", force: "fast", requested: "priority", observed: "priority", enabled: true, fastRate: 1, wantTier: "priority", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "upstream downgrade", model: "gpt-5.4", requested: "fast", observed: "default", enabled: true, fastRate: 3, wantTier: "default", wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "automatic UltraFast to Fast downgrade", model: "gpt-5.4", requested: "ultrafast", observed: "fast", enabled: true, fastRate: 3, wantTier: "priority", wantCustomerFactor: 3.75, wantLogRate: 3.75},
		{name: "forced UltraFast retains contracted rate", model: "gpt-5.4", force: "ultrafast", requested: "ultrafast", observed: "default", enabled: true, useResolver: true, wantTier: "ultrafast", wantCustomerFactor: 8.125, wantLogRate: 8.125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			if tc.useResolver {
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			}
			groupID := int64(45)
			group := &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
				RateMultiplier: 1.25, FastRateEnabled: tc.enabled, FastRateMultiplier: tc.fastRate, UltraFastRateMultiplier: 6.5, ForcedFastMode: tc.force}
			account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "fast-rate-test", Model: tc.model,
					ServiceTier: optionalTrimmedStringPtr(tc.requested), UpstreamServiceTier: optionalTrimmedStringPtr(tc.observed),
					Usage: OpenAIUsage{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens}, Duration: time.Second,
				},
				APIKey: &APIKey{ID: 2, GroupID: &groupID, Group: group}, User: &User{ID: 1}, Account: account,
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			base, err := svc.billingService.CalculateCost(tc.model, tokens, 1)
			require.NoError(t, err)
			upstreamTier := tc.wantTier
			if tc.force != "" && tc.observed != "" {
				upstreamTier = tc.observed
			}
			upstream, err := svc.billingService.CalculateCostWithServiceTier(tc.model, tokens, 1, upstreamTier)
			require.NoError(t, err)
			wantLogTotal := upstream.TotalCost
			if (tc.enabled || tc.force != "") && isOpenAISpeedTier(tc.wantTier) {
				wantLogTotal = base.TotalCost
				require.NotNil(t, usageRepo.lastLog.AccountStatsCost)
				require.InDelta(t, upstream.TotalCost, *usageRepo.lastLog.AccountStatsCost, 1e-10)
			}
			require.InDelta(t, wantLogTotal, usageRepo.lastLog.TotalCost, 1e-10)
			wantActual := base.TotalCost * tc.wantCustomerFactor
			if !tc.enabled && tc.force == "" && tc.wantTier == "priority" {
				wantActual = upstream.TotalCost * 1.25
			}
			require.InDelta(t, wantActual, usageRepo.lastLog.ActualCost, 1e-10)
			require.InDelta(t, wantActual, userRepo.lastAmount, 1e-10)
			require.InDelta(t, tc.wantLogRate, usageRepo.lastLog.RateMultiplier, 1e-10)
			if tc.enabled && isOpenAISpeedTier(tc.wantTier) {
				require.InDelta(t, tc.wantLogRate/1.25, *usageRepo.lastLog.ServiceTierMultiplier, 1e-10)
			} else {
				require.Nil(t, usageRepo.lastLog.ServiceTierMultiplier)
			}
			require.InDelta(t, usageRepo.lastLog.TotalCost*usageRepo.lastLog.RateMultiplier,
				usageRepo.lastLog.ActualCost, 1e-10)
			require.Equal(t, optionalTrimmedStringPtr(tc.wantTier), usageRepo.lastLog.ServiceTier)
			require.Equal(t, 1, userRepo.deductCalls)
		})
	}
}

func TestForcedSpeedSeparatesCustomerAndAccountQuota(t *testing.T) {
	for _, mode := range []string{"fast", "ultrafast"} {
		for _, subscription := range []string{SubscriptionTypeStandard, SubscriptionTypeSubscription} {
			t.Run(mode+"/"+subscription, func(t *testing.T) {
				s := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				s.billingCacheService.cfg = s.cfg
				g := &Group{ID: 41, Platform: PlatformOpenAI, ForcedFastMode: mode, FastRateMultiplier: 4, UltraFastRateMultiplier: 10, RateMultiplier: 1.5, SubscriptionType: subscription}
				key := &APIKey{ID: 42, Group: g, GroupID: &g.ID, Quota: 100, RateLimit5h: 50}
				account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{"quota_limit": 100.0}}
				tier := g.ForcedOpenAIServiceTier()
				const model = "gpt-5.4"
				var q *MediaBillingQuote
				input := &OpenAIRecordUsageInput{APIKey: key, User: &User{ID: 44}, Account: account, APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{}, MediaQuote: &q,
					Result: &OpenAIForwardResult{RequestID: "quota-split", Model: model, ServiceTier: &tier, UpstreamServiceTier: &tier, Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 100}}}
				if subscription == SubscriptionTypeSubscription {
					input.Subscription = &UserSubscription{ID: 45}
				}
				require.NoError(t, s.RecordUsage(context.Background(), input))
				tokens := UsageTokens{InputTokens: 1000, OutputTokens: 100}
				base, err := s.billingService.CalculateCost(model, tokens, g.RateMultiplier)
				require.NoError(t, err)
				upstream, err := s.billingService.CalculateCostWithServiceTier(model, tokens, 1, tier)
				require.NoError(t, err)
				require.Greater(t, upstream.TotalCost, base.ActualCost)
				require.InDelta(t, upstream.TotalCost, q.Command.AccountQuotaCost, 1e-9)
				require.InDelta(t, base.ActualCost, q.Command.APIKeyQuotaCost, 1e-9)
				require.InDelta(t, base.ActualCost, q.Command.APIKeyRateLimitCost, 1e-9)
				if subscription == SubscriptionTypeSubscription {
					require.InDelta(t, base.ActualCost, q.Command.SubscriptionCost, 1e-9)
					require.Zero(t, q.Command.BalanceCost)
					require.Zero(t, q.Command.UserPlatformQuotaCost)
				} else {
					require.InDelta(t, base.ActualCost, q.Command.BalanceCost, 1e-9)
					require.InDelta(t, base.ActualCost, q.Command.UserPlatformQuotaCost, 1e-9)
					require.Zero(t, q.Command.SubscriptionCost)
				}
				require.Nil(t, q.Usage.ServiceTierMultiplier)
			})
		}
	}
}

func TestOpenAIGroupFastRatePolicyFilteredRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
		ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter, Scope: BetaPolicyScopeAll,
	}}}
	policyService := newOpenAIGatewayServiceWithSettings(t, settings)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.settingService = policyService.settingService
	groupID := int64(45)
	group := &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true,
		RateMultiplier: 1.25, FastRateEnabled: true, FastRateMultiplier: 3}
	user := &User{ID: 1}
	apiKey := &APIKey{ID: 2, GroupID: &groupID, Group: group, UserID: user.ID, User: user}
	account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key"}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"fast-filter-test"}},
		Body: io.NopCloser(strings.NewReader(
			`data: {"type":"response.completed","response":{"id":"resp_fast","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":100,"total_tokens":1100}}}` + "\n\n")),
	}}
	svc.httpUpstream = upstream
	body := []byte(`{"model":"gpt-5.4","service_tier":"fast","input":"hello","stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key", apiKey)
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.Nil(t, result.ServiceTier)
	require.False(t, gjson.GetBytes(upstream.lastBody, "service_tier").Exists())
	require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: result, APIKey: apiKey, User: user, Account: account,
	}))
	require.NotNil(t, usageRepo.lastLog)
	require.Nil(t, usageRepo.lastLog.ServiceTier)
	require.InDelta(t, 1.25, usageRepo.lastLog.RateMultiplier, 1e-10)
	base, err := svc.billingService.CalculateCost("gpt-5.4", UsageTokens{InputTokens: 1000, OutputTokens: 100}, 1.25)
	require.NoError(t, err)
	require.InDelta(t, base.ActualCost, usageRepo.lastLog.ActualCost, 1e-10)
	require.InDelta(t, base.ActualCost, userRepo.lastAmount, 1e-10)
}

func TestOpenAIGroupFastRateAuthSnapshot(t *testing.T) {
	groupID := int64(45)
	keyService := &APIKeyService{}
	snapshot := keyService.snapshotFromAPIKey(context.Background(), &APIKey{
		ID: 2, UserID: 1, User: &User{ID: 1}, GroupID: &groupID,
		Group: &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive,
			FastRateEnabled: true, FastRateMultiplier: 3.5, UltraFastRateMultiplier: 7.5, ForcedFastMode: "ultrafast"},
	})
	require.NotNil(t, snapshot)
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var decoded APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(data, &decoded))
	restored := keyService.snapshotToAPIKey("test-key", &decoded)
	require.NotNil(t, restored.Group)
	require.True(t, restored.Group.FastRateEnabled)
	require.Equal(t, 3.5, restored.Group.FastRateMultiplier)
	require.Equal(t, 7.5, restored.Group.UltraFastRateMultiplier)
	require.Equal(t, "ultrafast", restored.Group.ForcedFastMode)
}

func TestOpenAIGroupFastRateMediaTokenSnapshot(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{},
		&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	resolver := NewModelPricingResolver(nil, svc.billingService)
	pricing := &MediaImagePricing{
		Model: "gpt-5.4", TokenMultiplier: 1.25, ServiceTier: "priority", FastRateMultiplier: 3,
		Resolved: resolver.Resolve(context.Background(), PricingInput{Model: "gpt-5.4"}),
	}
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 100}
	cost, err := pricing.calculate(svc.billingService, &OpenAIForwardResult{ImageCount: 1}, tokens)
	require.NoError(t, err)
	base, err := svc.billingService.CalculateCost("gpt-5.4", tokens, 1)
	require.NoError(t, err)
	priority, err := svc.billingService.CalculateCostWithServiceTier("gpt-5.4", tokens, 1, "priority")
	require.NoError(t, err)
	require.InDelta(t, priority.TotalCost, cost.TotalCost, 1e-10)
	require.NotNil(t, cost.CustomerBase)
	require.InDelta(t, base.TotalCost, cost.CustomerBase.TotalCost, 1e-10)
	require.InDelta(t, base.TotalCost*3.75, cost.ActualCost, 1e-10)
}
