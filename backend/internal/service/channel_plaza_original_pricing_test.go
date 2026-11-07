//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListPlazaGroups_ChannelOriginalPriceIsIndependentOfGroupAndCatalog(t *testing.T) {
	channelPrice := 2e-6
	groupPrice := 1e-6
	channel := Channel{
		ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10},
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{"gpt-example"},
			BillingMode: BillingModeToken, InputPrice: &channelPrice,
		}},
	}
	group := Group{
		ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformOpenAI, Models: []string{"gpt-example"},
			BillingMode: BillingModeToken, InputPrice: &groupPrice,
		}},
	}
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"gpt-example": {InputCostPerToken: 9e-6},
	})

	groups, err := newPlazaChannelService([]Channel{channel}, []Group{group}, catalog).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 1)
	model := groups[0].Models[0]
	require.Equal(t, "group", model.PricingSource)
	require.InDelta(t, groupPrice, *model.Pricing.InputPrice, 1e-12)
	require.NotNil(t, model.OfficialPricing)
	require.Equal(t, BillingModeToken, model.OfficialPricing.BillingMode)
	require.InDelta(t, channelPrice, *model.OfficialPricing.InputPrice, 1e-12)
	require.Nil(t, model.OfficialPricing.OutputPrice)
	require.InDelta(t, channelPrice, *channel.ModelPricing[0].InputPrice, 1e-12)
}

func TestListPlazaGroups_OriginalPricePrefersConfiguredChannelOverCatalogFallback(t *testing.T) {
	bare := Channel{
		ID: 1, Name: "alpha", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"shared-model": "shared-model"}},
	}
	priced := plazaPricedChannel(2, "beta", []int64{10}, PlatformOpenAI, "shared-model")
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"shared-model": {InputCostPerToken: 9e-6},
	})
	groups := []Group{{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1}}

	out, err := newPlazaChannelService([]Channel{priced, bare}, groups, catalog).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 1)
	model := out[0].Models[0]
	require.Equal(t, "channel", model.PricingSource)
	require.InDelta(t, 3e-6, *model.OfficialPricing.InputPrice, 1e-12)
	require.InDelta(t, 3e-6, *model.Pricing.InputPrice, 1e-12)
}

func TestListPlazaGroups_OriginalPriceRespectsPlatformMappingAndBillingSource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  string
		wantRaw bool
	}{
		{name: "mapped by default", wantRaw: true},
		{name: "requested model", source: BillingModelSourceRequested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := Channel{
				ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10},
				BillingModelSource: tc.source,
				ModelMapping:       map[string]map[string]string{PlatformCustom: {"public-alias": "provider-model"}},
				ModelPricing: []ChannelModelPricing{{
					Platform: PlatformCustom, Models: []string{"provider-model"},
					BillingMode: BillingModeToken, InputPrice: testPtrFloat64(2e-6),
				}},
			}
			group := Group{ID: 10, Platform: PlatformComposite, RateMultiplier: 1}

			out, err := newPlazaChannelService([]Channel{channel}, []Group{group}, nil).ListPlazaGroups(context.Background())

			require.NoError(t, err)
			models := plazaGroupPricingModelsByName(out[0])
			alias := models["public-alias"]
			if tc.wantRaw {
				require.NotNil(t, alias.OfficialPricing)
				require.InDelta(t, 2e-6, *alias.OfficialPricing.InputPrice, 1e-12)
			} else {
				require.Nil(t, alias.OfficialPricing)
			}
		})
	}
}

