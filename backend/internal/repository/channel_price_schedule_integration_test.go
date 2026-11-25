//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelPriceSchedulePostgresRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewChannelRepository(integrationDB)
	zero, input, output := 0.0, 1e-6, 4e-6
	card := service.ChannelModelPricing{Platform: "openai", Models: []string{"price-schedule-test"}, BillingMode: service.BillingModeTokenPeak,
		InputPrice: &input, OutputPrice: &output, ImageInputPrice: &zero,
		TimeWindows: []service.TokenPriceWindow{{Start: "23:30", End: "02:15", InputPrice: &zero}}}
	stats := card.Clone()
	stats.TimeWindows[0].OutputPrice = &input
	ch := &service.Channel{Name: "schedule-" + uniqueNano(), Status: service.StatusActive, BillingModelSource: "requested",
		ModelPricing: []service.ChannelModelPricing{card}, AccountStatsPricingRules: []service.AccountStatsPricingRule{{Name: "upstream", Pricing: []service.ChannelModelPricing{stats}}}}
	require.NoError(t, repo.Create(ctx, ch))
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, ch.ID)) })
	loaded, err := repo.GetByID(ctx, ch.ID)
	require.NoError(t, err)
	require.Len(t, loaded.ModelPricing, 1)
	require.Len(t, loaded.AccountStatsPricingRules, 1)
	require.Len(t, loaded.AccountStatsPricingRules[0].Pricing, 1)
	for _, pair := range [][2]service.ChannelModelPricing{{card, loaded.ModelPricing[0]}, {stats, loaded.AccountStatsPricingRules[0].Pricing[0]}} {
		require.Equal(t, pair[0].BillingMode, pair[1].BillingMode)
		require.Equal(t, pair[0].TimeWindows, pair[1].TimeWindows)
		require.Equal(t, pair[0].ImageInputPrice, pair[1].ImageInputPrice)
	}
	loaded.ModelPricing[0].TimeWindows = nil
	require.NoError(t, repo.UpdateModelPricing(ctx, &loaded.ModelPricing[0]))
	loaded, err = repo.GetByID(ctx, ch.ID)
	require.NoError(t, err)
	require.Empty(t, loaded.ModelPricing[0].TimeWindows)
	require.Equal(t, &input, loaded.ModelPricing[0].InputPrice)
	require.Equal(t, stats.TimeWindows, loaded.AccountStatsPricingRules[0].Pricing[0].TimeWindows)
}
