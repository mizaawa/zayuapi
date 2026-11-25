package domain

import (
	"math"
	"testing"
	"time"
)

func TestTokenPriceWindowBoundaries(t *testing.T) {
	base, override, free := 1., 2., 0.
	card := ModelPricing{BillingMode: BillingModeTokenPeak, InputPrice: &base, OutputPrice: &base, ImageInputPrice: &base,
		TimeWindows: []TokenPriceWindow{{Start: "23:30", End: "02:15", InputPrice: &override, ImageInputPrice: &free}, {Start: "02:15", End: "04:00", InputPrice: &free}}}
	if err := card.ValidateTimeWindows(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		utc          string
		input, image float64
	}{{"15:29", 1, 1}, {"15:30", 2, 0}, {"16:00", 2, 0}, {"18:14", 2, 0}, {"18:15", 0, 1}, {"20:00", 1, 1}} {
		at, err := time.Parse(time.RFC3339, "2026-11-25T"+tc.utc+":00Z")
		if err != nil {
			t.Fatal(err)
		}
		got := card.AtTime(at)
		if got.BillingMode != BillingModeToken || *got.InputPrice != tc.input || *got.ImageInputPrice != tc.image || *got.OutputPrice != base {
			t.Fatalf("%s: %+v", tc.utc, got)
		}
	}
	if len(card.TimeWindows) != 2 || card.BillingMode != BillingModeTokenPeak || *card.InputPrice != base {
		t.Fatal("cached card mutated")
	}
}
func TestTokenPriceWindowRejectsInvalidConfig(t *testing.T) {
	price, invalid := 1., math.Inf(1)
	good := TokenPriceWindow{Start: "23:00", End: "02:00", InputPrice: &price}
	for _, w := range []TokenPriceWindow{{Start: "01:59", End: "14:00", InputPrice: &price}, {Start: "24:00", End: "04:00", InputPrice: &price}, {Start: "9:30", End: "10:00", InputPrice: &price}, {Start: "09:60", End: "10:00", InputPrice: &price}, {Start: "09:00", End: "09:00", InputPrice: &price}, {Start: "06:00", End: "07:00"}, {Start: "06:00", End: "07:00", InputPrice: &invalid}} {
		p := ModelPricing{BillingMode: BillingModeTokenPeak, InputPrice: &price, OutputPrice: &price, TimeWindows: []TokenPriceWindow{good, w}}
		if p.ValidateTimeWindows() == nil {
			t.Errorf("accepted %+v", w)
		}
	}
	p := ModelPricing{BillingMode: BillingModeTokenPeak, InputPrice: &price}
	if p.ValidateTimeWindows() == nil {
		t.Fatal("missing fallback")
	}
	p.BillingMode = BillingModeToken
	p.TimeWindows = []TokenPriceWindow{good}
	if p.ValidateTimeWindows() == nil {
		t.Fatal("ignored schedule in non-scheduled mode")
	}
}
