package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_GroupModelPricingBillsUnknownModels(t *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformOpenAI, PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			inputPrice, outputPrice := 2e-6, 6e-6
			group := &Group{
				ID:             71,
				Platform:       platform,
				RateMultiplier: 1.5,
				ModelPricing: []ChannelModelPricing{{
					Platform:    platform,
					Models:      []string{"grok-4.7"},
					BillingMode: BillingModeToken,
					InputPrice:  &inputPrice,
					OutputPrice: &outputPrice,
				}},
			}

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "group-model-pricing-" + platform,
					Model:     "grok-4.7",
					Usage:     OpenAIUsage{InputTokens: 1200, OutputTokens: 300},
					Duration:  time.Second,
				},
				APIKey:  &APIKey{ID: 72, GroupID: &group.ID, Group: group},
				User:    &User{ID: 73},
				Account: &Account{ID: 74, Platform: platform, Type: AccountTypeAPIKey},
			})

			require.NoError(t, err)
			require.Equal(t, 1, usageRepo.calls)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, 0.0024, usageRepo.lastLog.InputCost, 1e-12)
			require.InDelta(t, 0.0018, usageRepo.lastLog.OutputCost, 1e-12)
			require.InDelta(t, 0.0042, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.0063, usageRepo.lastLog.ActualCost, 1e-12)
			require.Equal(t, 1, userRepo.deductCalls)
			require.InDelta(t, 0.0063, userRepo.lastAmount, 1e-12)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_CustomDeepSeekGroupPriceIsDeducted(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	groupID := int64(711)
	inputPrice, outputPrice := 4e-6, 10e-6
	group := &Group{
		ID:             groupID,
		Platform:       PlatformComposite,
		RateMultiplier: 1.5,
		ModelPricing: []ChannelModelPricing{{
			Platform:    PlatformCustom,
			Models:      []string{"deepseek-v4-pro"},
			BillingMode: BillingModeToken,
			InputPrice:  &inputPrice,
			OutputPrice: &outputPrice,
		}},
	}

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "custom-deepseek-group-pricing",
			Model:         "deepseek-v4-pro",
			UpstreamModel: "deepseek-v4-pro",
			Usage:         OpenAIUsage{InputTokens: 1200, OutputTokens: 300},
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 712, GroupID: &groupID, Group: group},
		User:    &User{ID: 713},
		Account: &Account{ID: 714, Platform: PlatformCustom, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 0.0048, usageRepo.lastLog.InputCost, 1e-12)
	require.InDelta(t, 0.003, usageRepo.lastLog.OutputCost, 1e-12)
	require.InDelta(t, 0.0078, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.0117, usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, userRepo.deductCalls)
	require.InDelta(t, 0.0117, userRepo.lastAmount, 1e-12)
}

func TestOpenAIGatewayServiceRecordUsage_GroupModelPricingHonorsBillingModelSource(t *testing.T) {
	for _, tt := range []struct {
		name               string
		billingModelSource string
		wantTotal          float64
	}{
		{name: "requested alias", billingModelSource: BillingModelSourceRequested, wantTotal: 0.002},
		{name: "channel mapped alias", billingModelSource: BillingModelSourceChannelMapped, wantTotal: 0.004},
		{name: "upstream model", billingModelSource: BillingModelSourceUpstream, wantTotal: 0.008},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			requestedPrice, mappedPrice, upstreamPrice := 1e-6, 2e-6, 4e-6
			group := &Group{
				ID:             81,
				Platform:       PlatformGrok,
				RateMultiplier: 1.5,
				ModelPricing: []ChannelModelPricing{
					{Models: []string{"public-grok-alias"}, InputPrice: &requestedPrice, OutputPrice: &requestedPrice},
					{Models: []string{"mapped-grok-alias"}, InputPrice: &mappedPrice, OutputPrice: &mappedPrice},
					{Models: []string{"grok-4.7"}, InputPrice: &upstreamPrice, OutputPrice: &upstreamPrice},
				},
			}

			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:     "group-model-pricing-alias",
					Model:         "public-grok-alias",
					BillingModel:  "grok-4.7",
					UpstreamModel: "grok-4.7",
					Usage:         OpenAIUsage{InputTokens: 1200, OutputTokens: 800},
					Duration:      time.Second,
				},
				APIKey:  &APIKey{ID: 82, GroupID: &group.ID, Group: group},
				User:    &User{ID: 83},
				Account: &Account{ID: 84, Platform: PlatformGrok, Type: AccountTypeAPIKey},
				ChannelUsageFields: ChannelUsageFields{
					OriginalModel:      "public-grok-alias",
					ChannelMappedModel: "mapped-grok-alias",
					BillingModelSource: tt.billingModelSource,
				},
			})

			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, tt.wantTotal, usageRepo.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tt.wantTotal*1.5, usageRepo.lastLog.ActualCost, 1e-12)
			require.Equal(t, 1, userRepo.deductCalls)
			require.InDelta(t, tt.wantTotal*1.5, userRepo.lastAmount, 1e-12)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_GroupModelPricingFallbackCandidate(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	inputPrice, outputPrice := 2e-6, 6e-6
	group := &Group{
		ID:             91,
		Platform:       PlatformGrok,
		RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Models:      []string{"grok-4.7"},
			InputPrice:  &inputPrice,
			OutputPrice: &outputPrice,
		}},
	}

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "group-model-pricing-fallback",
			Model:         "unpriced-public-alias",
			UpstreamModel: "grok-4.7",
			Usage:         OpenAIUsage{InputTokens: 1200, OutputTokens: 300},
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 92, GroupID: &group.ID, Group: group},
		User:    &User{ID: 93},
		Account: &Account{ID: 94, Platform: PlatformGrok, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "unpriced-public-alias", usageRepo.lastLog.Model)
	require.InDelta(t, 0.0042, usageRepo.lastLog.TotalCost, 1e-12)
	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd)
	require.InDelta(t, 0.0042, billingRepo.lastCmd.BalanceCost, 1e-12)
}

