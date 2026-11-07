package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// displayCNYRateForLogs annotates only records whose channel configuration has
// not changed since the record was written. Earlier/legacy rows remain USD;
// current toggles and rates must never reinterpret historical USD amounts.
func (s *UsageService) displayCNYRateForLogs(ctx context.Context, logs []UsageLog) {
	if s.entClient == nil || len(logs) == 0 {
		return
	}
	ids := make(map[int64]struct{})
	for i := range logs {
		if logs[i].ChannelID != nil && logs[i].Group != nil {
			ids[*logs[i].ChannelID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return
	}
	values := make([]string, 0, len(ids))
	for id := range ids {
		values = append(values, strconv.FormatInt(id, 10))
	}
	rows, err := s.entClient.QueryContext(ctx, "SELECT id, features_config, updated_at FROM channels WHERE id IN ("+strings.Join(values, ",")+")")
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()
	type configAtTime struct {
		features map[string]any
		updated  time.Time
	}
	channels := make(map[int64]configAtTime, len(ids))
	for rows.Next() {
		var id int64
		var raw []byte
		var updated time.Time
		if rows.Scan(&id, &raw, &updated) != nil {
			return
		}
		var features map[string]any
		if json.Unmarshal(raw, &features) != nil {
			continue
		}
		channels[id] = configAtTime{features: features, updated: updated}
	}
	if rows.Err() != nil {
		return
	}
	for i := range logs {
		log := &logs[i]
		if log.ChannelID == nil || log.Group == nil {
			continue
		}
		cfg, ok := channels[*log.ChannelID]
		if !ok || !channelCurrencyCurrentForLog(log.CreatedAt, cfg.updated) {
			continue
		}
		platform := log.Group.Platform
		if platform == PlatformComposite {
			platform = PlatformCustom
		}
		currency := (&Channel{FeaturesConfig: cfg.features}).PricingCurrency(platform)
		if currency.DiscountedCNY {
			rate := currency.CNYPerUSD
			log.DisplayCNYRate = &rate
		}
	}
}

func channelCurrencyCurrentForLog(logCreated, channelUpdated time.Time) bool {
	return !logCreated.Before(channelUpdated)
}
