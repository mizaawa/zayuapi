package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// Reverse atomically compensates every database ledger touched by Apply. The
// original charge must already own its idempotency key; the reversal gets a
// second key, so repeated failed/cancelled polls are harmless.
func (r *usageBillingRepository) Reverse(ctx context.Context, cmd *service.UsageBillingReversalCommand) (_ *service.UsageBillingReversalResult, err error) {
	if cmd == nil {
		return &service.UsageBillingReversalResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	original := &cmd.Original
	if cmd.ReversalRequestID == "" || original.RequestID == "" || original.APIKeyID <= 0 {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	if !original.HasValidFingerprint() {
		return nil, service.ErrUsageBillingRequestConflict
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	originalApplied, err := usageBillingClaimMatches(
		ctx, tx, original.RequestID, original.APIKeyID, original.RequestFingerprint,
	)
	if err != nil {
		return nil, err
	}
	if !originalApplied {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return &service.UsageBillingReversalResult{Applied: false}, nil
	}

	applied, err := r.claimUsageBillingRequest(
		ctx, tx, cmd.ReversalRequestID, original.APIKeyID, service.UsageBillingReversalFingerprint(cmd),
	)
	if err != nil {
		return nil, err
	}
	if !applied {
		if err := tx.Rollback(); err != nil {
			return nil, err
		}
		tx = nil
		return &service.UsageBillingReversalResult{Applied: false}, nil
	}

	result := &service.UsageBillingReversalResult{Applied: true}
	if err := r.applyUsageBillingReversalEffects(ctx, tx, original, result); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE usage_logs
		SET actual_cost = 0, account_stats_cost = CASE WHEN account_stats_cost IS NULL THEN NULL ELSE 0 END
		WHERE request_id = $1 AND api_key_id = $2
	`, original.RequestID, original.APIKeyID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func usageBillingClaimMatches(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, fingerprint string) (bool, error) {
	var persistedFingerprint string
	err := tx.QueryRowContext(ctx, `
		SELECT request_fingerprint FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&persistedFingerprint)
	if err == nil {
		if strings.TrimSpace(persistedFingerprint) != strings.TrimSpace(fingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&persistedFingerprint)
	if err == nil {
		if strings.TrimSpace(persistedFingerprint) != strings.TrimSpace(fingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (r *usageBillingRepository) applyUsageBillingReversalEffects(
	ctx context.Context,
	tx *sql.Tx,
	original *service.UsageBillingCommand,
	result *service.UsageBillingReversalResult,
) error {
	if original == nil {
		return nil
	}
	chargedAt := original.ChargedAt
	if chargedAt.IsZero() {
		chargedAt = time.Now().UTC()
	}

	if original.BalanceCost > 0 {
		var balance float64
		err := tx.QueryRowContext(ctx, `
			UPDATE users SET balance = balance + $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
			RETURNING balance
		`, original.BalanceCost, original.UserID).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrUserNotFound
		}
		if err != nil {
			return err
		}
		result.NewBalance = &balance
	}

	if original.SubscriptionCost > 0 && original.SubscriptionID != nil {
		res, err := tx.ExecContext(ctx, `
			UPDATE user_subscriptions SET
				daily_usage_usd = CASE WHEN daily_window_start IS NULL OR daily_window_start <= $1 THEN GREATEST(0, daily_usage_usd - $2) ELSE daily_usage_usd END,
				weekly_usage_usd = CASE WHEN weekly_window_start IS NULL OR weekly_window_start <= $1 THEN GREATEST(0, weekly_usage_usd - $2) ELSE weekly_usage_usd END,
				monthly_usage_usd = CASE WHEN monthly_window_start IS NULL OR monthly_window_start <= $1 THEN GREATEST(0, monthly_usage_usd - $2) ELSE monthly_usage_usd END,
				updated_at = NOW()
			WHERE id = $3 AND deleted_at IS NULL
		`, chargedAt, original.SubscriptionCost, *original.SubscriptionID)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return service.ErrSubscriptionNotFound
		}
	}

	if original.APIKeyQuotaCost > 0 || original.APIKeyRateLimitCost > 0 {
		res, err := tx.ExecContext(ctx, `
			UPDATE api_keys SET
				quota_used = GREATEST(0, quota_used - $1),
				usage_5h = CASE WHEN window_5h_start IS NULL OR (window_5h_start <= $3 AND window_5h_start + INTERVAL '5 hours' > $3) THEN GREATEST(0, usage_5h - $2) ELSE usage_5h END,
				usage_1d = CASE WHEN window_1d_start IS NULL OR (window_1d_start <= $3 AND window_1d_start + INTERVAL '24 hours' > $3) THEN GREATEST(0, usage_1d - $2) ELSE usage_1d END,
				usage_7d = CASE WHEN window_7d_start IS NULL OR (window_7d_start <= $3 AND window_7d_start + INTERVAL '7 days' > $3) THEN GREATEST(0, usage_7d - $2) ELSE usage_7d END,
				status = CASE WHEN status = $5 AND (quota <= 0 OR GREATEST(0, quota_used - $1) < quota) THEN $4 ELSE status END,
				updated_at = NOW()
			WHERE id = $6 AND deleted_at IS NULL
		`, original.APIKeyQuotaCost, original.APIKeyRateLimitCost, chargedAt,
			service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted, original.APIKeyID)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return service.ErrAPIKeyNotFound
		}
	}

	if original.AccountQuotaCost > 0 {
		res, err := tx.ExecContext(ctx, `
			UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb)
				|| jsonb_build_object('quota_used', GREATEST(0, COALESCE((extra->>'quota_used')::numeric, 0) - $1))
				|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
					jsonb_build_object('quota_daily_used', CASE
						WHEN (extra->>'quota_daily_start')::timestamptz <= $2
							AND CASE WHEN COALESCE(extra->>'quota_daily_reset_mode', 'rolling') = 'fixed'
								THEN (extra->>'quota_daily_reset_at')::timestamptz > $2
								ELSE (extra->>'quota_daily_start')::timestamptz + INTERVAL '24 hours' > $2
							END
						THEN GREATEST(0, COALESCE((extra->>'quota_daily_used')::numeric, 0) - $1)
						ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0)
					END)
					ELSE '{}'::jsonb END
				|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
					jsonb_build_object('quota_weekly_used', CASE
						WHEN (extra->>'quota_weekly_start')::timestamptz <= $2
							AND CASE WHEN COALESCE(extra->>'quota_weekly_reset_mode', 'rolling') = 'fixed'
								THEN (extra->>'quota_weekly_reset_at')::timestamptz > $2
								ELSE (extra->>'quota_weekly_start')::timestamptz + INTERVAL '168 hours' > $2
							END
						THEN GREATEST(0, COALESCE((extra->>'quota_weekly_used')::numeric, 0) - $1)
						ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0)
					END)
					ELSE '{}'::jsonb END,
				updated_at = NOW()
			WHERE id = $3 AND deleted_at IS NULL
		`, original.AccountQuotaCost, chargedAt, original.AccountID)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return service.ErrAccountNotFound
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &original.AccountID, nil, nil); err != nil {
			return err
		}
	}

	if original.UserPlatformQuotaCost > 0 && strings.TrimSpace(original.Platform) != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE user_platform_quotas SET
				daily_usage_usd = CASE WHEN daily_window_start = $1 THEN GREATEST(0, daily_usage_usd - $3) ELSE daily_usage_usd END,
				weekly_usage_usd = CASE WHEN weekly_window_start = $2 THEN GREATEST(0, weekly_usage_usd - $3) ELSE weekly_usage_usd END,
				monthly_usage_usd = CASE WHEN monthly_window_start IS NULL OR (monthly_window_start <= $4 AND monthly_window_start + INTERVAL '30 days' > $4) THEN GREATEST(0, monthly_usage_usd - $3) ELSE monthly_usage_usd END,
				updated_at = NOW()
			WHERE user_id = $5 AND platform = $6 AND deleted_at IS NULL
		`, original.PlatformDailyWindowStart, original.PlatformWeeklyWindowStart,
			original.UserPlatformQuotaCost, chargedAt, original.UserID, strings.TrimSpace(original.Platform))
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	// Deleted keys have no counters to maintain; their in-flight usage still settles.
	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost, cmd.ChargedAt); err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost, cmd.ChargedAt)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	if cmd.UserPlatformQuotaCost > 0 && strings.TrimSpace(cmd.Platform) != "" {
		if err := incrementUsageBillingUserPlatformQuota(ctx, tx, cmd); err != nil {
			return err
		}
		result.UserPlatformQuotaApplied = true
	}

	return nil
}

func incrementUsageBillingUserPlatformQuota(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.UserPlatformQuotaCost <= 0 || strings.TrimSpace(cmd.Platform) == "" {
		return nil
	}
	chargedAt := cmd.ChargedAt
	if chargedAt.IsZero() {
		chargedAt = time.Now().UTC()
	}
	dailyStart := cmd.PlatformDailyWindowStart
	if dailyStart.IsZero() {
		dailyStart = chargedAt
	}
	weeklyStart := cmd.PlatformWeeklyWindowStart
	if weeklyStart.IsZero() {
		weeklyStart = chargedAt
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO user_platform_quotas (
			user_id, platform,
			daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
			daily_window_start, weekly_window_start, monthly_window_start,
			created_at, updated_at
		) VALUES ($1, $2, $3, $3, $3, $4, $5, $6, $6, $6)
		ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO UPDATE SET
			daily_usage_usd = CASE
				WHEN user_platform_quotas.daily_window_start IS NULL OR user_platform_quotas.daily_window_start < EXCLUDED.daily_window_start
				THEN EXCLUDED.daily_usage_usd
				WHEN user_platform_quotas.daily_window_start = EXCLUDED.daily_window_start
				THEN user_platform_quotas.daily_usage_usd + EXCLUDED.daily_usage_usd
				ELSE user_platform_quotas.daily_usage_usd
			END,
			weekly_usage_usd = CASE
				WHEN user_platform_quotas.weekly_window_start IS NULL OR user_platform_quotas.weekly_window_start < EXCLUDED.weekly_window_start
				THEN EXCLUDED.weekly_usage_usd
				WHEN user_platform_quotas.weekly_window_start = EXCLUDED.weekly_window_start
				THEN user_platform_quotas.weekly_usage_usd + EXCLUDED.weekly_usage_usd
				ELSE user_platform_quotas.weekly_usage_usd
			END,
			monthly_usage_usd = CASE
				WHEN user_platform_quotas.monthly_window_start IS NULL OR user_platform_quotas.monthly_window_start + INTERVAL '30 days' <= EXCLUDED.monthly_window_start
				THEN EXCLUDED.monthly_usage_usd
				WHEN EXCLUDED.monthly_window_start >= user_platform_quotas.monthly_window_start
				THEN user_platform_quotas.monthly_usage_usd + EXCLUDED.monthly_usage_usd
				ELSE user_platform_quotas.monthly_usage_usd
			END,
			daily_window_start = CASE
				WHEN user_platform_quotas.daily_window_start IS NULL OR user_platform_quotas.daily_window_start < EXCLUDED.daily_window_start
				THEN EXCLUDED.daily_window_start ELSE user_platform_quotas.daily_window_start END,
			weekly_window_start = CASE
				WHEN user_platform_quotas.weekly_window_start IS NULL OR user_platform_quotas.weekly_window_start < EXCLUDED.weekly_window_start
				THEN EXCLUDED.weekly_window_start ELSE user_platform_quotas.weekly_window_start END,
			monthly_window_start = CASE
				WHEN user_platform_quotas.monthly_window_start IS NULL OR user_platform_quotas.monthly_window_start + INTERVAL '30 days' <= EXCLUDED.monthly_window_start
				THEN EXCLUDED.monthly_window_start
				ELSE user_platform_quotas.monthly_window_start
			END,
			updated_at = GREATEST(user_platform_quotas.updated_at, EXCLUDED.updated_at)
	`, cmd.UserID, strings.TrimSpace(cmd.Platform), cmd.UserPlatformQuotaCost, dailyStart, weeklyStart, chargedAt)
	return err
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64, chargedAt time.Time) error {
	if chargedAt.IsZero() {
		chargedAt = time.Now().UTC()
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= $3 THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= $3 THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= $3 THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= $3 THEN $3 ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= $3 THEN date_trunc('day', $3::timestamptz) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= $3 THEN date_trunc('day', $3::timestamptz) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID, chargedAt)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64, chargedAt time.Time) (*service.AccountQuotaState, error) {
	if chargedAt.IsZero() {
		chargedAt = time.Now().UTC()
	}
	chargedAtText := chargedAt.UTC().Format(time.RFC3339Nano)
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $3
					ELSE COALESCE(extra->>'quota_daily_start', $3) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $3
					ELSE COALESCE(extra->>'quota_weekly_start', $3) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID, chargedAtText)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
