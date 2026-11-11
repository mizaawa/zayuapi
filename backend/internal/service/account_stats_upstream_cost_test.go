//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// These tests pin the operations panel's upstream cost (account statistics):
// it must price what the upstream reported at catalog rates and never copy
// the site's own selling prices (group / channel per-image or per-model
// prices), while the customer's charge stays exactly as before.

func statsCatalogBillingService(entries map[string]*LiteLLMModelPricing) *BillingService {
	return NewBillingService(&config.Config{}, &PricingService{pricingData: entries})
}

func TestOpenAIRecordUsage_GroupImagePriceIsNotUpstreamCost(t *testing.T) {
	imagePrice := 0.02
	groupID := int64(12)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.billingService = statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"gpt-image-2": {InputCostPerToken: 5e-6, OutputCostPerToken: 3e-5, OutputCostPerImageToken: 3e-5},
	})
	accountRate := 0.5

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "resp_image_group_price",
			Model:     "gpt-image-2",
			Usage: OpenAIUsage{
				InputTokens:       1110,
				OutputTokens:      1756,
				ImageOutputTokens: 1756,
			},
			ImageCount: 2,
			ImageSize:  "1K",
			Duration:   time.Second,
		},
		APIKey: &APIKey{
			ID:      1008,
			GroupID: i64p(groupID),
			Group:   &Group{ID: groupID, RateMultiplier: 1.0, ImagePrice1K: &imagePrice},
		},
		User:    &User{ID: 2008},
		Account: &Account{ID: 3008, RateMultiplier: &accountRate},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)

	// Customer billing is unchanged: two images at the group's per-image price.
	require.InDelta(t, 0.04, log.TotalCost, 1e-12)
	require.InDelta(t, 0.04, log.ActualCost, 1e-12)
	require.InDelta(t, 0.04, userRepo.lastAmount, 1e-12)

	// Upstream cost: the catalog price of the tokens the upstream reported.
	// Image output tokens are part of output tokens and are counted once.
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 1110*5e-6+1756*3e-5, *log.AccountStatsCost, 1e-12)
	require.NotNil(t, log.AccountRateMultiplier)
	require.InDelta(t, 0.5, *log.AccountRateMultiplier, 1e-12)
}

func TestGatewayRecordUsage_GroupModelPriceIsNotUpstreamCost(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
	svc.billingService = statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"gemini-2.5-pro": {InputCostPerToken: 1.25e-6, OutputCostPerToken: 1e-5},
	})
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	groupID := int64(44)
	inputPrice, outputPrice := 2.5e-6, 2e-5
	group := &Group{
		ID: groupID, Platform: PlatformGemini, Status: StatusActive, Hydrated: true, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformGemini, Models: []string{"gemini-2.5-pro"}, BillingMode: BillingModeToken,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
		}},
	}

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gemini_group_price",
			Usage:     ClaudeUsage{InputTokens: 10000, OutputTokens: 2000},
			Model:     "gemini-2.5-pro",
			Duration:  time.Second,
		},
		APIKey:  &APIKey{ID: 501, GroupID: &groupID, Group: group},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, Platform: PlatformGemini, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)

	// The customer pays the group's own token price…
	require.InDelta(t, 10000*2.5e-6+2000*2e-5, log.TotalCost, 1e-12)
	// …while the upstream cost uses the catalog price of the same tokens.
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 10000*1.25e-6+2000*1e-5, *log.AccountStatsCost, 1e-12)
}

func TestGatewayRecordUsage_ForcedCacheBillingKeepsUpstreamInputCost(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "sticky_failover",
			Usage:     ClaudeUsage{InputTokens: 100000, OutputTokens: 1000},
			Model:     "claude-sonnet-4",
			Duration:  time.Second,
		},
		APIKey:            &APIKey{ID: 502, Quota: 100},
		User:              &User{ID: 602},
		Account:           &Account{ID: 702},
		ForceCacheBilling: true,
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)

	// The customer is billed the sticky-session discount (input as cache read)…
	require.Zero(t, log.InputTokens)
	require.Equal(t, 100000, log.CacheReadTokens)
	// …but the new account's cache was cold, so the upstream charged full input.
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 100000*3e-6+1000*15e-6, *log.AccountStatsCost, 1e-12)
}

