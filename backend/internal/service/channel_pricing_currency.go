package service

import (
	"fmt"
	"math"
)

// ChannelPricingCurrency only controls presentation/input. Persisted model prices,
// usage costs and wallet balances remain USD. Missing legacy settings mean USD.
type ChannelPricingCurrency struct {
	OfficialCNY   bool    `json:"official_cny"`
	DiscountedCNY bool    `json:"discounted_cny"`
	CNYPerUSD     float64 `json:"cny_per_usd"`
}

const defaultChannelCNYPerUSD = 7.15

func (c *Channel) PricingCurrency(platform string) ChannelPricingCurrency {
	result := ChannelPricingCurrency{CNYPerUSD: defaultChannelCNYPerUSD}
	if c == nil || c.FeaturesConfig == nil {
		return result
	}
	all, ok := c.FeaturesConfig["pricing_currency"].(map[string]any)
	if !ok {
		return result
	}
	value, ok := all[platform].(map[string]any)
	if !ok {
		return result
	}
	result.OfficialCNY, _ = value["official_cny"].(bool)
	result.DiscountedCNY, _ = value["discounted_cny"].(bool)
	if rate, ok := value["cny_per_usd"].(float64); ok && rate > 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate) {
		result.CNYPerUSD = rate
	}
	return result
}

func validateChannelPricingCurrency(config map[string]any) error {
	if config == nil || config["pricing_currency"] == nil {
		return nil
	}
	all, ok := config["pricing_currency"].(map[string]any)
	if !ok {
		return fmt.Errorf("pricing_currency must be an object")
	}
	for platform, raw := range all {
		value, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("pricing_currency.%s must be an object", platform)
		}
		for _, key := range []string{"official_cny", "discounted_cny"} {
			if _, ok := value[key].(bool); !ok {
				return fmt.Errorf("pricing_currency.%s.%s must be a boolean", platform, key)
			}
		}
		rate, ok := value["cny_per_usd"].(float64)
		if !ok || math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 || rate > 1e6 {
			return fmt.Errorf("pricing_currency.%s.cny_per_usd must be a positive finite number", platform)
		}
	}
	return nil
}