func TestOpenAIGatewayServiceRecordUsage_GroupModelPricingPerRequest(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	price := 0.25
	group := &Group{
		ID:             101,
		Platform:       PlatformGrok,
		RateMultiplier: 1.5,
		ModelPricing: []ChannelModelPricing{{
			Models:          []string{"grok-4.7"},
			BillingMode:     BillingModePerRequest,
			PerRequestPrice: &price,
		}},
	}

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "group-model-pricing-per-request",
			Model:     "grok-4.7",
			Duration:  time.Second,
		},
		APIKey:  &APIKey{ID: 102, GroupID: &group.ID, Group: group},
		User:    &User{ID: 103},
		Account: &Account{ID: 104, Platform: PlatformGrok, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModePerRequest), *usageRepo.lastLog.BillingMode)
	require.InDelta(t, 0.25, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.375, usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, userRepo.deductCalls)
	require.InDelta(t, 0.375, userRepo.lastAmount, 1e-12)
}

func TestGatewayServiceCalculateRecordUsageCost_GroupModelPricingAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformOpenAI, PlatformCustom, PlatformAnthropic, PlatformGemini, PlatformAntigravity} {
		t.Run(platform, func(t *testing.T) {
			billingService := NewBillingService(&config.Config{}, nil)
			svc := &GatewayService{
				billingService: billingService,
				resolver:       NewModelPricingResolver(nil, billingService),
			}
			inputPrice, outputPrice := 2e-6, 6e-6
			group := &Group{
				ID:       111,
				Platform: platform,
				ModelPricing: []ChannelModelPricing{{
					Platform:    platform,
					Models:      []string{"grok-4.7"},
					BillingMode: BillingModeToken,
					InputPrice:  &inputPrice,
					OutputPrice: &outputPrice,
				}},
			}

			cost := svc.calculateRecordUsageCost(
				context.Background(),
				&ForwardResult{Model: "grok-4.7", Usage: ClaudeUsage{InputTokens: 1200, OutputTokens: 300}},
				&APIKey{GroupID: &group.ID, Group: group},
				"grok-4.7",
				1.5,
				1,
				&recordUsageOpts{},
			)

			require.NotNil(t, cost)
			require.Equal(t, string(BillingModeToken), cost.BillingMode)
			require.InDelta(t, 0.0042, cost.TotalCost, 1e-12)
			require.InDelta(t, 0.0063, cost.ActualCost, 1e-12)
		})
	}
}

func TestModelPricingResolver_GroupModelPricingOverridesChannelAndGlobal(t *testing.T) {
	inputPrice, outputPrice := 2e-6, 6e-6
	group := &Group{
		ID:       121,
		Platform: PlatformOpenAI,
		ModelPricing: []ChannelModelPricing{{
			Models:      []string{"gpt-5.1"},
			InputPrice:  &inputPrice,
			OutputPrice: &outputPrice,
		}},
	}
	resolver := newOpenAITokenImageChannelPricingResolverForTest(t, group.ID, "gpt-5.1")

	resolved := resolver.Resolve(context.Background(), PricingInput{
		Model:   "gpt-5.1",
		GroupID: &group.ID,
		Group:   group,
	})

	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.NotNil(t, resolved.BasePricing)
	require.Equal(t, inputPrice, resolved.BasePricing.InputPricePerToken)
	require.Equal(t, outputPrice, resolved.BasePricing.OutputPricePerToken)
}
