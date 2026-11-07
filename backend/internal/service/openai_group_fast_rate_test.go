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
		wantTier                         string
		wantCustomerFactor               float64
		wantLogRate                      float64
	}{
		{name: "ordinary request", model: "gpt-5.4", enabled: true, fastRate: 3, wantCustomerFactor: 1.25, wantLogRate: 1.25},
		{name: "fast alias", model: "gpt-5.4", requested: "fast", enabled: true, fastRate: 3, wantTier: "priority", wantCustomerFactor: 3.75, wantLogRate: 3.75},
		{name: "fast with resolver and explicit priority prices", model: "gpt-5.1-codex", requested: "priority", enabled: true, useResolver: true, fastRate: 2.5, wantTier: "priority", wantCustomerFactor: 3.125, wantLogRate: 3.125},
		{name: "fast switch disabled", model: "gpt-5.4", requested: "priority", fastRate: 3, wantTier: "priority", wantCustomerFactor: 2.5, wantLogRate: 1.25},
		{name: "upstream downgrade", model: "gpt-5.4", requested: "fast", observed: "default", enabled: true, fastRate: 3, wantTier: "default", wantCustomerFactor: 1.25, wantLogRate: 1.25},
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
				RateMultiplier: 1.25, FastRateEnabled: tc.enabled, FastRateMultiplier: tc.fastRate}
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
			upstream, err := svc.billingService.CalculateCostWithServiceTier(tc.model, tokens, 1, tc.wantTier)
			require.NoError(t, err)
			wantLogTotal := upstream.TotalCost
			if tc.enabled && tc.wantTier == "priority" {
				wantLogTotal = base.TotalCost
				require.NotNil(t, usageRepo.lastLog.AccountStatsCost)
				require.InDelta(t, upstream.TotalCost, *usageRepo.lastLog.AccountStatsCost, 1e-10)
			}
			require.InDelta(t, wantLogTotal, usageRepo.lastLog.TotalCost, 1e-10)
			wantActual := base.TotalCost * tc.wantCustomerFactor
			if !tc.enabled && tc.wantTier == "priority" {
				wantActual = upstream.TotalCost * 1.25
			}
			require.InDelta(t, wantActual, usageRepo.lastLog.ActualCost, 1e-10)
			require.InDelta(t, wantActual, userRepo.lastAmount, 1e-10)
			require.InDelta(t, tc.wantLogRate, usageRepo.lastLog.RateMultiplier, 1e-10)
			require.InDelta(t, usageRepo.lastLog.TotalCost*usageRepo.lastLog.RateMultiplier,
				usageRepo.lastLog.ActualCost, 1e-10)
			require.Equal(t, optionalTrimmedStringPtr(tc.wantTier), usageRepo.lastLog.ServiceTier)
			require.Equal(t, 1, userRepo.deductCalls)
		})
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
			FastRateEnabled: true, FastRateMultiplier: 3.5},
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