func TestListPlazaGroups_ChannelOriginalPreservesModesTiersAndZero(t *testing.T) {
	zero := 0.0
	channel := Channel{
		ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformCustom, Models: []string{"token"}, BillingMode: BillingModeToken,
				InputPrice: &zero, Intervals: []PricingInterval{{MinTokens: 100000, InputPrice: testPtrFloat64(4e-6)}}},
			{Platform: PlatformCustom, Models: []string{"request"}, BillingMode: BillingModePerRequest,
				PerRequestPrice: testPtrFloat64(0.04)},
			{Platform: PlatformCustom, Models: []string{"image"}, BillingMode: BillingModeImage,
				PerRequestPrice: testPtrFloat64(0.2), Intervals: []PricingInterval{{TierLabel: "4K", PerRequestPrice: testPtrFloat64(0.3)}}},
			{Platform: PlatformCustom, Models: []string{"video"}, BillingMode: BillingModeVideo,
				PerRequestPrice: testPtrFloat64(0.08), Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: testPtrFloat64(0.12)}}},
		},
	}
	group := Group{ID: 10, Platform: PlatformComposite, RateMultiplier: 0.5,
		ImagePrice1K: testPtrFloat64(0.01), VideoPrice720P: testPtrFloat64(0.05)}

	out, err := newPlazaChannelService([]Channel{channel}, []Group{group}, nil).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	models := plazaGroupPricingModelsByName(out[0])
	require.Zero(t, *models["token"].OfficialPricing.InputPrice)
	require.Equal(t, 100000, models["token"].OfficialPricing.Intervals[0].MinTokens)
	require.InDelta(t, 4e-6, *models["token"].OfficialPricing.Intervals[0].InputPrice, 1e-12)
	require.Equal(t, BillingModePerRequest, models["request"].OfficialPricing.BillingMode)
	require.InDelta(t, 0.04, *models["request"].OfficialPricing.PerRequestPrice, 1e-12)
	require.Equal(t, BillingModeImage, models["image"].OfficialPricing.BillingMode)
	require.InDelta(t, 0.2, *models["image"].OfficialPricing.PerRequestPrice, 1e-12)
	require.InDelta(t, 0.3, *models["image"].OfficialPricing.Intervals[0].PerRequestPrice, 1e-12)
	require.InDelta(t, 0.01, *models["image"].Pricing.Intervals[0].PerRequestPrice, 1e-12)
	require.Equal(t, BillingModeVideo, models["video"].OfficialPricing.BillingMode)
	require.InDelta(t, 0.12, *models["video"].OfficialPricing.Intervals[0].PerRequestPrice, 1e-12)
	require.InDelta(t, 0.05, *models["video"].Pricing.Intervals[0].PerRequestPrice, 1e-12)
	require.InDelta(t, 0.3, *channel.ModelPricing[2].Intervals[0].PerRequestPrice, 1e-12)
}

func TestListPlazaGroups_MissingChannelPriceStaysMissing(t *testing.T) {
	bare := Channel{
		ID: 1, Name: "bare", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"gpt-example": "gpt-example"}},
	}
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"gpt-example": {InputCostPerToken: 9e-6},
	})

	out, err := newPlazaChannelService([]Channel{bare}, []Group{{ID: 10, Platform: PlatformOpenAI}}, catalog).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	model := out[0].Models[0]
	require.Nil(t, model.OfficialPricing)
	require.Equal(t, "reference", model.PricingSource)
	require.InDelta(t, 9e-6, *model.Pricing.InputPrice, 1e-12)

	groupOnly := Group{ID: 20, Platform: PlatformCustom, ModelPricing: []ChannelModelPricing{{
		Platform: PlatformCustom, Models: []string{"group-only"}, InputPrice: testPtrFloat64(3e-6),
	}}}
	out, err = newPlazaChannelService(nil, []Group{groupOnly}, nil).ListPlazaGroups(context.Background())
	require.NoError(t, err)
	require.Nil(t, out[0].Models[0].OfficialPricing)
	require.Equal(t, "group", out[0].Models[0].PricingSource)
}

