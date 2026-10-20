package service

import (
	"context"
	"encoding/json"
	"fmt"
)

// MediaImagePricing retains customer prices at admission, while settlement
// still uses the image count and token usage actually delivered. It contains
// prices only: no API key, account credentials, or live configuration pointers.
type MediaImagePricing struct {
	Model                     string                   `json:"model"`
	TokenMultiplier           float64                  `json:"token_multiplier"`
	ImageMultiplier           float64                  `json:"image_multiplier"`
	ServiceTier               string                   `json:"service_tier,omitempty"`
	LongContextBillingEnabled bool                     `json:"long_context_billing_enabled"`
	Resolved                  *ResolvedPricing         `json:"resolved,omitempty"`
	ChannelPricing            *ChannelModelPricing     `json:"channel_pricing,omitempty"`
	ImageUnitCosts            map[string]CostBreakdown `json:"image_unit_costs,omitempty"`
}

func (s *OpenAIGatewayService) captureMediaImagePricing(ctx context.Context, apiKey *APIKey, result *OpenAIForwardResult, models []string, tokenMultiplier, imageMultiplier float64, serviceTier string, longContext bool) (*MediaImagePricing, error) {
	model := firstUsageBillingModel(models)
	pricing := &MediaImagePricing{Model: model, TokenMultiplier: tokenMultiplier, ImageMultiplier: imageMultiplier, ServiceTier: serviceTier, LongContextBillingEnabled: longContext}
	resolved := s.resolveOpenAIChannelPricing(ctx, model, apiKey)
	if resolved != nil && resolved.Mode == BillingModeToken {
		pricing.Resolved, pricing.ChannelPricing = resolved, resolved.channelPricing
	} else {
		pricing.ImageUnitCosts = make(map[string]CostBreakdown, 3)
		for _, tier := range []string{"1K", "2K", "4K"} {
			unit := *result
			unit.ImageCount, unit.ImageSize = 1, tier
			cost := s.calculateOpenAIImageCost(ctx, model, apiKey, &unit, imageMultiplier)
			if cost == nil {
				return nil, fmt.Errorf("media image price is unavailable")
			}
			if cost.BillingMode == "" {
				cost.BillingMode = string(BillingModeImage)
			}
			pricing.ImageUnitCosts[tier] = *cost
		}
	}
	// Resolved pricing includes pointers into mutable configuration. Detach
	// them now, not only when ReserveMedia later serializes the reservation.
	data, err := json.Marshal(pricing)
	if err != nil {
		return nil, fmt.Errorf("invalid media image price: %w", err)
	}
	var detached MediaImagePricing
	if err := json.Unmarshal(data, &detached); err != nil {
		return nil, err
	}
	return &detached, nil
}

func (p *MediaImagePricing) calculate(s *BillingService, result *OpenAIForwardResult, tokens UsageTokens) (*CostBreakdown, error) {
	if p == nil || s == nil || result == nil {
		return nil, fmt.Errorf("media image price is unavailable")
	}
	if err := validateUsageTokens(tokens); err != nil {
		return nil, err
	}
	if p.Resolved != nil {
		resolved := *p.Resolved
		resolved.channelPricing = p.ChannelPricing
		return s.CalculateCostUnified(CostInput{
			Model: p.Model, Tokens: tokens, RequestCount: result.ImageCount,
			RateMultiplier: p.TokenMultiplier, ServiceTier: p.ServiceTier,
			Resolver: &ModelPricingResolver{}, Resolved: &resolved,
			LongContextBillingEnabled: &p.LongContextBillingEnabled,
		})
	}
	if result.ImageCount < 0 || !validBillableRequestCount(max(1, result.ImageCount)) {
		return nil, fmt.Errorf("invalid delivered media image count")
	}
	unit, ok := p.ImageUnitCosts[NormalizeImageBillingTierOrDefault(result.ImageSize)]
	if !ok {
		return nil, fmt.Errorf("media image price snapshot is incomplete")
	}
	count := float64(result.ImageCount)
	for _, value := range []*float64{&unit.InputCost, &unit.ImageInputCost, &unit.OutputCost, &unit.ImageOutputCost, &unit.CacheCreationCost, &unit.CacheReadCost, &unit.TotalCost, &unit.ActualCost} {
		*value *= count
	}
	return &unit, nil
}

func mediaImagePricingReservation(ctx context.Context, input *OpenAIRecordUsageInput) (*MediaBillingReservation, error) {
	r := input.MediaReservation
	if r == nil {
		r, _ = ctx.Value(mediaWorkbenchPrepaymentKey{}).(*MediaBillingReservation)
	}
	if r == nil || r.ImagePricing == nil {
		return nil, nil // Existing reservations retain their original behavior.
	}
	if input.APIKey == nil || input.User == nil || r.Quote.Command.UserID != input.User.ID || r.Quote.Command.APIKeyID != input.APIKey.ID || (input.MediaReservationID != "" && input.MediaReservationID != r.ID) {
		return nil, ErrUsageBillingRequestConflict
	}
	return r, nil
}
