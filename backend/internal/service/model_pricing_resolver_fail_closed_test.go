//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func calculateWithChannelPricing(t *testing.T, pricing []ChannelModelPricing, input CostInput) (*CostBreakdown, error) {
	t.Helper()
	r := newResolverWithChannel(t, pricing)
	input.Ctx = context.Background()
	input.GroupID = groupIDPtr()
	input.Resolver = r
	if input.RateMultiplier == 0 {
		input.RateMultiplier = 1
	}
	return r.billingService.CalculateCostUnified(input)
}

// The UI labels an entry's flat prices "default prices (used when no interval
// matches)"; they must be used instead of an empty $0 card.
func TestChannelPricing_FlatDefaultsApplyWhenNoIntervalMatches(t *testing.T) {
	cost, err := calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"house-model"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(1e-6),
		OutputPrice: testPtrFloat64(2e-6),
		Intervals: []PricingInterval{
			{MinTokens: 200000, InputPrice: testPtrFloat64(4e-6), OutputPrice: testPtrFloat64(8e-6)},
		},
	}}, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 500}})

	require.NoError(t, err)
	require.InDelta(t, 1000*1e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 500*2e-6, cost.OutputCost, 1e-12)
}

// An entry with only intervals for a model without a catalog price falls back
// to the nearest interval rather than an all-zero card.
func TestChannelPricing_IntervalsOnlyUseNearestIntervalOutsideRanges(t *testing.T) {
	pricing := []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"house-model"},
		BillingMode: BillingModeToken,
		Intervals: []PricingInterval{
			{MinTokens: 0, MaxTokens: testPtrInt(128000), InputPrice: testPtrFloat64(2e-6), OutputPrice: testPtrFloat64(6e-6)},
			{MinTokens: 128000, InputPrice: testPtrFloat64(4e-6), OutputPrice: testPtrFloat64(12e-6)},
		},
	}}

	// Context 0 never satisfies (0, 128000]: the first interval still prices it.
	cost, err := calculateWithChannelPricing(t, pricing, CostInput{Model: "house-model", Tokens: UsageTokens{OutputTokens: 100}})
	require.NoError(t, err)
	require.InDelta(t, 100*6e-6, cost.OutputCost, 1e-12)

	longOnly := []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"house-model"},
		BillingMode: BillingModeToken,
		Intervals:   []PricingInterval{{MinTokens: 200000, InputPrice: testPtrFloat64(4e-6), OutputPrice: testPtrFloat64(12e-6)}},
	}}
	cost, err = calculateWithChannelPricing(t, longOnly, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}})
	require.NoError(t, err)
	require.InDelta(t, 1000*4e-6, cost.InputCost, 1e-12)
	require.Greater(t, cost.ActualCost, 0.0)
}

func TestChannelPricing_MatchedIntervalInheritsUnsetPrices(t *testing.T) {
	cost, err := calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:       "anthropic",
		Models:         []string{"house-model"},
		BillingMode:    BillingModeToken,
		CacheReadPrice: testPtrFloat64(0.1e-6),
		Intervals: []PricingInterval{
			{MinTokens: 0, InputPrice: testPtrFloat64(2e-6), OutputPrice: testPtrFloat64(6e-6)},
		},
	}}, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 1000}})

	require.NoError(t, err)
	require.InDelta(t, 1000*0.1e-6, cost.CacheReadCost, 1e-12)
}

// Without a catalog price, blank cache prices must not make cached tokens free:
// they fall back to the card's input price.
func TestChannelPricing_BlankCachePricesFallBackToInputWithoutCatalog(t *testing.T) {
	cost, err := calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"house-model"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(2e-6),
		OutputPrice: testPtrFloat64(6e-6),
	}}, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 100, CacheReadTokens: 1000, CacheCreationTokens: 50}})

	require.NoError(t, err)
	require.InDelta(t, 1000*2e-6, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 50*2e-6, cost.CacheCreationCost, 1e-12)

	// A catalog-priced model keeps the catalog cache prices.
	cost, err = calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"claude-sonnet-4"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(2e-6),
	}}, CostInput{Model: "claude-sonnet-4", Tokens: UsageTokens{CacheReadTokens: 1000}})
	require.NoError(t, err)
	require.InDelta(t, 1000*0.3e-6, cost.CacheReadCost, 1e-12)
}

// A blank image output price means "same as this card's output price", never free.
func TestChannelPricing_BlankImageOutputPriceUsesCardOutputPrice(t *testing.T) {
	cost, err := calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:    "anthropic",
		Models:      []string{"house-image-model"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(1e-6),
		OutputPrice: testPtrFloat64(10e-6),
	}}, CostInput{Model: "house-image-model", Tokens: UsageTokens{OutputTokens: 1200, ImageOutputTokens: 1000}})

	require.NoError(t, err)
	require.InDelta(t, 1000*10e-6, cost.ImageOutputCost, 1e-12)
	require.InDelta(t, 200*10e-6, cost.OutputCost, 1e-12)

	// An explicit 0 stays free.
	cost, err = calculateWithChannelPricing(t, []ChannelModelPricing{{
		Platform:         "anthropic",
		Models:           []string{"house-image-model"},
		BillingMode:      BillingModeToken,
		OutputPrice:      testPtrFloat64(10e-6),
		ImageOutputPrice: testPtrFloat64(0),
	}}, CostInput{Model: "house-image-model", Tokens: UsageTokens{OutputTokens: 1200, ImageOutputTokens: 1000}})
	require.NoError(t, err)
	require.Zero(t, cost.ImageOutputCost)
}