func TestResolveAccountStatsCost_CatalogWithoutChannelIgnoresCustomerPrice(t *testing.T) {
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
	})
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}

	for name, cs := range map[string]*ChannelService{
		"no channel service":    nil,
		"group without channel": newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, 1, "anthropic"),
	} {
		result := resolveAccountStatsCost(context.Background(), cs, bs, 1, 99, "claude-sonnet-4", tokens, 1, 999)
		require.NotNil(t, result, name)
		require.InDelta(t, 0.2, *result, 1e-12, name)
	}
}

func TestResolveAccountStatsCost_UngroupedKeyStillUsesCatalog(t *testing.T) {
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
	})
	result := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "claude-sonnet-4",
		UsageTokens{InputTokens: 100, OutputTokens: 50}, accountStatsRequestUnits{requestCount: 1}, defaultAccountStatsPricingOptions())
	require.NotNil(t, result)
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestResolveAccountStatsCost_FallsBackToRequestedModelCatalog(t *testing.T) {
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
	})
	opts := defaultAccountStatsPricingOptions()
	opts.fallbackModels = []string{"", "claude-sonnet-4"}
	result := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "relay-private-model",
		UsageTokens{InputTokens: 100, OutputTokens: 50}, accountStatsRequestUnits{requestCount: 1}, opts)
	require.NotNil(t, result)
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestResolveAccountStatsCost_ChannelSwitchUsesChannelOwnPricing(t *testing.T) {
	inputPrice, outputPrice := 0.01, 0.02
	channel := &Channel{
		ID:                         1,
		Status:                     StatusActive,
		ApplyPricingToAccountStats: true,
		ModelPricing: []ChannelModelPricing{{
			Platform: "anthropic", Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
		}},
	}
	cs := newTestChannelServiceForStats(t, channel, 10, "anthropic")
	expandPricingToCache(cs.cache.Load().(*channelCache), channel, 10, "anthropic")
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}

	// Without a billing service the channel entry is priced directly.
	result := resolveAccountStatsCost(context.Background(), cs, nil, 1, 10, "claude-sonnet-4", tokens, 1, 999)
	require.NotNil(t, result)
	require.InDelta(t, 2.0, *result, 1e-12)

	// With one, the channel entry goes through the billing engine (no group,
	// no multiplier), still at the channel's own price.
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
	})
	result = resolveAccountStatsCost(context.Background(), cs, bs, 1, 10, "claude-sonnet-4", tokens, 1, 999)
	require.NotNil(t, result)
	require.InDelta(t, 2.0, *result, 1e-12)
}

func TestResolveAccountStatsCost_ChannelSwitchWithoutModelFallsToCatalog(t *testing.T) {
	channel := &Channel{ID: 1, Status: StatusActive, ApplyPricingToAccountStats: true}
	cs := newTestChannelServiceForStats(t, channel, 10, "anthropic")
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
	})
	result := resolveAccountStatsCost(context.Background(), cs, bs, 1, 10, "claude-sonnet-4",
		UsageTokens{InputTokens: 100, OutputTokens: 50}, 1, 0.75)
	require.NotNil(t, result)
	require.InDelta(t, 0.2, *result, 1e-12)
}

