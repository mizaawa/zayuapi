package service

import (
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const DefaultOpenAIFastRateMultiplier = 2.0

// NormalizeOpenAIFastRateConfig keeps this pricing rule specific to OpenAI groups.
func NormalizeOpenAIFastRateConfig(platform string, enabled bool, multiplier float64) (bool, float64) {
	if platform != PlatformOpenAI {
		return false, DefaultOpenAIFastRateMultiplier
	}
	return enabled, multiplier
}

// ValidateOpenAIFastRateConfig bounds the customer multiplier to a useful,
// finite range that fits the four-decimal database column.
func ValidateOpenAIFastRateConfig(platform string, enabled bool, multiplier float64) error {
	if enabled && platform != PlatformOpenAI {
		return infraerrors.BadRequest("INVALID_OPENAI_FAST_RATE", "fast_rate_enabled is only available for OpenAI groups")
	}
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0.1 || multiplier > 100 {
		return infraerrors.BadRequest("INVALID_OPENAI_FAST_RATE", "fast_rate_multiplier must be between 0.1 and 100")
	}
	return nil
}

func openAIFastRateMultiplier(group *Group, serviceTier string) float64 {
	if group == nil || group.Platform != PlatformOpenAI || !group.FastRateEnabled ||
		normalizeBillingServiceTier(serviceTier) != "priority" ||
		ValidateOpenAIFastRateConfig(group.Platform, true, group.FastRateMultiplier) != nil {
		return 0
	}
	return group.FastRateMultiplier
}
