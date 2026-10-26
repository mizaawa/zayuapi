package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ProviderRecords are stored in a private setting. They are deliberately not
// included in SystemSettings, so the generic settings API never returns them.
const providerRecordsSettingKey = "admin_provider_records_v1"

func (s *SettingService) LoadProviderRecords(ctx context.Context, out any) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("provider settings unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, providerRecordsSettingKey)
	if errors.Is(err, ErrSettingNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return fmt.Errorf("decode providers: %w", err)
	}
	return nil
}

func (s *SettingService) SaveProviderRecords(ctx context.Context, records any) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("provider settings unavailable")
	}
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("encode providers: %w", err)
	}
	return s.settingRepo.Set(ctx, providerRecordsSettingKey, string(data))
}
