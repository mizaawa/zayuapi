package service

import (
	"fmt"
)

// ChannelPricingCurrency controls only the symbols used to display prices.
type ChannelPricingCurrency struct {
	OfficialCNY   bool `json:"official_cny"`
	DiscountedCNY bool `json:"discounted_cny"`
}

func (c *Channel) PricingCurrency(platform string) ChannelPricingCurrency {
	var result ChannelPricingCurrency
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
	}
	return nil
}
