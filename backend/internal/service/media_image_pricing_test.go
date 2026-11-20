//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaImagePricingKeepsAdmissionPrice(t *testing.T) {
	for _, token := range []bool{false, true} {
		t.Run(map[bool]string{false: "per-image", true: "token-interval"}[token], func(t *testing.T) {
			repo := &mediaOutcomeRepository{}
			s := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			s.usageBillingRepo, s.billingCacheService = repo, nil
			s.resolver = NewModelPricingResolver(nil, s.billingService)
			price, inputPrice, outputPrice, imagePrice := .1, .001, .002, .003
			group := &Group{ID: 812, Platform: PlatformCustom, RateMultiplier: 1, ImagePrice1K: &price}
			if token {
				group.ModelPricing = []ChannelModelPricing{{Models: []string{"fixture-image"}, BillingMode: BillingModeToken, ImageOutputPrice: &imagePrice,
					Intervals: []PricingInterval{{InputPrice: &inputPrice, OutputPrice: &outputPrice}}}}
			}
			key := &APIKey{ID: 813, UserID: 814, GroupID: &group.ID, Group: group}
			input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "fixture-image", ImageCount: 2, ImageSize: "1K", Usage: OpenAIUsage{InputTokens: 32000, OutputTokens: 65536, ImageOutputTokens: 65536}}, APIKey: key, User: &User{ID: 814}, Account: &Account{ID: 815, Platform: PlatformCustom}, WorkbenchKind: "image"}
			r, err := s.PrechargeMedia(context.Background(), input)
			require.NoError(t, err)
			encoded, err := json.Marshal(r)
			require.NoError(t, err)
			var restored MediaBillingReservation
			require.NoError(t, json.Unmarshal(encoded, &restored))
			// Pricing edits and auth refreshes after admission must not reprice
			// an already accepted task. Actual delivered units still apply.
			price, inputPrice, outputPrice, imagePrice = .9, .01, .02, .03
			group.RateMultiplier = 9
			actual := *input
			actual.Result = &OpenAIForwardResult{Model: "fixture-image", ImageCount: 1, ImageSize: "1K", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 8, ImageOutputTokens: 5}}
			var quote *MediaBillingQuote
			actual.MediaQuote = &quote
			require.NoError(t, s.RecordUsage(WithMediaWorkbenchPrepayment(context.Background(), &restored), &actual))
			want := .1
			if token {
				want = .031 // 10 input + 3 text output + 5 image output tokens.
			}
			require.InDelta(t, want, quote.Command.BalanceCost, 1e-8)
			require.Equal(t, 1, quote.Usage.ImageCount)
			// The handler also passes the reservation explicitly because its
			// mandatory usage worker may have a different background context.
			actual.MediaReservation = &restored
			actual.MediaReservationID = restored.ID
			actual.PricingAt = input.PricingAt.AddDate(1, 0, 0)
			group.ModelPricing = nil // Removing the live model must not erase its admitted price.
			require.NoError(t, s.RecordUsage(context.Background(), &actual))
			require.InDelta(t, want, quote.Command.BalanceCost, 1e-8)
			after, err := json.Marshal(&restored)
			require.NoError(t, err)
			require.JSONEq(t, string(encoded), string(after), "settlement must not mutate the admitted pricing")
			actual.User = &User{ID: 999}
			require.ErrorIs(t, s.RecordUsage(context.Background(), &actual), ErrUsageBillingRequestConflict)
		})
	}
}

func TestForcedSpeedMediaPriceSurvivesGroupEdits(t *testing.T) {
	const model = "speed-image"
	for _, tc := range []struct {
		name              string
		enabled, perImage bool
		want              float64
	}{
		{"forced token speed at standard rate", false, false, .02},
		{"explicit token surcharge", true, false, .15},
		{"per-image without surcharge", false, true, .4},
		{"per-image with speed pricing enabled", true, true, .4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			s.usageBillingRepo, s.billingCacheService = &mediaOutcomeRepository{}, nil
			s.resolver = NewModelPricingResolver(nil, s.billingService)
			inputPrice, perImagePrice := .001, .4
			group := &Group{ID: 812, Platform: PlatformOpenAI, RateMultiplier: 2, ImageRateIndependent: true, ImageRateMultiplier: 1, ImagePrice1K: &perImagePrice,
				ForcedFastMode: "ultrafast", FastRateEnabled: tc.enabled, UltraFastRateMultiplier: 7.5}
			if !tc.perImage {
				group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, InputPrice: &inputPrice}}
			}
			key := &APIKey{ID: 813, UserID: 814, GroupID: &group.ID, Group: group}
			result := &OpenAIForwardResult{Model: model, ImageCount: 1, ImageSize: "1K", ServiceTier: optionalTrimmedStringPtr("ultrafast"), Usage: OpenAIUsage{InputTokens: 10}}
			input := &OpenAIRecordUsageInput{Result: result, APIKey: key, User: &User{ID: key.UserID}, Account: &Account{ID: 815, Platform: PlatformOpenAI}, WorkbenchKind: "image"}
			reservation, err := s.PrechargeMedia(context.Background(), input)
			require.NoError(t, err)
			require.InDelta(t, tc.want, reservation.Quote.Command.BalanceCost, 1e-10)
			data, err := json.Marshal(reservation)
			require.NoError(t, err)
			var restored MediaBillingReservation
			require.NoError(t, json.Unmarshal(data, &restored))
			group.FastRateEnabled, group.UltraFastRateMultiplier = !tc.enabled, 12
			var quote *MediaBillingQuote
			input.MediaReservation, input.MediaQuote = &restored, &quote
			require.NoError(t, s.RecordUsage(context.Background(), input))
			require.InDelta(t, tc.want, quote.Command.BalanceCost, 1e-10)
			if !tc.perImage && tc.enabled {
				require.Equal(t, 7.5, *quote.Usage.ServiceTierMultiplier)
			} else {
				require.Nil(t, quote.Usage.ServiceTierMultiplier)
			}
		})
	}
}
