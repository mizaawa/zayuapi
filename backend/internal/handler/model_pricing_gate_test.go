//go:build unit

package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newPricingGateTestOpenAIService() *service.OpenAIGatewayService {
	cfg := &config.Config{}
	billing := service.NewBillingService(cfg, nil)
	resolver := service.NewModelPricingResolver(nil, billing)
	return service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		billing, nil, nil, nil, nil, nil, nil, resolver, nil, nil, nil, nil,
	)
}

// grok-4.7-fast has no price in any source: once the pricing gate vetoed every
// candidate, the client gets an explicit 403 instead of a retryable 503, and
// the failure is not booked as a capacity shortage.
func TestClassifyNoAccountError_PricingGateBlockedReturns403(t *testing.T) {
	svc := newPricingGateTestOpenAIService()
	groupID := int64(9)
	apiKey := &service.APIKey{ID: 1, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, RateMultiplier: 1}}

	c := newTestGinContextWithRequest()
	ctx := svc.WithModelPricingGate(c.Request.Context(), apiKey, "grok-4.7-fast", service.ChannelMappingResult{MappedModel: "grok-4.7-fast"})
	vetoed, _ := service.OpenAIProfitControlVeto(ctx, &service.Account{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey})
	require.True(t, vetoed)
	c.Request = c.Request.WithContext(ctx)

	diag := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	cls := classifyNoAccountErrorFromGin(c, diag, apiKey, "grok-4.7-fast", "grok-4.7-fast", service.PlatformOpenAI)

	require.Equal(t, http.StatusForbidden, cls.Status)
	require.Equal(t, "permission_error", cls.ErrType)
	require.True(t, cls.ModelNotFound)
	require.Contains(t, cls.Message, `"grok-4.7-fast"`)
	require.Empty(t, diag.calls)
}

func TestClassifyNoAccountError_PricedModelKeepsCapacityClassification(t *testing.T) {
	svc := newPricingGateTestOpenAIService()
	groupID := int64(9)
	apiKey := &service.APIKey{ID: 1, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformGrok, RateMultiplier: 1}}

	c := newTestGinContextWithRequest()
	c.Request = c.Request.WithContext(svc.WithModelPricingGate(c.Request.Context(), apiKey, "grok-4.7", service.ChannelMappingResult{MappedModel: "grok-4.7"}))

	diag := &fakeDiagnoser{resp: service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}}
	cls := classifyNoAccountErrorFromGin(c, diag, apiKey, "grok-4.7", "grok-4.7", service.PlatformGrok)

	require.Equal(t, http.StatusServiceUnavailable, cls.Status)
	require.False(t, cls.ModelNotFound)
}

func TestOpenAIWSTurnHasBillablePartialUsage(t *testing.T) {
	firstTokenMs := 12
	streamed := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 10, OutputTokens: 5}, FirstTokenMs: &firstTokenMs}

	require.True(t, openAIWSTurnHasBillablePartialUsage(streamed, errors.New("upstream stream ended before terminal event")))
	require.False(t, openAIWSTurnHasBillablePartialUsage(streamed, nil), "successful turns are billed by the normal path")
	require.False(t, openAIWSTurnHasBillablePartialUsage(streamed, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}), "failover turns are replayed elsewhere")

	notStreamed := &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 10, OutputTokens: 5}}
	require.False(t, openAIWSTurnHasBillablePartialUsage(notStreamed, errors.New("boom")))

	noUsage := &service.OpenAIForwardResult{FirstTokenMs: &firstTokenMs}
	require.False(t, openAIWSTurnHasBillablePartialUsage(noUsage, errors.New("boom")))
}

func TestBillingErrorDetails_CustomVideoPricingUnavailableExplainsWhy(t *testing.T) {
	err := errors.Join(service.ErrCustomVideoPricingUnavailable, errors.New(`model "seedance-x" at 720p`))
	status, code, message, _ := billingErrorDetails(err)

	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "billing_error", code)
	require.Contains(t, message, "custom video pricing is not configured")
}
