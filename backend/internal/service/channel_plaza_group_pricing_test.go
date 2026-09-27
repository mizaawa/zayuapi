//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListPlazaGroups_GroupPricingWithoutChannel(t *testing.T) {
	for _, platform := range []string{
		PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity,
		PlatformGrok, PlatformCustom, PlatformComposite,
	} {
		t.Run(platform, func(t *testing.T) {
			pricingPlatform := platform
			if platform == PlatformComposite {
				pricingPlatform = PlatformCustom
			}
			groups := []Group{{
				ID: 10, Name: "group-only", Platform: platform, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{{
					Platform: pricingPlatform, Models: []string{"vendor-model"},
					BillingMode: BillingModeToken,
					InputPrice:  testPtrFloat64(2e-6), OutputPrice: testPtrFloat64(4e-6),
				}},
			}}

			out, err := newPlazaChannelService(nil, groups, nil).ListPlazaGroups(context.Background())

			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Equal(t, platform, out[0].Platform)
			require.Len(t, out[0].Models, 1)
			require.Equal(t, "vendor-model", out[0].Models[0].Name)
			require.Equal(t, pricingPlatform, out[0].Models[0].Platform)
			require.NotNil(t, out[0].Models[0].Pricing)
			require.Equal(t, groups[0].ModelPricing[0], *out[0].Models[0].Pricing)
		})
	}
}

func TestListPlazaGroups_GroupPricingExactBeforeWildcardAndZeroOverride(t *testing.T) {
	channel := plazaPricedChannel(1, "channel", []int64{10}, PlatformCustom, "vendor-exact", "vendor-other")
	channel.BillingModelSource = BillingModelSourceRequested
	groups := []Group{{
		ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformCustom, Models: []string{"vendor-*"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(5e-6)},
			{Platform: PlatformCustom, Models: []string{"VENDOR-EXACT"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(0)},
		},
	}}

	out, err := newPlazaChannelService([]Channel{channel}, groups, nil).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 2, "a differently cased group rule must not duplicate a channel model")
	byName := plazaGroupPricingModelsByName(out[0])
	require.NotNil(t, byName["vendor-exact"].Pricing)
	require.NotNil(t, byName["vendor-exact"].Pricing.InputPrice)
	require.Zero(t, *byName["vendor-exact"].Pricing.InputPrice)
	require.Nil(t, byName["vendor-exact"].Pricing.OutputPrice, "missing group fields must not inherit channel overrides")
	require.InDelta(t, 5e-6, *byName["vendor-other"].Pricing.InputPrice, 1e-12)
	require.NotContains(t, byName, "vendor-*")
}

func TestListPlazaGroups_GroupWildcardPricingDoesNotInventModels(t *testing.T) {
	groups := []Group{{
		ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformCustom, Models: []string{"vendor-*"},
			BillingMode: BillingModeToken, InputPrice: testPtrFloat64(2e-6),
		}},
	}}

	out, err := newPlazaChannelService(nil, groups, nil).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Empty(t, out)
}

func TestListPlazaGroups_GroupPricingPlatformIsolation(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			pricingPlatform := platform
			if platform == PlatformComposite {
				pricingPlatform = PlatformCustom
			}
			groups := []Group{{
				ID: 10, Name: "group", Platform: platform, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformAnthropic, Models: []string{"foreign-model"}, InputPrice: testPtrFloat64(9e-6)},
					{Platform: PlatformAnthropic, Models: []string{"own-model"}, InputPrice: testPtrFloat64(9e-6)},
					{Platform: pricingPlatform, Models: []string{"own-model"}, InputPrice: testPtrFloat64(2e-6)},
					{Models: []string{"untagged-model"}, InputPrice: testPtrFloat64(3e-6)},
				},
			}}

			out, err := newPlazaChannelService(nil, groups, nil).ListPlazaGroups(context.Background())

			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Len(t, out[0].Models, 2)
			byName := plazaGroupPricingModelsByName(out[0])
			require.NotContains(t, byName, "foreign-model")
			require.Equal(t, pricingPlatform, byName["own-model"].Platform)
			require.InDelta(t, 2e-6, *byName["own-model"].Pricing.InputPrice, 1e-12)
			require.Equal(t, pricingPlatform, byName["untagged-model"].Platform)
		})
	}
}

func TestListPlazaGroups_GroupPricingRespectsChannelBillingModelSource(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		price  float64
	}{
		{name: "requested", source: BillingModelSourceRequested, price: 1e-6},
		{name: "channel mapped", source: BillingModelSourceChannelMapped, price: 2e-6},
		{name: "default channel mapped", source: "", price: 2e-6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			channel := Channel{
				ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10}, BillingModelSource: tt.source,
				ModelMapping: map[string]map[string]string{PlatformCustom: {"public-alias": "provider-model"}},
				ModelPricing: []ChannelModelPricing{{
					Platform: PlatformCustom, Models: []string{"provider-model"},
					BillingMode: BillingModeToken, InputPrice: testPtrFloat64(9e-6),
				}},
			}
			groups := []Group{{
				ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformCustom, Models: []string{"public-alias"}, InputPrice: testPtrFloat64(1e-6)},
					{Platform: PlatformCustom, Models: []string{"provider-model"}, InputPrice: testPtrFloat64(2e-6)},
				},
			}}

			out, err := newPlazaChannelService([]Channel{channel}, groups, nil).ListPlazaGroups(context.Background())

			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Len(t, out[0].Models, 2)
			byName := plazaGroupPricingModelsByName(out[0])
			require.InDelta(t, tt.price, *byName["public-alias"].Pricing.InputPrice, 1e-12)
			require.InDelta(t, 2e-6, *byName["provider-model"].Pricing.InputPrice, 1e-12)
		})
	}
}

