package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const frozenBalanceRecoveryInterval = 30 * time.Second

// Recovery has its own loop: slow provider polls and a disabled batch queue
// must not delay the return of a user's frozen funds.
func (s *OpenAIGatewayService) startFrozenBalanceRecovery() {
	repo, ok := s.usageBillingRepo.(ExpiredBalanceHoldRecovery)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.frozenBalanceCancel = cancel
	s.frozenBalanceDone = make(chan struct{})
	go func() {
		defer close(s.frozenBalanceDone)
		ticker := time.NewTicker(frozenBalanceRecoveryInterval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			checkCtx, checkCancel := context.WithTimeout(ctx, 25*time.Second)
			err := s.recoverFrozenBalanceOnce(checkCtx, repo, time.Now())
			checkCancel()
			if err != nil && ctx.Err() == nil {
				logger.L().Warn("frozen_balance.recovery_retry", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *OpenAIGatewayService) recoverFrozenBalanceOnce(ctx context.Context, repo ExpiredBalanceHoldRecovery, now time.Time) error {
	hours := s.settingService.GetFrozenBalanceReleaseHours(ctx)
	users, recoveryErr := repo.RecoverExpiredBalanceHolds(ctx, now.Add(-time.Duration(hours)*time.Hour), 100)
	errs := []error{recoveryErr}
	// A timed-out scan may still have committed refunds. Give those cache
	// invalidations their own bounded context instead of an expired scan context.
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	seen := make(map[int64]struct{}, len(users))
	// The repository may return successful refunds together with a retryable
	// error for another row. Always invalidate the successful users' caches.
	for _, userID := range users {
		if _, ok := seen[userID]; ok || userID <= 0 {
			continue
		}
		seen[userID] = struct{}{}
		if keys := s.mediaBillingAPIKeys.Load(); keys != nil {
			keys.InvalidateAuthCacheByUserID(cacheCtx, userID)
		}
		if s.billingCacheService != nil {
			if err := s.billingCacheService.InvalidateUserBalance(cacheCtx, userID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
