package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.MediaBillingRepository = (*usageBillingRepository)(nil)

// Keep the confirmed outcome before touching balances so a transient ledger
// failure can be retried after a restart without converting success to refund.
func (r *usageBillingRepository) StageMediaOutcome(ctx context.Context, id string, quote *service.MediaBillingQuote) error {
	var data any
	if quote != nil {
		encoded, err := json.Marshal(quote)
		if err != nil {
			return err
		}
		data = encoded
	}
	_, err := r.db.ExecContext(ctx, `UPDATE media_billing_reservations SET final_quote=COALESCE(final_quote,$2),refund_requested=$3,updated_at=NOW(),next_check_at=NOW()
		WHERE id=$1 AND state IN ('reserved','pending') AND NOT refund_requested AND (final_quote IS NULL OR NOT $3)`, id, data, quote == nil)
	return err
}

func (r *usageBillingRepository) ReserveMedia(ctx context.Context, reservation *service.MediaBillingReservation) error {
	cmd := &reservation.Quote.Command
	cmd.Normalize()
	for _, amount := range []float64{cmd.BalanceCost, cmd.SubscriptionCost, cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost, cmd.AccountQuotaCost, cmd.UserPlatformQuotaCost} {
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return errors.New("invalid media price")
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkMediaFunds(ctx, tx, cmd); err != nil {
		return err
	}
	data, err := json.Marshal(reservation)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO media_billing_reservations (id,user_id,api_key_id,account_id,group_id,quote) VALUES ($1,$2,$3,$4,$5,$6)`, reservation.ID, cmd.UserID, cmd.APIKeyID, cmd.AccountID, reservation.GroupID, data); err != nil {
		return err
	}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, &service.UsageBillingApplyResult{}); err != nil {
		return err
	}
	return tx.Commit()
}

// Every request for a user takes the same row lock, including requests on
// different API keys and application replicas. A cached balance cannot admit
// multiple reservations against the same remaining funds.
func checkMediaFunds(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	var balance float64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT balance,status FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, cmd.UserID).Scan(&balance, &status); err != nil {
		return err
	}
	if status != service.StatusActive || cmd.BalanceCost > 0 && balance < cmd.BalanceCost {
		return service.ErrInsufficientBalance
	}
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		var daily, weekly, monthly, dayLimit, weekLimit, monthLimit float64
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT s.daily_usage_usd,s.weekly_usage_usd,s.monthly_usage_usd,COALESCE(g.daily_limit_usd,0),COALESCE(g.weekly_limit_usd,0),COALESCE(g.monthly_limit_usd,0),s.expires_at
		FROM user_subscriptions s JOIN groups g ON g.id=s.group_id WHERE s.id=$1 AND s.user_id=$2 AND s.deleted_at IS NULL AND g.deleted_at IS NULL FOR UPDATE OF s`, *cmd.SubscriptionID, cmd.UserID).Scan(&daily, &weekly, &monthly, &dayLimit, &weekLimit, &monthLimit, &expires); err != nil {
			return err
		}
		if !expires.After(time.Now()) {
			return service.ErrSubscriptionNotFound
		}
		if dayLimit > 0 && daily+cmd.SubscriptionCost > dayLimit {
			return service.ErrDailyLimitExceeded
		}
		if weekLimit > 0 && weekly+cmd.SubscriptionCost > weekLimit {
			return service.ErrWeeklyLimitExceeded
		}
		if monthLimit > 0 && monthly+cmd.SubscriptionCost > monthLimit {
			return service.ErrMonthlyLimitExceeded
		}
	}
	var quota, used, rate5h, rate1d, rate7d, used5h, used1d, used7d float64
	if err := tx.QueryRowContext(ctx, `SELECT status,quota,quota_used,rate_limit_5h,rate_limit_1d,rate_limit_7d,
		CASE WHEN window_5h_start + INTERVAL '5 hours' > NOW() THEN usage_5h ELSE 0 END,
		CASE WHEN window_1d_start + INTERVAL '24 hours' > NOW() THEN usage_1d ELSE 0 END,
		CASE WHEN window_7d_start + INTERVAL '7 days' > NOW() THEN usage_7d ELSE 0 END
		FROM api_keys WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) FOR UPDATE`, cmd.APIKeyID, cmd.UserID).Scan(&status, &quota, &used, &rate5h, &rate1d, &rate7d, &used5h, &used1d, &used7d); err != nil {
		return err
	}
	if status != service.StatusAPIKeyActive || quota > 0 && used+cmd.APIKeyQuotaCost > quota {
		return service.ErrInsufficientBalance
	}
	if rate5h > 0 && used5h+cmd.APIKeyRateLimitCost > rate5h {
		return service.ErrAPIKeyRateLimit5hExceeded
	}
	if rate1d > 0 && used1d+cmd.APIKeyRateLimitCost > rate1d {
		return service.ErrAPIKeyRateLimit1dExceeded
	}
	if rate7d > 0 && used7d+cmd.APIKeyRateLimitCost > rate7d {
		return service.ErrAPIKeyRateLimit7dExceeded
	}
	if cmd.UserPlatformQuotaCost > 0 {
		var allowed bool
		err := tx.QueryRowContext(ctx, `SELECT
			(COALESCE(daily_limit_usd,0)<=0 OR (CASE WHEN daily_window_start >= $4 THEN daily_usage_usd ELSE 0 END)+$3<=daily_limit_usd) AND
			(COALESCE(weekly_limit_usd,0)<=0 OR (CASE WHEN weekly_window_start >= $5 THEN weekly_usage_usd ELSE 0 END)+$3<=weekly_limit_usd) AND
			(COALESCE(monthly_limit_usd,0)<=0 OR (CASE WHEN monthly_window_start + INTERVAL '30 days' > NOW() THEN monthly_usage_usd ELSE 0 END)+$3<=monthly_limit_usd)
			FROM user_platform_quotas WHERE user_id=$1 AND platform=$2 AND deleted_at IS NULL FOR UPDATE`, cmd.UserID, cmd.Platform, cmd.UserPlatformQuotaCost, cmd.PlatformDailyWindowStart, cmd.PlatformWeeklyWindowStart).Scan(&allowed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && !allowed {
			return service.ErrDailyLimitExceeded
		}
	}
	return nil
}

