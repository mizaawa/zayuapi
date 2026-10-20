package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type expiredBalanceHold struct {
	id     string
	userID int64
	keyID  int64
}

// RecoverExpiredBalanceHolds works without Redis or the batch queue. Every
// refund and its receipt commit together and use the same locks as settlement.
// A rollback from v0.2.165 may leave token holds in its optional legacy table;
// recovering those rows does not restore token precharge or admission checks.
func (r *usageBillingRepository) RecoverExpiredBalanceHolds(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrBillingServiceUnavailable
	}
	if cutoff.IsZero() {
		return nil, errors.New("frozen balance release cutoff is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	users, batchErr := r.recoverExpiredBatchHolds(ctx, cutoff, limit)
	legacyUsers, legacyErr := r.recoverExpiredLegacyTokenHolds(ctx, cutoff, limit)
	return append(users, legacyUsers...), errors.Join(batchErr, legacyErr)
}

func (r *usageBillingRepository) recoverExpiredBatchHolds(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT j.batch_id,j.user_id,j.api_key_id
		FROM batch_image_jobs j
		JOIN (
			SELECT request_id,api_key_id,created_at FROM usage_billing_dedup
			UNION ALL SELECT request_id,api_key_id,created_at FROM usage_billing_dedup_archive
		) h ON h.request_id='batch_image_hold:'||j.batch_id AND h.api_key_id=j.api_key_id
		WHERE h.created_at<=$1 AND COALESCE(j.hold_amount,j.estimated_cost)>0
		AND NOT EXISTS (SELECT 1 FROM balance_hold_releases e WHERE e.source_kind='batch_image' AND e.source_id=j.batch_id AND e.api_key_id=j.api_key_id)
		AND NOT EXISTS (SELECT 1 FROM usage_billing_dedup d WHERE d.api_key_id=j.api_key_id AND d.request_id IN ('batch_image_capture:'||j.batch_id,'batch_image_release:'||j.batch_id))
		AND NOT EXISTS (SELECT 1 FROM usage_billing_dedup_archive d WHERE d.api_key_id=j.api_key_id AND d.request_id IN ('batch_image_capture:'||j.batch_id,'batch_image_release:'||j.batch_id))
		ORDER BY h.created_at,j.batch_id LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	items, err := scanExpiredBalanceHolds(rows)
	if err != nil {
		return nil, err
	}
	users := []int64{}
	var errs []error
	for _, item := range items {
		applied, err := r.releaseExpiredBatchHold(ctx, item, cutoff)
		if err != nil {
			errs = append(errs, fmt.Errorf("expired batch hold %s: %w", item.id, err))
		} else if applied {
			users = append(users, item.userID)
		}
	}
	return users, errors.Join(errs...)
}

func scanExpiredBalanceHolds(rows *sql.Rows) ([]expiredBalanceHold, error) {
	defer func() { _ = rows.Close() }()
	items := []expiredBalanceHold{}
	for rows.Next() {
		var item expiredBalanceHold
		if err := rows.Scan(&item.id, &item.userID, &item.keyID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *usageBillingRepository) releaseExpiredBatchHold(ctx context.Context, item expiredBalanceHold, cutoff time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockHash("batch_image_billing:"+item.id)); err != nil {
		return false, err
	}
	cmd := &service.BatchImageBalanceHoldCommand{BatchID: item.id, UserID: item.userID, APIKeyID: item.keyID}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(hold_amount,estimated_cost),COALESCE(request_hash,'') FROM batch_image_jobs
		WHERE batch_id=$1 AND user_id=$2 AND api_key_id=$3
		AND EXISTS (SELECT 1 FROM usage_billing_dedup WHERE request_id=$4 AND api_key_id=$3 AND created_at<=$5
		UNION ALL SELECT 1 FROM usage_billing_dedup_archive WHERE request_id=$4 AND api_key_id=$3 AND created_at<=$5)`, item.id, item.userID, item.keyID, service.BatchImageHoldRequestID(item.id), cutoff).Scan(&cmd.HoldAmount, &cmd.RequestPayloadHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cmd.Normalize()
	if cmd.HoldAmount <= 0 {
		return false, nil
	}
	for _, id := range []string{service.BatchImageCaptureRequestID(item.id), service.BatchImageReleaseRequestID(item.id)} {
		done, err := batchImageHoldClaimExists(ctx, tx, id, item.keyID)
		if err != nil || done {
			return false, err
		}
	}
	// This transaction is the first expiration marker; an existing marker
	// means another worker already returned the funds.
	var marker int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM balance_hold_releases WHERE source_kind='batch_image' AND source_id=$1 AND api_key_id=$2`, item.id, item.keyID).Scan(&marker)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if _, err := releaseUsageBillingBatchImageBalance(ctx, tx, cmd); err != nil {
		return false, err
	}
	if err := recordExpiredBalanceHold(ctx, tx, "batch_image", item, cmd.HoldAmount); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func batchImageHoldExpired(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (bool, error) {
	var owner int64
	var amount float64
	err := tx.QueryRowContext(ctx, `SELECT user_id,amount FROM balance_hold_releases
		WHERE source_kind='batch_image' AND source_id=$1 AND api_key_id=$2`, cmd.BatchID, cmd.APIKeyID).Scan(&owner, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if owner != cmd.UserID || amount != cmd.HoldAmount {
		return false, service.ErrUsageBillingRequestConflict
	}
	return true, nil
}

// Expiration ends the hold, not the provider job. Its eventual successful
// output follows normal postpaid billing and never consumes another job's hold.
func captureExpiredBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	balance, _, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.ActualAmount)
	if err != nil {
		return nil, err
	}
	return &service.BatchImageBalanceHoldResult{NewBalance: &balance}, nil
}

func recordExpiredBalanceHold(ctx context.Context, tx *sql.Tx, kind string, item expiredBalanceHold, amount float64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO balance_hold_releases(source_kind,source_id,api_key_id,user_id,amount) VALUES($1,$2,$3,$4,$5)`, kind, item.id, item.keyID, item.userID, amount)
	return err
}

func (r *usageBillingRepository) recoverExpiredLegacyTokenHolds(ctx context.Context, cutoff time.Time, limit int) ([]int64, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT to_regclass('token_billing_reservations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,api_key_id FROM token_billing_reservations
		WHERE state IN ('reserved','dispatched') AND created_at<=$1 ORDER BY created_at,id LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	items, err := scanExpiredBalanceHolds(rows)
	if err != nil {
		return nil, err
	}
	users := []int64{}
	var errs []error
	for _, item := range items {
		applied, err := r.releaseExpiredLegacyTokenHold(ctx, item, cutoff)
		if err != nil {
			errs = append(errs, fmt.Errorf("expired legacy token hold %s: %w", item.id, err))
		} else if applied {
			users = append(users, item.userID)
		}
	}
	return users, errors.Join(errs...)
}

type legacyTokenFinalQuote struct {
	Command service.UsageBillingCommand `json:"command"`
	Usage   service.UsageLog            `json:"usage"`
	Maximum bool                        `json:"maximum,omitempty"`
}

func (r *usageBillingRepository) releaseExpiredLegacyTokenHold(ctx context.Context, item expiredBalanceHold, cutoff time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Match the old token service's user-before-reservation lock order, so an
	// instance still shutting down cannot race a refund with final settlement.
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, item.userID).Scan(&owner); err != nil {
		return false, err
	}
	var hold float64
	var final []byte
	err = tx.QueryRowContext(ctx, `SELECT hold_amount,final_quote FROM token_billing_reservations
		WHERE id=$1 AND user_id=$2 AND api_key_id=$3 AND state IN ('reserved','dispatched') AND created_at<=$4 FOR UPDATE`, item.id, item.userID, item.keyID, cutoff).Scan(&hold, &final)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if hold < 0 || math.IsNaN(hold) || math.IsInf(hold, 0) {
		return false, errors.New("invalid legacy frozen amount")
	}
	quote, err := parseLegacyTokenFinalQuote(final, item)
	if err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$1,frozen_balance=COALESCE(frozen_balance,0)-$1,updated_at=NOW()
		WHERE id=$2 AND COALESCE(frozen_balance,0)>=$1`, hold, item.userID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, err
	} else if n != 1 {
		return false, errors.New("legacy token frozen balance invariant violated")
	}
	if quote != nil {
		// Only observed usage may consume the old hold. Never turn a missing
		// outcome or a conservative maximum estimate into a new user charge.
		claimed, err := r.claimUsageBillingKey(ctx, tx, &quote.Command)
		if err != nil {
			return false, err
		}
		if !claimed {
			return false, service.ErrUsageBillingRequestConflict
		}
		if err := r.applyUsageBillingEffects(ctx, tx, &quote.Command, &service.UsageBillingApplyResult{}); err != nil {
			return false, err
		}
		quote.Usage.RequestID, quote.Usage.UserID, quote.Usage.APIKeyID = item.id, item.userID, item.keyID
		if err := writeMediaUsageLog(ctx, tx, &quote.Usage); err != nil {
			return false, err
		}
		data, err := json.Marshal(quote)
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='settled',settlement_kind='actual',settled_quote=$2,outstanding_amount=0,updated_at=NOW() WHERE id=$1`, item.id, data); err != nil {
			return false, err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='released',updated_at=NOW() WHERE id=$1`, item.id); err != nil {
		return false, err
	}
	if err := recordExpiredBalanceHold(ctx, tx, "legacy_token", item, hold); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func parseLegacyTokenFinalQuote(data []byte, item expiredBalanceHold) (*legacyTokenFinalQuote, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var quote legacyTokenFinalQuote
	if err := json.Unmarshal(data, &quote); err != nil {
		return nil, err
	}
	if quote.Maximum {
		return nil, nil
	}
	cmd := &quote.Command
	if cmd.RequestID != item.id || cmd.UserID != item.userID || cmd.APIKeyID != item.keyID {
		return nil, service.ErrUsageBillingRequestConflict
	}
	for _, amount := range []float64{cmd.BalanceCost, cmd.SubscriptionCost, cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost, cmd.AccountQuotaCost, cmd.UserPlatformQuotaCost} {
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return nil, errors.New("invalid legacy token final charge")
		}
	}
	cmd.Normalize()
	return &quote, nil
}