func TestResolveAccountStatsCost_GeminiImageTokensCountedOnce(t *testing.T) {
	bs := statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"gemini-3-pro-image-preview": {InputCostPerToken: 2e-6, OutputCostPerToken: 1.2e-5, OutputCostPerImageToken: 1.2e-4},
	})
	// usageMetadata: 1520 candidate tokens, 1120 of them image tokens.
	tokens := UsageTokens{InputTokens: 200, OutputTokens: 1520, ImageOutputTokens: 1120}
	result := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "gemini-3-pro-image-preview",
		tokens, accountStatsRequestUnits{requestCount: 1, imageCount: 1, imageSize: "2K"}, defaultAccountStatsPricingOptions())
	require.NotNil(t, result)
	require.InDelta(t, 200*2e-6+400*1.2e-5+1120*1.2e-4, *result, 1e-12)
}

func TestResolveAccountStatsCost_PriorityTierUsesPriorityCatalogPrice(t *testing.T) {
	bs := newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"gpt-5.4": {InputPricePerToken: 1e-6, OutputPricePerToken: 2e-6, InputPricePerTokenPriority: 2e-6, OutputPricePerTokenPriority: 4e-6},
	})
	opts := defaultAccountStatsPricingOptions()
	opts.serviceTier = "priority"
	result := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "gpt-5.4",
		UsageTokens{InputTokens: 1000, OutputTokens: 100}, accountStatsRequestUnits{requestCount: 1}, opts)
	require.NotNil(t, result)
	require.InDelta(t, 1000*2e-6+100*4e-6, *result, 1e-12)
}

func TestResolveAccountStatsCost_HostedImageToolSplitsTextAndImageModels(t *testing.T) {
	bs := statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"gpt-5.4-mini": {InputCostPerToken: 1e-6, OutputCostPerToken: 4e-6},
		"gpt-image-2":  {InputCostPerToken: 5e-6, OutputCostPerToken: 3e-5, OutputCostPerImageToken: 3e-5, InputCostPerImageToken: 8e-6},
	})
	opts := defaultAccountStatsPricingOptions()
	opts.imageModel = "gpt-image-2"
	// Text model usage plus the tool's own image tokens (tool_usage.image_gen).
	tokens := UsageTokens{InputTokens: 500, OutputTokens: 80, ImageOutputTokens: 1600, ImageInputTokens: 300}
	result := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "gpt-5.4-mini",
		tokens, accountStatsRequestUnits{requestCount: 1, imageCount: 1}, opts)
	require.NotNil(t, result)
	text := 500*1e-6 + 80*4e-6
	image := 300*8e-6 + 1600*3e-5
	require.InDelta(t, text+image, *result, 1e-12)
}

func TestResolveAccountStatsCost_TokenlessMediaUsesCatalogUnitPrice(t *testing.T) {
	bs := statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"dall-e-3": {OutputCostPerImage: 0.04},
	})
	images := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "dall-e-3",
		UsageTokens{}, accountStatsRequestUnits{requestCount: 3, imageCount: 3, imageSize: "1K"}, defaultAccountStatsPricingOptions())
	require.NotNil(t, images)
	require.InDelta(t, 0.12, *images, 1e-12)

	grokPrice, ok := getDefaultGrokImagineVideoPrice("grok-imagine-video", "720p")
	require.True(t, ok)
	video := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "grok-imagine-video",
		UsageTokens{}, accountStatsRequestUnits{requestCount: 1, videoCount: 1, videoSeconds: 8, videoResolution: "720p"}, defaultAccountStatsPricingOptions())
	require.NotNil(t, video)
	require.InDelta(t, grokPrice*8, *video, 1e-12)

	// A model with no catalog unit price keeps the historical fallback.
	require.Nil(t, resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "unknown-image-model",
		UsageTokens{}, accountStatsRequestUnits{requestCount: 1, imageCount: 1}, defaultAccountStatsPricingOptions()))
}

