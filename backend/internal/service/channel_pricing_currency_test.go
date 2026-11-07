package service

import "testing"

func TestChannelPricingCurrencyOnlyStoresDisplaySwitches(t *testing.T) {
	legacy := (&Channel{}).PricingCurrency(PlatformOpenAI)
	if legacy.OfficialCNY || legacy.DiscountedCNY {
		t.Fatalf("legacy currency = %+v", legacy)
	}
	channel := &Channel{FeaturesConfig: map[string]any{"pricing_currency": map[string]any{
		PlatformOpenAI:    map[string]any{"official_cny": true, "discounted_cny": false, "cny_per_usd": 7.2},
		PlatformAnthropic: map[string]any{"official_cny": false, "discounted_cny": true, "cny_per_usd": 7.3},
	}}}
	if got := channel.PricingCurrency(PlatformOpenAI); !got.OfficialCNY || got.DiscountedCNY {
		t.Fatalf("OpenAI currency = %+v", got)
	}
	if got := channel.PricingCurrency(PlatformAnthropic); got.OfficialCNY || !got.DiscountedCNY {
		t.Fatalf("Anthropic currency = %+v", got)
	}
	if err := validateChannelPricingCurrency(channel.FeaturesConfig); err != nil {
		t.Fatal(err)
	}
	pricingCurrencies, ok := channel.FeaturesConfig["pricing_currency"].(map[string]any)
	if !ok {
		t.Fatal("pricing currency config has an unexpected type")
	}
	openAICurrency, ok := pricingCurrencies[PlatformOpenAI].(map[string]any)
	if !ok {
		t.Fatal("OpenAI currency config has an unexpected type")
	}
	openAICurrency["cny_per_usd"] = -1.0 // ignored legacy data
	if err := validateChannelPricingCurrency(channel.FeaturesConfig); err != nil {
		t.Fatalf("legacy exchange-rate field should be ignored: %v", err)
	}
}