func TestChannelPricing_EmptyEntryIsUnbillableWithoutCatalog(t *testing.T) {
	pricing := []ChannelModelPricing{{Platform: "anthropic", Models: []string{"house-model", "claude-sonnet-4"}, BillingMode: BillingModeToken}}

	_, err := calculateWithChannelPricing(t, pricing, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 100, OutputTokens: 100}})
	require.True(t, errors.Is(err, ErrModelPricingUnavailable), "got %v", err)

	// The same blank entry on a catalog model falls back to the official price.
	cost, err := calculateWithChannelPricing(t, pricing, CostInput{Model: "claude-sonnet-4", Tokens: UsageTokens{InputTokens: 1000}})
	require.NoError(t, err)
	require.InDelta(t, 1000*3e-6, cost.InputCost, 1e-12)

	r := newResolverWithChannel(t, pricing)
	require.False(t, r.Resolve(context.Background(), PricingInput{Model: "house-model", GroupID: groupIDPtr()}).Billable())
	require.True(t, r.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: groupIDPtr()}).Billable())
	require.False(t, r.Resolve(context.Background(), PricingInput{Model: "never-priced-model"}).Billable())
}

func TestChannelPricing_ExplicitZeroPricesAreBillableAndFree(t *testing.T) {
	zero := 0.0
	pricing := []ChannelModelPricing{{Platform: "anthropic", Models: []string{"house-model"}, BillingMode: BillingModeToken, InputPrice: &zero, OutputPrice: &zero}}

	r := newResolverWithChannel(t, pricing)
	require.True(t, r.Resolve(context.Background(), PricingInput{Model: "house-model", GroupID: groupIDPtr()}).Billable())
	cost, err := calculateWithChannelPricing(t, pricing, CostInput{Model: "house-model", Tokens: UsageTokens{InputTokens: 100, OutputTokens: 100, CacheReadTokens: 100}})
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
}

func TestChannelPricing_PerRequestTierFallbacks(t *testing.T) {
	tiers := []PricingInterval{
		{TierLabel: "1k", PerRequestPrice: testPtrFloat64(0.04)},
		{TierLabel: "2K", PerRequestPrice: testPtrFloat64(0.08)},
		{TierLabel: "free", PerRequestPrice: testPtrFloat64(0)},
	}
	image := []ChannelModelPricing{{Platform: "anthropic", Models: []string{"house-image"}, BillingMode: BillingModeImage, Intervals: tiers}}

	cost, err := calculateWithChannelPricing(t, image, CostInput{Model: "house-image", SizeTier: "1K", RequestCount: 2})
	require.NoError(t, err)
	require.InDelta(t, 0.08, cost.TotalCost, 1e-12, "tier labels match case-insensitively")

	cost, err = calculateWithChannelPricing(t, image, CostInput{Model: "house-image", SizeTier: "4K", RequestCount: 1})
	require.NoError(t, err)
	require.InDelta(t, 0.08, cost.TotalCost, 1e-12, "an unmatched tier without a default bills the most expensive tier")

	cost, err = calculateWithChannelPricing(t, image, CostInput{Model: "house-image", SizeTier: "FREE", RequestCount: 1})
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost, "an explicit zero tier stays free")

	withDefault := []ChannelModelPricing{{Platform: "anthropic", Models: []string{"house-image"}, BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.05), Intervals: tiers}}
	cost, err = calculateWithChannelPricing(t, withDefault, CostInput{Model: "house-image", SizeTier: "4K", RequestCount: 1})
	require.NoError(t, err)
	require.InDelta(t, 0.05, cost.TotalCost, 1e-12, "the configured default wins over the highest tier")

	noPrice := []ChannelModelPricing{{Platform: "anthropic", Models: []string{"house-image"}, BillingMode: BillingModePerRequest, Intervals: []PricingInterval{{TierLabel: "1K"}}}}
	_, err = calculateWithChannelPricing(t, noPrice, CostInput{Model: "house-image", SizeTier: "1K", RequestCount: 1})
	require.True(t, errors.Is(err, ErrModelPricingUnavailable), "got %v", err)
}

func TestBillingService_CatalogZeroTokenPricesAreNotAPrice(t *testing.T) {
	ps := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"zero-chat-model": {InputCostPerToken: 0, OutputCostPerToken: 0, LiteLLMProvider: "volcengine", Mode: "chat"},
		"paid-chat-model": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, LiteLLMProvider: "openai", Mode: "chat"},
	}}
	svc := NewBillingService(nil, ps)

	_, err := svc.GetModelPricing("zero-chat-model")
	require.True(t, errors.Is(err, ErrModelPricingUnavailable), "got %v", err)

	pricing, err := svc.GetModelPricing("paid-chat-model")
	require.NoError(t, err)
	require.InDelta(t, 1e-6, pricing.InputPricePerToken, 1e-15)
}

