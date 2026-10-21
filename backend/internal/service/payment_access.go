package service

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type PaymentAccess struct {
	Enabled          bool `json:"enabled"`
	Allowed          bool `json:"allowed"`
	ExclusiveEnabled bool `json:"exclusive_enabled"`
}

// GetPaymentAccess reads the current grant and settings without an auth cache.
// Revoking a grant must apply to already-issued sessions and resume tokens.
func (s *PaymentService) GetPaymentAccess(ctx context.Context, userID int64) (*PaymentAccess, error) {
	if s.configService == nil || s.configService.settingRepo == nil {
		return nil, fmt.Errorf("payment configuration is unavailable")
	}
	values, err := s.configService.settingRepo.GetMultiple(ctx, []string{SettingPaymentEnabled, SettingPaymentExclusiveEnabled})
	if err != nil {
		return nil, fmt.Errorf("get payment access settings: %w", err)
	}
	access := &PaymentAccess{
		Enabled:          values[SettingPaymentEnabled] == "true",
		ExclusiveEnabled: values[SettingPaymentExclusiveEnabled] == "true",
	}
	access.Allowed = !access.ExclusiveEnabled
	if !access.ExclusiveEnabled {
		return access, nil
	}
	if userID <= 0 {
		return access, nil
	}
	if s.entClient == nil {
		return nil, fmt.Errorf("payment user repository is unavailable")
	}
	user, err := s.entClient.User.Get(ctx, userID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return access, nil
		}
		return nil, err
	}
	access.Allowed = user.Status == StatusActive && user.ExclusiveRechargeEnabled
	return access, nil
}

func (s *PaymentService) RequirePaymentAccess(ctx context.Context, userID int64) error {
	access, err := s.GetPaymentAccess(ctx, userID)
	if err != nil {
		return err
	}
	if !access.Allowed {
		return infraerrors.Forbidden("PAYMENT_ACCESS_DENIED", "exclusive recharge permission is required")
	}
	return nil
}
