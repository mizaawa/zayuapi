package service

import (
	"context"
	"fmt"
)

// GetPaymentAccessSettings bypasses public-settings caches so revocations and
// disabling payment also apply to the browser's OAuth handoff immediately.
func (s *SettingService) GetPaymentAccessSettings(ctx context.Context) (enabled, exclusive bool, err error) {
	if s == nil || s.settingRepo == nil {
		return false, false, fmt.Errorf("payment settings are unavailable")
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingPaymentEnabled, SettingPaymentExclusiveEnabled})
	if err != nil {
		return false, false, err
	}
	return values[SettingPaymentEnabled] == "true", values[SettingPaymentExclusiveEnabled] == "true", nil
}