func TestListPlazaGroups_OriginalPricesNeverShareSameNameAcrossPlatformsOrGroups(t *testing.T) {
	channel := Channel{
		ID: 1, Name: "same-name", Status: StatusActive, GroupIDs: []int64{10, 20},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"same-model"}, InputPrice: testPtrFloat64(1e-6)},
			{Platform: PlatformCustom, Models: []string{"same-model"}, InputPrice: testPtrFloat64(2e-6)},
		},
	}
	other := plazaPricedChannel(2, "other", []int64{30}, PlatformOpenAI, "same-model")
	groups := []Group{{ID: 10, Platform: PlatformOpenAI}, {ID: 20, Platform: PlatformComposite}, {ID: 30, Platform: PlatformOpenAI}}
	out, err := newPlazaChannelService([]Channel{channel, other}, groups, nil).ListPlazaGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, out, 3)
	for i, expected := range []float64{1e-6, 2e-6, 3e-6} {
		require.Len(t, out[i].Models, 1)
		require.InDelta(t, expected, *out[i].Models[0].OfficialPricing.InputPrice, 1e-12)
		require.Equal(t, BillingModeToken, out[i].Models[0].OfficialPricing.BillingMode, "legacy empty billing modes use token units")
	}
}

func TestListPlazaGroups_ChannelChoiceKeepsPricesAndCurrencyTogether(t *testing.T) {
	first := plazaPricedChannel(10, "same", []int64{10}, PlatformOpenAI, "model")
	second := plazaPricedChannel(20, "SAME", []int64{10}, PlatformOpenAI, "MODEL")
	first.ModelPricing[0].InputPrice = testPtrFloat64(0)
	first.FeaturesConfig = map[string]any{"pricing_currency": map[string]any{
		PlatformOpenAI: map[string]any{"official_cny": true, "discounted_cny": false, "cny_per_usd": 8.0},
	}}
	second.ModelPricing[0].InputPrice = testPtrFloat64(9e-6)
	groups := []Group{{ID: 10, Platform: PlatformOpenAI}}
	for _, channels := range [][]Channel{{second, first}, {first, second}} {
		out, err := newPlazaChannelService(channels, groups, nil).ListPlazaGroups(context.Background())
		require.NoError(t, err)
		require.Len(t, out[0].Models, 1)
		model := out[0].Models[0]
		require.Zero(t, *model.Pricing.InputPrice)
		require.Zero(t, *model.OfficialPricing.InputPrice)
		require.True(t, model.PricingCurrency.OfficialCNY)
		require.False(t, model.PricingCurrency.DiscountedCNY)
		require.True(t, model.PricingCurrency.OfficialCNY)
	}
}

func TestListPlazaGroups_OriginalPriceExactBeforeWildcard(t *testing.T) {
	channel := Channel{
		ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"alias": "vendor-a", "vendor-b": "vendor-b"}},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"vendor-*"}, InputPrice: testPtrFloat64(4e-6)},
			{Platform: PlatformOpenAI, Models: []string{"VENDOR-A"}, InputPrice: testPtrFloat64(0)},
		},
	}
	out, err := newPlazaChannelService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, nil).ListPlazaGroups(context.Background())
	require.NoError(t, err)
	models := plazaGroupPricingModelsByName(out[0])
	require.Zero(t, *models["alias"].OfficialPricing.InputPrice)
	require.InDelta(t, 4e-6, *models["vendor-b"].OfficialPricing.InputPrice, 1e-12)
	require.NotContains(t, models, "vendor-*")
}

func TestListPlazaGroups_EmptyLegacyEntryKeepsReferenceModeAndMissingOriginal(t *testing.T) {
	channel := Channel{
		ID: 1, Name: "legacy", Status: StatusActive, GroupIDs: []int64{10},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"legacy-image"}}},
	}
	catalog := newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
		"legacy-image": {Mode: "image_generation", OutputCostPerImage: 0.04},
	})
	out, err := newPlazaChannelService([]Channel{channel}, []Group{{ID: 10, Platform: PlatformOpenAI}}, catalog).ListPlazaGroups(context.Background())
	require.NoError(t, err)
	model := out[0].Models[0]
	require.Nil(t, model.OfficialPricing)
	require.Equal(t, "reference", model.PricingSource)
	require.Equal(t, BillingModeImage, model.Pricing.BillingMode)
	require.InDelta(t, 0.04, *model.Pricing.PerRequestPrice, 1e-12)
}
