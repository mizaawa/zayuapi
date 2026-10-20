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
