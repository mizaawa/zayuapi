//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestToModelPlazaGroupDTO_ChannelOriginalPricingIncludesModeAndTiers(t *testing.T) {
	price := 0.12
	group := service.PlazaGroup{Models: []service.PlazaModel{{
		Name: "vendor-video", Platform: service.PlatformCustom,
		PricingSource: "group",
		OfficialPricing: &service.PlazaOfficialPricing{
			BillingMode:     service.BillingModeVideo,
			PerRequestPrice: &price,
			Intervals:       []service.PricingInterval{{TierLabel: "720p", PerRequestPrice: &price}},
		},
	}}}

	dto := toModelPlazaGroupDTO(&group, nil)
	raw, err := json.Marshal(dto)
	require.NoError(t, err)

	var decoded struct {
		Models []struct {
			PricingSource   string `json:"pricing_source"`
			OfficialPricing struct {
				BillingMode     string   `json:"billing_mode"`
				PerRequestPrice *float64 `json:"per_request_price"`
				Intervals       []struct {
					TierLabel       string   `json:"tier_label"`
					PerRequestPrice *float64 `json:"per_request_price"`
				} `json:"intervals"`
			} `json:"official_pricing"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Models, 1)
	require.Equal(t, "group", decoded.Models[0].PricingSource)
	require.Equal(t, "video", decoded.Models[0].OfficialPricing.BillingMode)
	require.InDelta(t, 0.12, *decoded.Models[0].OfficialPricing.PerRequestPrice, 1e-12)
	require.Equal(t, "720p", decoded.Models[0].OfficialPricing.Intervals[0].TierLabel)
	require.InDelta(t, 0.12, *decoded.Models[0].OfficialPricing.Intervals[0].PerRequestPrice, 1e-12)
	require.NotContains(t, string(raw), "channel_id")
	require.NotContains(t, string(raw), "pricing_id")
}