func TestCalculateStatsCost_ImageRuleUsesSizeTier(t *testing.T) {
	price1K, price4K := 0.05, 0.2
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeImage,
		Intervals: []PricingInterval{
			{TierLabel: "1K", PerRequestPrice: &price1K},
			{TierLabel: "4K", PerRequestPrice: &price4K},
		},
	}
	result := calculateStatsCostWithUnits(pricing, UsageTokens{}, accountStatsRequestUnits{requestCount: 2, imageCount: 2, imageSize: "4K"})
	require.NotNil(t, result)
	require.InDelta(t, 0.4, *result, 1e-12)
}

func TestCalculateTokenStatsCost_SplitsImageTokensLikeBilling(t *testing.T) {
	in, out, imgIn, imgOut := 0.001, 0.002, 0.004, 0.01
	pricing := &ChannelModelPricing{InputPrice: &in, OutputPrice: &out, ImageInputPrice: &imgIn, ImageOutputPrice: &imgOut}
	result := calculateTokenStatsCost(pricing, UsageTokens{InputTokens: 100, ImageInputTokens: 40, OutputTokens: 50, ImageOutputTokens: 10})
	require.NotNil(t, result)
	require.InDelta(t, 60*0.001+40*0.004+40*0.002+10*0.01, *result, 1e-12)

	// Without image prices the image tokens fall back to the text prices.
	plain := &ChannelModelPricing{InputPrice: &in, OutputPrice: &out}
	result = calculateTokenStatsCost(plain, UsageTokens{InputTokens: 100, ImageInputTokens: 40, OutputTokens: 50, ImageOutputTokens: 10})
	require.NotNil(t, result)
	require.InDelta(t, 100*0.001+50*0.002, *result, 1e-12)
}

func TestCalculateTokenStatsCost_IntervalsMatchContextTokensAndKeepImagePrice(t *testing.T) {
	low, high, out, imgOut := 0.001, 0.003, 0.002, 0.01
	maxLow := 200
	pricing := &ChannelModelPricing{
		ImageOutputPrice: &imgOut,
		Intervals: []PricingInterval{
			{MinTokens: 0, MaxTokens: &maxLow, InputPrice: &low, OutputPrice: &out},
			{MinTokens: 200, InputPrice: &high, OutputPrice: &out},
		},
	}
	// 150 context tokens: the low interval even though input+output exceeds 200.
	result := calculateTokenStatsCost(pricing, UsageTokens{InputTokens: 150, OutputTokens: 100, ImageOutputTokens: 20})
	require.NotNil(t, result)
	require.InDelta(t, 150*0.001+80*0.002+20*0.01, *result, 1e-12)
}

func TestOpenAIRecordUsage_HostedImageToolSplitOnlyForToolTokens(t *testing.T) {
	catalog := map[string]*LiteLLMModelPricing{
		"gpt-5.4":     {InputCostPerToken: 2.5e-6, OutputCostPerToken: 1.5e-5},
		"gpt-image-2": {InputCostPerToken: 5e-6, OutputCostPerToken: 1e-5, OutputCostPerImageToken: 3e-5, InputCostPerImageToken: 8e-6},
	}
	body := []byte(`{"usage":{"input_tokens":43792,"output_tokens":1005},"tool_usage":{"image_gen":{"input_tokens":7918,"input_tokens_details":{"image_tokens":7620,"text_tokens":298},"output_tokens":186,"output_tokens_details":{"image_tokens":186,"text_tokens":0},"total_tokens":8104}}}`)
	toolUsage, ok := extractOpenAIUsageFromJSONBytes(body)
	require.True(t, ok)
	require.True(t, toolUsage.hostedImageToolTokens)

	record := func(usage OpenAIUsage, upstreamModel, billingModel, toolModel string) *UsageLog {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		svc.billingService = statsCatalogBillingService(catalog)
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID: "hosted_tool_" + upstreamModel, Model: upstreamModel, UpstreamModel: upstreamModel,
				BillingModel: billingModel, HostedImageToolModel: toolModel,
				Usage: usage, ImageCount: 1, ImageSize: "1K", Duration: time.Second,
			},
			APIKey:  &APIKey{ID: 1101},
			User:    &User{ID: 2101},
			Account: &Account{ID: 3101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		})
		require.NoError(t, err)
		require.NotNil(t, usageRepo.lastLog)
		return usageRepo.lastLog
	}

	// Responses + hosted tool: text tokens at gpt-5.4, tool tokens at gpt-image-2.
	log := record(toolUsage, "gpt-5.4", "gpt-image-2", "gpt-image-2")
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 43792*2.5e-6+1005*1.5e-5+7620*8e-6+186*3e-5, *log.AccountStatsCost, 1e-9)

	// Grok-style media: BillingModel differs from the upstream model, but the
	// image tokens are part of the reported output, so nothing is split off.
	subset := OpenAIUsage{InputTokens: 100, OutputTokens: 400, ImageOutputTokens: 400}
	log = record(subset, "gpt-image-2", "grok-imagine-image", "")
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 100*5e-6+400*3e-5, *log.AccountStatsCost, 1e-12)
}