func (r *usageBillingRepository) SettleMedia(ctx context.Context, id string, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	reservation, state, err := readMediaReservation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if reservation.Quote.Command.UserID != cmd.UserID || reservation.Quote.Command.APIKeyID != cmd.APIKeyID || cmd.RequestID != id {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if state == "settled" {
		return &service.UsageBillingApplyResult{}, nil
	}
	if state == "refunded" {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if reservation.RefundPending {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if reservation.FinalQuote != nil {
		*cmd = reservation.FinalQuote.Command
	}
	cmd.Normalize()
	claimed, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if err := r.reverseMediaEffects(ctx, tx, &reservation.Quote.Command); err != nil {
		return nil, err
	}
	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_billing_reservations SET state='settled',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *usageBillingRepository) RefundMedia(ctx context.Context, id string) (*service.MediaBillingReservation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	reservation, state, err := readMediaReservation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if state == "refunded" || state == "settled" {
		return nil, nil
	}
	if reservation.FinalQuote != nil {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if err := r.reverseMediaEffects(ctx, tx, &reservation.Quote.Command); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE media_billing_reservations SET state='refunded',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return reservation, nil
}

func readMediaReservation(ctx context.Context, tx *sql.Tx, id string) (*service.MediaBillingReservation, string, error) {
	var data, final []byte
	var refund bool
	var state, task, endpoint string
	if err := tx.QueryRowContext(ctx, `SELECT quote,state,task_id,endpoint,final_quote,refund_requested FROM media_billing_reservations WHERE id=$1 FOR UPDATE`, id).Scan(&data, &state, &task, &endpoint, &final, &refund); err != nil {
		return nil, "", err
	}
	var r service.MediaBillingReservation
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, "", err
	}
	r.TaskID, r.Endpoint = task, service.GrokMediaEndpoint(endpoint)
	r.RefundPending = refund
	if len(final) > 0 {
		if err := json.Unmarshal(final, &r.FinalQuote); err != nil {
			return nil, "", err
		}
	}
	return &r, state, nil
}

// A deleted API key or provider account must not prevent returning the user's
// money. There are no live quota counters to restore on those deleted rows.
func (r *usageBillingRepository) reverseMediaEffects(ctx context.Context, tx *sql.Tx, original *service.UsageBillingCommand) error {
	command := *original
	var keyExists, accountExists, subscriptionExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM api_keys WHERE id=$1 AND deleted_at IS NULL),EXISTS(SELECT 1 FROM accounts WHERE id=$2 AND deleted_at IS NULL),EXISTS(SELECT 1 FROM user_subscriptions WHERE id=$3 AND deleted_at IS NULL)`, command.APIKeyID, command.AccountID, command.SubscriptionID).Scan(&keyExists, &accountExists, &subscriptionExists); err != nil {
		return err
	}
	if !keyExists {
		command.APIKeyQuotaCost, command.APIKeyRateLimitCost = 0, 0
	}
	if !accountExists {
		command.AccountQuotaCost = 0
	}
	if !subscriptionExists {
		command.SubscriptionCost = 0
	}
	return r.applyUsageBillingReversalEffects(ctx, tx, &command, &service.UsageBillingReversalResult{})
}

func (r *usageBillingRepository) BindMediaTask(ctx context.Context, id, task string, endpoint service.GrokMediaEndpoint) error {
	result, err := r.db.ExecContext(ctx, `UPDATE media_billing_reservations SET task_id=$2,endpoint=$3,state='pending',updated_at=NOW() WHERE id=$1 AND state='reserved'`, id, task, endpoint)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return service.ErrUsageBillingRequestConflict
	}
	return err
}

func (r *usageBillingRepository) FindMediaTask(ctx context.Context, task string, userID, keyID int64) (*service.MediaBillingReservation, error) {
	var data []byte
	err := r.db.QueryRowContext(ctx, `SELECT quote FROM media_billing_reservations WHERE task_id=$1 AND user_id=$2 AND api_key_id=$3`, task, userID, keyID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result service.MediaBillingReservation
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	result.TaskID = task
	return &result, nil
}

func (r *usageBillingRepository) PendingMedia(ctx context.Context, limit int) ([]*service.MediaBillingReservation, error) {
	rows, err := r.db.QueryContext(ctx, `UPDATE media_billing_reservations SET next_check_at=NOW()+INTERVAL '2 minutes' WHERE id IN (
		SELECT id FROM media_billing_reservations WHERE state IN ('pending','reserved') AND next_check_at<=NOW() AND (task_id<>'' OR final_quote IS NOT NULL OR refund_requested) ORDER BY next_check_at LIMIT $1 FOR UPDATE SKIP LOCKED
	) RETURNING quote,task_id,endpoint,final_quote,refund_requested`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []*service.MediaBillingReservation{}
	for rows.Next() {
		var data, final []byte
		var refund bool
		var task, endpoint string
		if err := rows.Scan(&data, &task, &endpoint, &final, &refund); err != nil {
			return nil, err
		}
		var item service.MediaBillingReservation
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		item.TaskID, item.Endpoint = task, service.GrokMediaEndpoint(endpoint)
		item.RefundPending = refund
		if len(final) > 0 {
			if err := json.Unmarshal(final, &item.FinalQuote); err != nil {
				return nil, err
			}
		}
		items = append(items, &item)
	}
	return items, rows.Err()
}
