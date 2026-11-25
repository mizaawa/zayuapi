//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInFlightTextPricingRejectsUnknownChannelTable(t *testing.T) {
	key := modelPricingGateTestAPIKey(PlatformOpenAI)
	account := modelPricingGateTestAccount(1, PlatformOpenAI, nil)
	dbReads := 0
	repo := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) {
		dbReads++
		return nil, errors.New("price database unavailable")
	}}
	channels := NewChannelService(repo, nil, nil, nil)
	billing := NewBillingService(nil, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"priced": {InputCostPerToken: .1}}})
	oai := &OpenAIGatewayService{resolver: NewModelPricingResolver(channels, billing)}
	for range 2 { // First read fails; the second hits the negative cache.
		quote, billable := oai.PriceTurnForAccount(context.Background(), key, "priced", ChannelMappingResult{}, account)
		require.False(t, billable)
		require.True(t, ModelPricingTemporarilyUnavailable(quote))
	}
	require.Equal(t, 1, dbReads)
	channels.cache.Store(populateChannelCache(nil, nil)) // A successful empty result permits catalog fallback.
	quote, billable := oai.PriceTurnForAccount(context.Background(), key, "priced", ChannelMappingResult{}, account)
	require.True(t, billable)
	require.False(t, ModelPricingTemporarilyUnavailable(quote))
}

func TestInFlightTextPricingSurvivesChannelAndCatalogRemoval(t *testing.T) {
	const channelModel, catalogModel = "channel-only-test", "catalog-only-test"
	apiKey := modelPricingGateTestAPIKey(PlatformOpenAI)
	groupID := *apiKey.GroupID
	channel := Channel{ID: 1, GroupIDs: []int64{groupID}, Status: StatusActive, ApplyPricingToAccountStats: true, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformOpenAI, Models: []string{channelModel}, BillingMode: BillingModeTokenPeak, InputPrice: testPtrFloat64(.25), OutputPrice: testPtrFloat64(0),
	}}}
	channels := &ChannelService{}
	catalog := &PricingService{}
	billing := NewBillingService(nil, catalog)
	r := NewModelPricingResolver(channels, billing)
	gw := &GatewayService{resolver: r, billingService: billing}
	oai := &OpenAIGatewayService{resolver: r, billingService: billing}
	bg := context.Background()
	for _, model := range []string{channelModel, catalogModel} {
		// Each subtest starts with the only valid price available again.
		t.Run(model, func(t *testing.T) {
			channels.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{groupID: PlatformOpenAI}))
			catalog.mu.Lock()
			catalog.pricingData = map[string]*LiteLLMModelPricing{catalogModel: {InputCostPerToken: .25}}
			catalog.revision++
			catalog.mu.Unlock()
			account := modelPricingGateTestAccount(1, PlatformOpenAI, nil)
			quote, billable := oai.PriceTurnForAccount(bg, apiKey, model, ChannelMappingResult{}, account)
			require.True(t, billable)
			channels.cache.Store(populateChannelCache(nil, nil))
			catalog.mu.Lock()
			catalog.pricingData = nil
			catalog.revision++
			catalog.mu.Unlock()
			worker := WithUsagePriceSnapshot(bg, quote)
			tokens := UsageTokens{InputTokens: 4}
			cost, err := oai.calculateOpenAIRecordUsageTokenCost(worker, apiKey, model, 2, tokens, "", true)
			require.NoError(t, err)
			require.Equal(t, 2.0, cost.ActualCost)
			cost = gw.calculateTokenCost(worker, &ForwardResult{Usage: ClaudeUsage{InputTokens: 4}}, apiKey, model, 2, &recordUsageOpts{})
			require.Equal(t, 2.0, cost.ActualCost)
			stats := resolveAccountStatsCostWithUnits(worker, channels, billing, account.ID, groupID, model, tokens, accountStatsRequestUnits{}, defaultAccountStatsPricingOptions())
			require.NotNil(t, stats)
			require.Equal(t, 1.0, *stats)
			_, billable = oai.PriceTurnForAccount(quote, apiKey, model, ChannelMappingResult{}, account)
			require.False(t, billable, "a new turn must not inherit a deleted price")
		})
	}
}
