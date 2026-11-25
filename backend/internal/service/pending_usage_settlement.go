package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

func durableUsage(repo UsageBillingRepository, usage *UsageLog, reservation string) (DurableUsageBilling, bool) {
	durable, ok := repo.(DurableUsageBilling)
	return durable, ok && reservation == "" && usage != nil && usage.ImageCount == 0 && usage.VideoCount == 0
}

func transientSettlementError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		code := state.SQLState()
		return strings.HasPrefix(code, "08") || code == "57014" || code == "40001" || code == "40P01"
	}
	return false
}

func settleUsageReceipt(ctx context.Context, repo DurableUsageBilling, cmd *UsageBillingCommand, usage *UsageLog) (*UsageBillingApplyResult, error) {
	base := context.WithoutCancel(ctx)
	var err error
	for _, timeout := range []time.Duration{15 * time.Second, 30 * time.Second, 90 * time.Second} {
		stageCtx, cancel := context.WithTimeout(base, timeout)
		err = repo.StageUsage(stageCtx, cmd, usage)
		cancel()
		if !transientSettlementError(err) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	settleCtx, cancel := context.WithTimeout(base, 90*time.Second)
	defer cancel()
	// On failure the durable receipt remains pending; never replace it with a $0 log.
	return repo.SettleUsage(settleCtx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func invalidateSettledUsage(ctx context.Context, cache *BillingCacheService, key *UsageBillingRecoveryKey, auth any) {
	if cache != nil {
		_ = cache.InvalidateUserBalance(ctx, key.UserID)
		_ = cache.InvalidateAPIKeyRateLimit(ctx, key.APIKeyID)
		if key.GroupID > 0 {
			_ = cache.InvalidateSubscription(ctx, key.UserID, key.GroupID)
		}
		if key.Platform != "" {
			_ = cache.InvalidateUserPlatformQuota(ctx, key.UserID, key.Platform)
		}
	}
	if invalidator, ok := auth.(interface{ InvalidateAuthCacheByUserID(context.Context, int64) }); ok {
		invalidator.InvalidateAuthCacheByUserID(ctx, key.UserID)
	}
}

func (s *OpenAIGatewayService) startUsageSettlementRecovery() {
	repo, ok := s.usageBillingRepo.(DurableUsageBilling)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.usageRecoveryCancel, s.usageRecoveryDone = cancel, make(chan struct{})
	go func() {
		defer close(s.usageRecoveryDone)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			for range 20 {
				if ctx.Err() != nil {
					return
				}
				claimCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				key, err := repo.ClaimPendingUsage(claimCtx)
				stop()
				if err != nil || key == nil {
					if err != nil && ctx.Err() == nil {
						logger.L().Warn("usage_settlement.scan_failed", zap.Error(err))
					}
					break
				}
				applyCtx, stop := context.WithTimeout(ctx, 90*time.Second)
				_, err = repo.SettleUsage(applyCtx, key.RequestID, key.APIKeyID, key.Fingerprint)
				stop()
				if err != nil {
					if ctx.Err() == nil {
						logger.L().Error("usage_settlement.recovery_failed", zap.String("request_id", key.RequestID), zap.Error(err))
					}
					continue
				}
				cacheCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				invalidateSettledUsage(cacheCtx, s.billingCacheService, key, s.mediaBillingAPIKeys.Load())
				stop()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
