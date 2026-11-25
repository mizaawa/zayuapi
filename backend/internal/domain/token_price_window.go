package domain

import (
	"fmt"
	"math"
	"time"
)

// Start is inclusive, End exclusive. An earlier end wraps over midnight.
type TokenPriceWindow struct {
	Start            string   `json:"start"`
	End              string   `json:"end"`
	InputPrice       *float64 `json:"input_price"`
	OutputPrice      *float64 `json:"output_price"`
	CacheWritePrice  *float64 `json:"cache_write_price"`
	CacheReadPrice   *float64 `json:"cache_read_price"`
	ImageInputPrice  *float64 `json:"image_input_price"`
	ImageOutputPrice *float64 `json:"image_output_price"`
}

func clockMinute(clock string) (int, bool) {
	if len(clock) != 5 {
		return 0, false
	}
	t, err := time.Parse("15:04", clock)
	return t.Hour()*60 + t.Minute(), err == nil
}

func (p ModelPricing) ValidateTimeWindows() error {
	if p.BillingMode != BillingModeTokenPeak {
		if len(p.TimeWindows) > 0 {
			return fmt.Errorf("time windows require token_peak billing")
		}
		return nil
	}
	if len(p.Intervals) > 0 {
		return fmt.Errorf("time windows cannot be combined with context tiers")
	}
	// Require both directions on the fallback card, including explicit zero for free usage.
	if p.InputPrice == nil || p.OutputPrice == nil {
		return fmt.Errorf("fallback input and output token prices are required")
	}
	var occupied [1440]bool
	for i, w := range p.TimeWindows {
		a, okA := clockMinute(w.Start)
		b, okB := clockMinute(w.End)
		if !okA || !okB || a == b {
			return fmt.Errorf("time window %d needs distinct HH:MM endpoints", i+1)
		}
		hasPrice := false
		for _, price := range []*float64{w.InputPrice, w.OutputPrice, w.CacheReadPrice, w.CacheWritePrice, w.ImageInputPrice, w.ImageOutputPrice} {
			if price == nil {
				continue
			}
			if math.IsNaN(*price) || math.IsInf(*price, 0) || *price < 0 || *price > 1000 {
				return fmt.Errorf("invalid price in time window %d", i+1)
			}
			hasPrice = true
		}
		if !hasPrice {
			return fmt.Errorf("time window %d needs a token price", i+1)
		}
		for m := a; m != b; m = (m + 1) % 1440 {
			if occupied[m] {
				return fmt.Errorf("time window %d overlaps another window", i+1)
			}
			occupied[m] = true
		}
	}
	return nil
}

var shanghaiPricingZone = func() *time.Location {
	if zone, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return zone
	}
	return time.FixedZone("Asia/Shanghai", 8*3600)
}()

// AtTime projects a read-only schedule onto a normal token card before any billing-mode dispatch.
// Never modify the cached configuration or allow a missing override to replace an explicit zero.
func (p *ModelPricing) AtTime(at time.Time) *ModelPricing {
	if p == nil || p.BillingMode != BillingModeTokenPeak {
		return p
	}
	card := *p
	card.BillingMode = BillingModeToken
	card.TimeWindows = nil
	now := at.In(shanghaiPricingZone).Format("15:04")
	for _, w := range p.TimeWindows {
		inWindow := (w.Start < w.End && now >= w.Start && now < w.End) ||
			(w.Start > w.End && (now >= w.Start || now < w.End))
		if !inWindow {
			continue
		}
		if w.InputPrice != nil {
			card.InputPrice = w.InputPrice
		}
		if w.OutputPrice != nil {
			card.OutputPrice = w.OutputPrice
		}
		if w.CacheReadPrice != nil {
			card.CacheReadPrice = w.CacheReadPrice
		}
		if w.CacheWritePrice != nil {
			card.CacheWritePrice = w.CacheWritePrice
		}
		if w.ImageInputPrice != nil {
			card.ImageInputPrice = w.ImageInputPrice
		}
		if w.ImageOutputPrice != nil {
			card.ImageOutputPrice = w.ImageOutputPrice
		}
		break
	}
	return &card
}