// output_cost_per_image_token is a per-token price; batch image settlement
// must charge the catalog per-image price instead.
func TestBatchImageModelPricingResolver_UsesPerImagePrice(t *testing.T) {
	ps := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-3-pro-image-preview": {
			InputCostPerToken:       2e-6,
			OutputCostPerToken:      1.2e-5,
			OutputCostPerImage:      0.134,
			OutputCostPerImageToken: 0.00012,
			LiteLLMProvider:         "vertex_ai-language-models",
			Mode:                    "image_generation",
		},
		"gemini-2.5-pro": {InputCostPerToken: 1.25e-6, OutputCostPerToken: 1e-5, LiteLLMProvider: "vertex_ai-language-models", Mode: "chat"},
	}}
	bs := NewBillingService(nil, ps)
	resolver := &BatchImageModelPricingResolver{Resolver: NewModelPricingResolver(nil, bs)}

	unit, err := resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "gemini-3-pro-image-preview"})
	require.NoError(t, err)
	require.InDelta(t, 0.134, unit, 1e-12)

	_, err = resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "gemini-2.5-pro"})
	require.True(t, errors.Is(err, ErrBatchImageSettlementPricingMissing), "text models are not batch image models")
}

// Neither price catalog lists OpenAI's embedding models; the built-in table
// must price them so embeddings are neither free nor refused by the gate.
func TestBillingService_OpenAIEmbeddingFallbackPrices(t *testing.T) {
	svc := NewBillingService(nil, nil)
	for model, price := range map[string]float64{
		"text-embedding-3-small":        0.02e-6,
		"openai/text-embedding-3-large": 0.13e-6,
		"text-embedding-ada-002":        0.10e-6,
	} {
		pricing, err := svc.GetModelPricing(model)
		require.NoError(t, err, model)
		require.InDelta(t, price, pricing.InputPricePerToken, 1e-15, model)
	}
}

// xAI reports reasoning tokens beside completion/output tokens and bills them
// at the output rate (total = prompt + completion + reasoning). OpenAI already
// counts reasoning inside completion tokens; it must not be added twice.
func TestOpenAIUsageFromGJSON_FoldsReasoningOnlyWhenExcludedFromOutput(t *testing.T) {
	xaiChat := gjson.Parse(`{"prompt_tokens":663,"completion_tokens":50,"total_tokens":837,"completion_tokens_details":{"reasoning_tokens":124}}`)
	usage, ok := openAIUsageFromGJSON(xaiChat)
	require.True(t, ok)
	require.Equal(t, 663, usage.InputTokens)
	require.Equal(t, 174, usage.OutputTokens)

	xaiResponses := gjson.Parse(`{"input_tokens":100,"output_tokens":20,"total_tokens":420,"output_tokens_details":{"reasoning_tokens":300}}`)
	usage, ok = openAIUsageFromGJSON(xaiResponses)
	require.True(t, ok)
	require.Equal(t, 320, usage.OutputTokens)

	openAIStyle := gjson.Parse(`{"input_tokens":100,"output_tokens":320,"total_tokens":420,"output_tokens_details":{"reasoning_tokens":300}}`)
	usage, ok = openAIUsageFromGJSON(openAIStyle)
	require.True(t, ok)
	require.Equal(t, 320, usage.OutputTokens)

	noTotal := gjson.Parse(`{"input_tokens":100,"output_tokens":20,"output_tokens_details":{"reasoning_tokens":300}}`)
	usage, ok = openAIUsageFromGJSON(noTotal)
	require.True(t, ok)
	require.Equal(t, 20, usage.OutputTokens)

	malformedReasoning := gjson.Parse(`{"input_tokens":100,"output_tokens":20,"total_tokens":420,"output_tokens_details":{"reasoning_tokens":"lots"}}`)
	usage, ok = openAIUsageFromGJSON(malformedReasoning)
	require.True(t, ok, "a malformed best-effort reasoning field must not void the usage")
	require.Equal(t, 20, usage.OutputTokens)
}

func TestCopyOpenAIUsageFromResponsesUsage_FoldsExcludedReasoning(t *testing.T) {
	xai := copyOpenAIUsageFromResponsesUsage(&apicompat.ResponsesUsage{
		InputTokens: 100, OutputTokens: 20, TotalTokens: 420,
		OutputTokensDetails: &apicompat.ResponsesOutputTokensDetails{ReasoningTokens: 300},
	})
	require.Equal(t, 320, xai.OutputTokens)

	openAI := copyOpenAIUsageFromResponsesUsage(&apicompat.ResponsesUsage{
		InputTokens: 100, OutputTokens: 320, TotalTokens: 420,
		OutputTokensDetails: &apicompat.ResponsesOutputTokensDetails{ReasoningTokens: 300},
	})
	require.Equal(t, 320, openAI.OutputTokens)
}
