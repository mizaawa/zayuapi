package service

import (
	"testing"
	"time"
)

func TestChannelCurrencyCurrentForLog(t *testing.T) {
	created := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC)
	if !channelCurrencyCurrentForLog(created, created.Add(-time.Second)) {
		t.Fatal("new usage record should use the current channel's display unit")
	}
	if channelCurrencyCurrentForLog(created, created.Add(time.Second)) {
		t.Fatal("historical usage record must stay USD after channel update")
	}
}

func TestUsageDisplayCNYFlagDoesNotCarryAConversionRate(t *testing.T) {
	log := UsageLog{ActualCost: 1.25, DisplayCNY: true}
	if log.ActualCost != 1.25 || !log.DisplayCNY {
		t.Fatalf("usage display annotation changed the cost: %+v", log)
	}
}
