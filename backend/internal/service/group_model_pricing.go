package service

import (
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// CloneGroupModelPricing keeps mutable price pointers and model lists independent.
func CloneGroupModelPricing(pricing []ChannelModelPricing) []ChannelModelPricing {
	cloned := make([]ChannelModelPricing, len(pricing))
	for i := range pricing {
		cloned[i] = pricing[i].Clone()
		cloned[i].InputPrice = cloneGroupValuePointer(pricing[i].InputPrice)
		cloned[i].OutputPrice = cloneGroupValuePointer(pricing[i].OutputPrice)
		cloned[i].CacheWritePrice = cloneGroupValuePointer(pricing[i].CacheWritePrice)
		cloned[i].CacheReadPrice = cloneGroupValuePointer(pricing[i].CacheReadPrice)
		cloned[i].ImageInputPrice = cloneGroupValuePointer(pricing[i].ImageInputPrice)
		cloned[i].ImageOutputPrice = cloneGroupValuePointer(pricing[i].ImageOutputPrice)
		cloned[i].PerRequestPrice = cloneGroupValuePointer(pricing[i].PerRequestPrice)
		for j := range cloned[i].Intervals {
			interval := &cloned[i].Intervals[j]
			interval.MaxTokens = cloneGroupValuePointer(interval.MaxTokens)
			interval.InputPrice = cloneGroupValuePointer(interval.InputPrice)
			interval.OutputPrice = cloneGroupValuePointer(interval.OutputPrice)
			interval.CacheWritePrice = cloneGroupValuePointer(interval.CacheWritePrice)
			interval.CacheReadPrice = cloneGroupValuePointer(interval.CacheReadPrice)
			interval.PerRequestPrice = cloneGroupValuePointer(interval.PerRequestPrice)
		}
	}
	return cloned
}

func normalizeGroupModelPricing(platform string, pricing []ChannelModelPricing) ([]ChannelModelPricing, error) {
	if platform == PlatformComposite {
		platform = PlatformCustom
	}
	normalized := CloneGroupModelPricing(pricing)
	for i := range normalized {
		entry := &normalized[i]
		entry.Platform = platform
		entry.ID = 0
		entry.ChannelID = 0
		if entry.BillingMode == "" {
			entry.BillingMode = BillingModeToken
		}
		switch entry.BillingMode {
		case BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo:
		default:
			return nil, infraerrors.BadRequest("INVALID_BILLING_MODE", fmt.Sprintf("invalid billing mode for group pricing entry %d", i+1))
		}
		if len(entry.Models) == 0 {
			return nil, infraerrors.BadRequest("MODEL_PRICING_MISSING_MODEL", "group pricing requires at least one model")
		}
		for j, model := range entry.Models {
			model = strings.TrimSpace(model)
			if model == "" || strings.Contains(strings.TrimSuffix(model, "*"), "*") {
				return nil, infraerrors.BadRequest("INVALID_PRICING_MODEL", "pricing models must be nonempty and only support a trailing wildcard")
			}
			entry.Models[j] = model
		}
		if entry.BillingMode == BillingModeToken && !groupPricingHasTokenPrice(*entry) {
			return nil, infraerrors.BadRequest("BILLING_MODE_MISSING_PRICE", "token group pricing requires at least one token price")
		}
	}
	if err := validatePricingEntries(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

// ChannelPricingHasTokenPrice reports whether a token-mode pricing entry sets
// at least one token price, either flat or in an interval.
func ChannelPricingHasTokenPrice(pricing ChannelModelPricing) bool {
	return groupPricingHasTokenPrice(pricing)
}

func groupPricingHasTokenPrice(pricing ChannelModelPricing) bool {
	if pricing.InputPrice != nil || pricing.OutputPrice != nil ||
		pricing.CacheWritePrice != nil || pricing.CacheReadPrice != nil ||
		pricing.ImageInputPrice != nil || pricing.ImageOutputPrice != nil {
		return true
	}
	for _, interval := range pricing.Intervals {
		if interval.InputPrice != nil || interval.OutputPrice != nil ||
			interval.CacheWritePrice != nil || interval.CacheReadPrice != nil {
			return true
		}
	}
	return false
}
