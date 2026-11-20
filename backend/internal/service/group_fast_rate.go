package service

import (
	"math"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	DefaultOpenAIFastRateMultiplier      = 2.0
	DefaultOpenAIUltraFastRateMultiplier = 6.0
	OpenAIFastTierUltraFast              = "ultrafast"
)

func groupSpeedConfig(platform string, ultra float64, mode string) (float64, string, error) {
	if mode == "" || platform != PlatformOpenAI {
		mode = "off"
	}
	if platform != PlatformOpenAI {
		ultra = DefaultOpenAIUltraFastRateMultiplier
	}
	if !(ultra >= 0.1 && ultra <= 100) {
		return 0, "", infraerrors.BadRequest("INVALID_OPENAI_FAST_RATE", "ultrafast_rate_multiplier must be between 0.1 and 100")
	}
	switch mode {
	case "off", "fast", OpenAIFastTierUltraFast:
		return ultra, mode, nil
	default:
		return 0, "", infraerrors.BadRequest("INVALID_OPENAI_FAST_MODE", "forced_fast_mode must be off, fast or ultrafast")
	}
}

func forcedOpenAIServiceTier(group *Group) string {
	if group != nil && group.Platform == PlatformOpenAI {
		switch group.ForcedFastMode {
		case "fast":
			return OpenAIFastTierPriority
		case OpenAIFastTierUltraFast:
			return OpenAIFastTierUltraFast
		}
	}
	return ""
}

func (g *Group) ForcedOpenAIServiceTier() string { return forcedOpenAIServiceTier(g) }

func standardPriceForForcedOpenAISpeed(group *Group) bool {
	return group != nil && !group.FastRateEnabled && forcedOpenAIServiceTier(group) != ""
}

func isOpenAISpeedTier(tier string) bool {
	return tier == OpenAIFastTierPriority || tier == OpenAIFastTierUltraFast
}

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
	if group == nil || group.Platform != PlatformOpenAI || !group.FastRateEnabled {
		return 0
	}
	switch normalizeBillingServiceTier(serviceTier) {
	case OpenAIFastTierPriority:
		if group.FastRateMultiplier >= 0.1 && group.FastRateMultiplier <= 100 {
			return group.FastRateMultiplier
		}
	case OpenAIFastTierUltraFast:
		if group.UltraFastRateMultiplier >= 0.1 && group.UltraFastRateMultiplier <= 100 {
			return group.UltraFastRateMultiplier
		}
	}
	return 0
}