func TestListPlazaGroups_GroupPricingModelsUseChannelWildcardMapping(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		price  float64
	}{
		{name: "default channel mapped", price: 2e-6},
		{name: "requested", source: BillingModelSourceRequested, price: 1e-6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			channel := Channel{
				ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10}, BillingModelSource: tt.source,
				ModelMapping: map[string]map[string]string{PlatformCustom: {"vendor-*": "billing-model"}},
			}
			groups := []Group{{
				ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformCustom, Models: []string{"vendor-a"}, InputPrice: testPtrFloat64(1e-6)},
					{Platform: PlatformCustom, Models: []string{"billing-model"}, InputPrice: testPtrFloat64(2e-6)},
				},
			}}

			out, err := newPlazaChannelService([]Channel{channel}, groups, nil).ListPlazaGroups(context.Background())

			require.NoError(t, err)
			require.Len(t, out, 1)
			require.Len(t, out[0].Models, 2)
			byName := plazaGroupPricingModelsByName(out[0])
			require.InDelta(t, tt.price, *byName["vendor-a"].Pricing.InputPrice, 1e-12)
			require.InDelta(t, 2e-6, *byName["billing-model"].Pricing.InputPrice, 1e-12)
			require.NotContains(t, byName, "vendor-*")
		})
	}
}

func TestListPlazaGroups_GroupPricingModelMappedToChannelWildcardPrice(t *testing.T) {
	channel := Channel{
		ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10},
		ModelMapping: map[string]map[string]string{PlatformCustom: {"vendor-*": "billing-model"}},
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformCustom, Models: []string{"billing-*"}, InputPrice: testPtrFloat64(2e-6),
		}},
	}
	groups := []Group{{
		ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformCustom, Models: []string{"vendor-a"}, InputPrice: testPtrFloat64(1e-6),
		}},
	}}

	out, err := newPlazaChannelService([]Channel{channel}, groups, nil).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "vendor-a", out[0].Models[0].Name)
	require.NotNil(t, out[0].Models[0].Pricing)
	require.InDelta(t, 2e-6, *out[0].Models[0].Pricing.InputPrice, 1e-12)
}

func TestListPlazaGroups_GroupImagePricingPreservesTierPrecedenceAndSources(t *testing.T) {
	channel := Channel{
		ID: 1, Name: "channel", Status: StatusActive, GroupIDs: []int64{10}, BillingModelSource: BillingModelSourceRequested,
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformCustom, Models: []string{"vendor-image"},
			BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.9),
			Intervals: []PricingInterval{{TierLabel: "4K", PerRequestPrice: testPtrFloat64(0.8)}},
		}},
	}
	groups := []Group{{
		ID: 10, Name: "custom", Platform: PlatformComposite, RateMultiplier: 1,
		ImageRateIndependent: true, ImageRateMultiplier: 0.5, ImagePrice1K: testPtrFloat64(0),
		ModelPricing: []ChannelModelPricing{{
			Platform: PlatformCustom, Models: []string{"vendor-image"},
			BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(0.2),
			Intervals: []PricingInterval{{TierLabel: "4K", PerRequestPrice: testPtrFloat64(0.3)}},
		}},
	}}
	channelBefore := channel.Clone()
	groupPricingBefore := CloneGroupModelPricing(groups[0].ModelPricing)

	out, err := newPlazaChannelService([]Channel{channel}, groups, nil).ListPlazaGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.True(t, out[0].ImageRateIndependent)
	require.InDelta(t, 0.5, out[0].ImageRateMultiplier, 1e-12)
	require.Len(t, out[0].Models, 1)
	pricing := out[0].Models[0].Pricing
	require.NotNil(t, pricing)
	require.Equal(t, BillingModeImage, pricing.BillingMode)
	require.InDelta(t, 0.2, *pricing.PerRequestPrice, 1e-12)
	require.Len(t, pricing.Intervals, 3)
	tiers := make(map[string]float64, len(pricing.Intervals))
	for _, interval := range pricing.Intervals {
		require.NotNil(t, interval.PerRequestPrice)
		tiers[interval.TierLabel] = *interval.PerRequestPrice
	}
	require.Equal(t, map[string]float64{"1K": 0, "2K": 0.2, "4K": 0.3}, tiers)
	require.Equal(t, *channelBefore, channel)
	require.Equal(t, groupPricingBefore, groups[0].ModelPricing)
	require.Zero(t, *groups[0].ImagePrice1K)
}

func plazaGroupPricingModelsByName(group PlazaGroup) map[string]PlazaModel {
	models := make(map[string]PlazaModel, len(group.Models))
	for _, model := range group.Models {
		models[model.Name] = model
	}
	return models
}