func TestOpenAIRecordUsage_WebSearchUsesOfficialPerCallPrice(t *testing.T) {
	groupPrice := 0.05
	groupID := int64(13)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "web_search_calls", Model: "gpt-5.4", WebSearchCalls: 3, Duration: time.Second,
		},
		APIKey: &APIKey{
			ID: 1102, GroupID: &groupID,
			Group: &Group{ID: groupID, RateMultiplier: 1, WebSearchPricePerCall: &groupPrice},
		},
		User:    &User{ID: 2102},
		Account: &Account{ID: 3102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	// The customer pays the group's per-call price; the upstream charged the
	// official $0.01 per call.
	require.InDelta(t, 0.15, log.TotalCost, 1e-12)
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 0.03, *log.AccountStatsCost, 1e-12)
}

func TestAccountStatsTokensFromClaudeUsage_TTLOverrideKeepsAggregateOnly(t *testing.T) {
	usage := ClaudeUsage{InputTokens: 10, CacheCreationInputTokens: 300, CacheCreation5mTokens: 0, CacheCreation1hTokens: 300}
	plain := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	overridden := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{
		"cache_ttl_override_enabled": true, "cache_ttl_override_target": "1h",
	}}

	tokens := accountStatsTokensFromClaudeUsage(usage, plain)
	require.Equal(t, 300, tokens.CacheCreation1hTokens)

	tokens = accountStatsTokensFromClaudeUsage(usage, overridden)
	require.Equal(t, 300, tokens.CacheCreationTokens)
	require.Zero(t, tokens.CacheCreation5mTokens)
	require.Zero(t, tokens.CacheCreation1hTokens)
}

func TestResolveAccountStatsCost_Above200kTierPricesWholeRequest(t *testing.T) {
	bs := statsCatalogBillingService(map[string]*LiteLLMModelPricing{
		"gemini-2.5-pro": {
			InputCostPerToken: 1.25e-6, OutputCostPerToken: 1e-5, CacheReadInputTokenCost: 1.25e-7,
			InputCostPerTokenAbove200k: 2.5e-6, OutputCostPerTokenAbove200k: 1.5e-5, CacheReadInputTokenCostAbove200k: 2.5e-7,
		},
	})
	opts := defaultAccountStatsPricingOptions()

	long := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "gemini-2.5-pro",
		UsageTokens{InputTokens: 250000, OutputTokens: 2000}, accountStatsRequestUnits{requestCount: 1}, opts)
	require.NotNil(t, long)
	require.InDelta(t, 250000*2.5e-6+2000*1.5e-5, *long, 1e-12)

	// At or below 200k the base tier applies.
	short := resolveAccountStatsCostWithUnits(context.Background(), nil, bs, 1, 0, "gemini-2.5-pro",
		UsageTokens{InputTokens: 150000, CacheReadTokens: 50000, OutputTokens: 2000}, accountStatsRequestUnits{requestCount: 1}, opts)
	require.NotNil(t, short)
	require.InDelta(t, 150000*1.25e-6+50000*1.25e-7+2000*1e-5, *short, 1e-12)

	// Customer billing does not see the above-200k prices.
	pricing, err := bs.GetModelPricing("gemini-2.5-pro")
	require.NoError(t, err)
	require.InDelta(t, 1.25e-6, pricing.InputPricePerToken, 1e-18)
	require.Zero(t, pricing.LongContextInputThreshold)
}

