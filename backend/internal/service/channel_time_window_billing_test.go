//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelTimeWindowUnifiedBilling(t *testing.T) {
	f := testPtrFloat64
	p := ChannelModelPricing{Platform: "anthropic", Models: []string{"scheduled-test"}, BillingMode: BillingModeTokenPeak, InputPrice: f(2e-6), OutputPrice: f(5e-6), CacheReadPrice: f(1e-6), TimeWindows: []TokenPriceWindow{{Start: "09:00", End: "10:00", InputPrice: f(4e-6), ImageInputPrice: f(0), CacheReadPrice: f(0)}}}
	r := newResolverWithChannel(t, []ChannelModelPricing{p})
	for _, tc := range []struct {
		at    string
		total float64
	}{{"2026-11-25T01:59:59Z", (50*4 + 10*5) * 1e-6}, {"2026-11-25T02:00:00Z", (100*2 + 10*5 + 7) * 1e-6}} {
		at, err := time.Parse(time.RFC3339, tc.at)
		require.NoError(t, err)
		for _, key := range []any{gatewayTokenRequestPricingAtCtxKey{}, openAIPricingAtCtxKey{}} {
			ctx := context.WithValue(context.Background(), key, at)
			tokens := UsageTokens{InputTokens: 100, ImageInputTokens: 50, OutputTokens: 10, CacheReadTokens: 7}
			got, err := r.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: "scheduled-test", GroupID: groupIDPtr(), Resolver: r, Tokens: tokens, RateMultiplier: 2})
			require.NoError(t, err)
			require.Equal(t, "token", got.BillingMode)
			require.InDelta(t, tc.total*2, got.ActualCost, 1e-12)
			cost := calculateStatsCostWithUnits(effectiveTokenCard(ctx, &p), tokens, accountStatsRequestUnits{})
			require.NotNil(t, cost)
			require.InDelta(t, tc.total, *cost, 1e-12)
		}
	}
}
func TestPoolModeIgnoresLegacySwitch(t *testing.T) {
	for _, typ := range []string{AccountTypeAPIKey, AccountTypeBedrock} {
		for _, creds := range []map[string]any{nil, {"pool_mode": false}, {"pool_mode": true}} {
			require.True(t, (&Account{Type: typ, Credentials: creds}).IsPoolMode())
		}
	}
	require.False(t, (&Account{Type: AccountTypeOAuth, Credentials: map[string]any{"pool_mode": true}}).IsPoolMode())
}

func TestChannelTimeWindowFreeStatsDoNotFallThrough(t *testing.T) {
	p := ChannelModelPricing{BillingMode: BillingModeTokenPeak, InputPrice: testPtrFloat64(1), OutputPrice: testPtrFloat64(2), TimeWindows: []TokenPriceWindow{{Start: "08:00", End: "09:00", ImageInputPrice: testPtrFloat64(0)}}}
	card := p.AtTime(time.Date(2026, 11, 25, 0, 30, 0, 0, time.UTC))
	cost := calculateTokenStatsCost(card, UsageTokens{InputTokens: 10, ImageInputTokens: 10})
	require.NotNil(t, cost)
	require.Zero(t, *cost)
}

func TestScheduledImagePriceSurvivesReservationRoundTrip(t *testing.T) {
	f := testPtrFloat64
	p := ChannelModelPricing{Platform: "anthropic", Models: []string{"scheduled-image"}, BillingMode: BillingModeTokenPeak,
		InputPrice: f(1), OutputPrice: f(2), TimeWindows: []TokenPriceWindow{{Start: "08:00", End: "09:00", InputPrice: f(0), OutputPrice: f(0)}}}
	r := newResolverWithChannel(t, []ChannelModelPricing{p})
	ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, time.Date(2026, 11, 25, 0, 30, 0, 0, time.UTC))
	resolved := r.Resolve(ctx, PricingInput{Model: "scheduled-image", GroupID: groupIDPtr()})
	require.Equal(t, PricingSourceChannel, resolved.Source)
	snapshot := MediaImagePricing{Model: "scheduled-image", TokenMultiplier: 1, Resolved: resolved, ChannelPricing: resolved.channelPricing}
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var restored MediaImagePricing
	require.NoError(t, json.Unmarshal(data, &restored))
	result, tokens := &OpenAIForwardResult{ImageCount: 1}, UsageTokens{InputTokens: 100, OutputTokens: 30, ImageOutputTokens: 20}
	cost, err := restored.calculate(r.billingService, result, tokens)
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
	restored.ChannelPricing = &ChannelModelPricing{BillingMode: BillingModeToken}
	_, err = restored.calculate(r.billingService, result, tokens)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
