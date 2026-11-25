package domain

import (
	"strings"
	"time"
)

// BillingMode describes how a model is billed.
type BillingMode string

const (
	BillingModeToken      BillingMode = "token"
	BillingModeTokenPeak  BillingMode = "token_peak"
	BillingModePerRequest BillingMode = "per_request"
	BillingModeImage      BillingMode = "image"
	BillingModeVideo      BillingMode = "video"
)

func (m BillingMode) IsValid() bool {
	switch m {
	case BillingModeToken, BillingModeTokenPeak, BillingModePerRequest, BillingModeImage, BillingModeVideo, "":
		return true
	}
	return false
}

func (m BillingMode) IsValidUsageFilter() bool {
	return m != BillingModeTokenPeak && m.IsValid() // schedules settle as token usage
}

// ModelPricing is shared by channel and group custom pricing.
type ModelPricing struct {
	ID               int64              `json:"id"`
	ChannelID        int64              `json:"channel_id"`
	Platform         string             `json:"platform"`
	Models           []string           `json:"models"`
	BillingMode      BillingMode        `json:"billing_mode"`
	InputPrice       *float64           `json:"input_price"`
	OutputPrice      *float64           `json:"output_price"`
	CacheWritePrice  *float64           `json:"cache_write_price"`
	CacheReadPrice   *float64           `json:"cache_read_price"`
	ImageInputPrice  *float64           `json:"image_input_price"`
	ImageOutputPrice *float64           `json:"image_output_price"`
	PerRequestPrice  *float64           `json:"per_request_price"`
	TimeWindows      []TokenPriceWindow `json:"time_windows,omitempty"`
	Intervals        []PricingInterval  `json:"intervals"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

// PricingInterval describes a context interval (min, max] or a named media tier.
type PricingInterval struct {
	ID              int64     `json:"id"`
	PricingID       int64     `json:"pricing_id"`
	MinTokens       int       `json:"min_tokens"`
	MaxTokens       *int      `json:"max_tokens"`
	TierLabel       string    `json:"tier_label"`
	InputPrice      *float64  `json:"input_price"`
	OutputPrice     *float64  `json:"output_price"`
	CacheWritePrice *float64  `json:"cache_write_price"`
	CacheReadPrice  *float64  `json:"cache_read_price"`
	PerRequestPrice *float64  `json:"per_request_price"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func FindMatchingPricingInterval(intervals []PricingInterval, totalTokens int) *PricingInterval {
	for i := range intervals {
		iv := &intervals[i]
		if totalTokens > iv.MinTokens && (iv.MaxTokens == nil || totalTokens <= *iv.MaxTokens) {
			return iv
		}
	}
	return nil
}

func (p *ModelPricing) GetIntervalForContext(totalTokens int) *PricingInterval {
	return FindMatchingPricingInterval(p.Intervals, totalTokens)
}

func (p *ModelPricing) GetTierByLabel(label string) *PricingInterval {
	for i := range p.Intervals {
		if strings.EqualFold(p.Intervals[i].TierLabel, label) {
			return &p.Intervals[i]
		}
	}
	return nil
}

// Clone copies mutable slices; price pointers are shared and must be read-only.
func (p ModelPricing) Clone() ModelPricing {
	cp := p
	cp.TimeWindows = append([]TokenPriceWindow(nil), p.TimeWindows...)
	if p.Models != nil {
		cp.Models = make([]string, len(p.Models))
		copy(cp.Models, p.Models)
	}
	if p.Intervals != nil {
		cp.Intervals = make([]PricingInterval, len(p.Intervals))
		copy(cp.Intervals, p.Intervals)
	}
	return cp
}