func TestPricingServiceParsesAbove200kTierForStatsOnly(t *testing.T) {
	svc := &PricingService{}
	parsed, err := svc.parsePricingData([]byte(`{"gemini-2.5-pro":{"input_cost_per_token":1.25e-6,"output_cost_per_token":1e-5,"input_cost_per_token_above_200k_tokens":2.5e-6,"output_cost_per_token_above_200k_tokens":1.5e-5,"cache_read_input_token_cost_above_200k_tokens":2.5e-7,"cache_creation_input_token_cost_above_200k_tokens":2.5e-7}}`))
	require.NoError(t, err)
	entry := parsed["gemini-2.5-pro"]
	require.NotNil(t, entry)
	require.InDelta(t, 2.5e-6, entry.InputCostPerTokenAbove200k, 1e-18)
	require.InDelta(t, 1.5e-5, entry.OutputCostPerTokenAbove200k, 1e-18)
	require.InDelta(t, 2.5e-7, entry.CacheReadInputTokenCostAbove200k, 1e-18)
	require.InDelta(t, 2.5e-7, entry.CacheCreationInputTokenCostAbove200k, 1e-18)
	require.Zero(t, entry.LongContextInputTokenThreshold)
}

func TestCalculateTokenStatsCost_IntervalInheritsUnsetEntryPrices(t *testing.T) {
	in, out, cacheRead, cacheWrite := 3e-6, 15e-6, 3e-7, 3.75e-6
	longIn, longOut := 6e-6, 22.5e-6
	pricing := &ChannelModelPricing{
		InputPrice: &in, OutputPrice: &out, CacheReadPrice: &cacheRead, CacheWritePrice: &cacheWrite,
		Intervals: []PricingInterval{{MinTokens: 200000, InputPrice: &longIn, OutputPrice: &longOut}},
	}
	tokens := UsageTokens{InputTokens: 10000, CacheReadTokens: 250000, CacheCreationTokens: 5000, OutputTokens: 2000}
	result := calculateTokenStatsCost(pricing, tokens)
	require.NotNil(t, result)
	require.InDelta(t, 10000*longIn+2000*longOut+250000*cacheRead+5000*cacheWrite, *result, 1e-12)
}

func TestOpenAIRecordUsage_GrokKeepsCatalogLongContextTier(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.billingService = newTestBillingServiceWithPrices(map[string]*ModelPricing{
		"grok-4.7": {
			InputPricePerToken: 2e-6, OutputPricePerToken: 1e-5,
			LongContextInputThreshold: 200000, LongContextInputMultiplier: 2, LongContextOutputMultiplier: 2,
		},
	})
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "grok_long_context", Model: "grok-4.7",
			Usage:    OpenAIUsage{InputTokens: 300000, OutputTokens: 1000},
			Duration: time.Second,
		},
		APIKey:  &APIKey{ID: 1103},
		User:    &User{ID: 2103},
		Account: &Account{ID: 3103, Platform: PlatformGrok, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	log := usageRepo.lastLog
	require.NotNil(t, log)
	// xAI charges the long-context tier even though this site does not bill it
	// to the customer for Grok accounts.
	require.NotNil(t, log.AccountStatsCost)
	require.InDelta(t, 300000*2e-6*2+1000*1e-5*2, *log.AccountStatsCost, 1e-9)
}
