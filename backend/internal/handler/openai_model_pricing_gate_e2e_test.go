//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const modelPricingGateUpstreamSSE = `data: {"type":"response.created","response":{"id":"resp_gate","model":"grok-4.7-fast","status":"in_progress","output":[]}}` + "\n\n" +
	`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"hi"}` + "\n\n" +
	`data: {"type":"response.completed","response":{"id":"resp_gate","model":"grok-4.7-fast","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":100,"total_tokens":1100}}}` + "\n\n"

// runModelPricingGateResponses sends /v1/responses for grok-4.7-fast through the
// real handler and gateway. The only account has no model mapping, so it would
// accept (and forward) any model name — the setup of the reported $0 incident.
func runModelPricingGateResponses(t *testing.T, groupPricing []service.ChannelModelPricing) (*httptest.ResponseRecorder, *partialUsageUpstream, *partialUsageBillingRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	upstream := &partialUsageUpstream{payload: modelPricingGateUpstreamSSE}
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
	billingRepo := &partialUsageBillingRepo{}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxLineSize = 1024 * 1024
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	account := service.Account{
		ID: 1, Name: "accepts-any-model", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-token", "base_url": "https://api.openai.com"},
	}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	// Eligibility runs in simple mode; the gateway itself bills in standard mode.
	billingCfg := *cfg
	billingCfg.RunMode = config.RunModeStandard
	billing := service.NewBillingService(&billingCfg, nil)
	gateway := service.NewOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}, usageRepo, billingRepo,
		nil, nil, nil, nil, &billingCfg, nil, nil, billing,
		nil, billingCache, upstream, &service.DeferredService{}, nil, nil,
		service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil,
	)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
	})
	pool.Stop()
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), pool, nil, nil, nil, cfg)

	groupID := int64(2)
	key := &service.APIKey{ID: 99, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, RateMultiplier: 1, ModelPricing: groupPricing},
		User:  &service.User{ID: 100, Status: service.StatusActive, Balance: 100},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses",
		strings.NewReader(`{"model":"grok-4.7-fast","stream":true,"input":"hello"}`)).WithContext(context.Background())
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})

	h.Responses(c)
	return recorder, upstream, billingRepo
}

func TestOpenAIResponses_UnpricedModelIsRefusedBeforeUpstream(t *testing.T) {
	recorder, upstream, billingRepo := runModelPricingGateResponses(t, nil)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "grok-4.7-fast")
	require.Contains(t, recorder.Body.String(), "no price configured")
	require.Zero(t, upstream.calls, "an unpriced model must never reach the upstream")
	require.Empty(t, billingRepo.commands)
}

func TestOpenAIResponses_PricedModelIsForwardedAndCharged(t *testing.T) {
	input, output := 2e-6, 6e-6
	recorder, upstream, billingRepo := runModelPricingGateResponses(t, []service.ChannelModelPricing{{
		Platform: service.PlatformOpenAI, Models: []string{"grok-4.7-fast"}, BillingMode: service.BillingModeToken,
		InputPrice: &input, OutputPrice: &output,
	}})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Len(t, billingRepo.commands, 1)
	// 1000 input tokens at $2/MTok + 100 output tokens at $6/MTok.
	require.InDelta(t, 1000*input+100*output, billingRepo.commands[0].BalanceCost, 1e-12)
}
