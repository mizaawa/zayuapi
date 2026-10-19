package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type tokenBillingRow struct {
	reservation    service.TokenBillingReservation
	state, kind    string
	final, settled *service.TokenBillingQuote
	outstanding    float64
}

// Every money operation locks users before reservation rows. This also orders
// concurrent reservations, settlement, debt collection and balance adjustments
// across different requests owned by the same user.
func lockTokenBillingUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	return err
}

func readTokenBillingRow(ctx context.Context, tx *sql.Tx, id string) (*tokenBillingRow, error) {
	var owner int64
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM token_billing_reservations WHERE id=$1`, id).Scan(&owner); err != nil {
		return nil, err
	}
	if err := lockTokenBillingUser(ctx, tx, owner); err != nil {
		return nil, err
	}
	row := &tokenBillingRow{}
	var quote, final, settled []byte
	err := tx.QueryRowContext(ctx, `SELECT user_id,api_key_id,group_id,quote,state,final_quote,settled_quote,settlement_kind,outstanding_amount,lease_until
		FROM token_billing_reservations WHERE id=$1 FOR UPDATE`, id).Scan(&row.reservation.UserID, &row.reservation.APIKeyID, &row.reservation.GroupID, &quote, &row.state, &final, &settled, &row.kind, &row.outstanding, &row.reservation.LeaseUntil)
	if err != nil {
		return nil, err
	}
	row.reservation.ID = id
	if err := json.Unmarshal(quote, &row.reservation.Quote); err != nil {
		return nil, err
	}
	if len(final) > 0 {
		if err := json.Unmarshal(final, &row.final); err != nil {
			return nil, err
		}
	}
	if len(settled) > 0 {
		if err := json.Unmarshal(settled, &row.settled); err != nil {
			return nil, err
		}
	}
	return row, nil
}

func (r *usageBillingRepository) ReserveToken(ctx context.Context, reservation *service.TokenBillingReservation) error {
	if r == nil || r.db == nil {
		return service.ErrBillingServiceUnavailable
	}
	if reservation == nil || !strings.HasPrefix(reservation.ID, "token:") || reservation.UserID <= 0 || reservation.APIKeyID <= 0 ||
		reservation.Quote.Command.UserID != reservation.UserID || reservation.Quote.Command.APIKeyID != reservation.APIKeyID ||
		reservation.Quote.Command.RequestID != reservation.ID {
		return service.ErrUsageBillingRequestConflict
	}
	quote := &reservation.Quote
	if err := service.ValidateTokenBillingQuote(quote); err != nil {
		return err
	}
	service.NormalizeTokenBillingQuote(quote)
	if reservation.LeaseUntil.IsZero() || reservation.LeaseUntil.After(time.Now().Add(service.TokenBillingReservationLease)) {
		reservation.LeaseUntil = time.Now().Add(service.TokenBillingReservationLease)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockTokenBillingUser(ctx, tx, reservation.UserID); err != nil {
		return err
	}
	var existing []byte
	err = tx.QueryRowContext(ctx, `SELECT quote FROM token_billing_reservations WHERE id=$1`, reservation.ID).Scan(&existing)
	if err == nil {
		var saved service.TokenBillingQuote
		if err := json.Unmarshal(existing, &saved); err != nil {
			return err
		}
		if saved.Command.RequestFingerprint != quote.Command.RequestFingerprint {
			return service.ErrUsageBillingRequestConflict
		}
		// The financial operation is idempotent; dispatch is separately fenced
		// and never permits the same reservation to forward twice.
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// An over-maximum charge remains payable, never silently truncated. Collect
	// it from a recharge before authorizing any new upstream work.
	if err := collectTokenOutstanding(ctx, tx, reservation.UserID); err != nil {
		return err
	}
	var owes bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM token_billing_reservations WHERE user_id=$1 AND outstanding_amount>0)`, reservation.UserID).Scan(&owes); err != nil {
		return err
	}
	if owes {
		// Debt collection must survive a declined reservation.
		if err := tx.Commit(); err != nil {
			return err
		}
		return service.ErrInsufficientBalance
	}
	var keyOwner, group, failoverGroup int64
	var failoverEnabled bool
	err = tx.QueryRowContext(ctx, `SELECT user_id,COALESCE(group_id,0),failover_enabled,COALESCE(failover_group_id,0)
		FROM api_keys WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, reservation.APIKeyID).Scan(&keyOwner, &group, &failoverEnabled, &failoverGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAPIKeyNotFound
	}
	if err != nil {
		return err
	}
	if keyOwner != reservation.UserID {
		return service.ErrUsageBillingRequestConflict
	}
	if group != reservation.GroupID {
		// Effective request keys retain their persisted primary group. Only a
		// currently configured route may bill another group: key failover can
		// start before cooldown is recorded, or resume while it is active.
		// The separate invalid-request fallback also clones the effective key.
		var allowed bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups primary_group
			JOIN groups effective_group ON effective_group.id=$2
			WHERE primary_group.id=$1 AND primary_group.deleted_at IS NULL AND effective_group.deleted_at IS NULL
			AND primary_group.status='active' AND effective_group.status='active'
			AND (($3 AND $4=effective_group.id AND primary_group.platform NOT IN ('custom','composite')
				AND primary_group.platform=effective_group.platform)
			OR (primary_group.fallback_group_id_on_invalid_request=effective_group.id
				AND primary_group.platform IN ('anthropic','antigravity') AND primary_group.subscription_type<>'subscription'
				AND effective_group.platform='anthropic' AND effective_group.subscription_type<>'subscription'
				AND effective_group.fallback_group_id_on_invalid_request IS NULL)))`, group, reservation.GroupID, failoverEnabled, failoverGroup).Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return service.ErrUsageBillingRequestConflict
		}
	}
	var balance float64
	err = tx.QueryRowContext(ctx, `UPDATE users SET balance=balance-$1,frozen_balance=COALESCE(frozen_balance,0)+$1,updated_at=NOW()
		WHERE id=$2 AND deleted_at IS NULL AND balance >= $1 RETURNING balance`, quote.Command.BalanceCost, reservation.UserID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		// Keep any debt payment committed even if the new hold cannot fit.
		if err := tx.Commit(); err != nil {
			return err
		}
		return service.ErrInsufficientBalance
	}
	if err != nil {
		return err
	}
	data, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO token_billing_reservations(id,user_id,api_key_id,group_id,hold_amount,quote,lease_until)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, reservation.ID, reservation.UserID, reservation.APIKeyID, reservation.GroupID, quote.Command.BalanceCost, data, reservation.LeaseUntil)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *usageBillingRepository) DispatchToken(ctx context.Context, id string, userID, keyID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := readTokenBillingRow(ctx, tx, id)
	if err != nil {
		return err
	}
	if row.reservation.UserID != userID || row.reservation.APIKeyID != keyID || row.state != "reserved" {
		return service.ErrUsageBillingRequestConflict
	}
	var live bool
	if err := tx.QueryRowContext(ctx, `SELECT lease_until>NOW() FROM token_billing_reservations WHERE id=$1`, id).Scan(&live); err != nil {
		return err
	}
	if !live {
		return service.ErrUsageBillingRequestConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='dispatched',dispatched_at=NOW(),lease_until=NOW()+INTERVAL '6 hours',updated_at=NOW() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Stage commits the exact usage before attempting its financial transaction.
// The recovery worker can complete it after cancellation, process exit or a
// transient database error. Repeating a different outcome is always a conflict.
func (r *usageBillingRepository) StageTokenOutcome(ctx context.Context, id string, quote *service.TokenBillingQuote) error {
	if err := service.ValidateTokenBillingQuote(quote); err != nil {
		return err
	}
	service.NormalizeTokenBillingQuote(quote)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := readTokenBillingRow(ctx, tx, id)
	if err != nil {
		return err
	}
	if quote.Command.RequestID != id || quote.Command.UserID != row.reservation.UserID || quote.Command.APIKeyID != row.reservation.APIKeyID ||
		quote.Command.RequestPayloadHash != row.reservation.Quote.Command.RequestPayloadHash || row.state == "released" || row.state == "reserved" {
		return service.ErrUsageBillingRequestConflict
	}
	if row.final != nil {
		if row.final.Command.RequestFingerprint == quote.Command.RequestFingerprint && row.final.Maximum == quote.Maximum {
			return nil
		}
		if !row.final.Maximum || quote.Maximum {
			return service.ErrUsageBillingRequestConflict
		}
	}
	// A late exact outcome may correct a conservative crash-time maximum.
	if row.state == "settled" && row.kind != "maximum" {
		return service.ErrUsageBillingRequestConflict
	}
	data, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE token_billing_reservations SET final_quote=$2,updated_at=NOW() WHERE id=$1`, id, data); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *usageBillingRepository) SettleToken(ctx context.Context, id string) (*service.UsageBillingApplyResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := readTokenBillingRow(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result := &service.UsageBillingApplyResult{BalanceReserved: true, OutstandingBalanceCost: row.outstanding}
	if row.state == "settled" && (row.kind == "actual" || row.final == nil || row.final.Maximum) {
		return result, nil
	}
	if row.state != "dispatched" && (row.state != "settled" || row.kind != "maximum" || row.final == nil) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	quote := row.final
	kind := "actual"
	if quote != nil && quote.Maximum {
		kind = "maximum"
	}
	if quote == nil {
		var expired bool
		if err := tx.QueryRowContext(ctx, `SELECT lease_until<=NOW() FROM token_billing_reservations WHERE id=$1`, id).Scan(&expired); err != nil {
			return nil, err
		}
		if !expired {
			return nil, service.ErrTokenBillingOutcomePending
		}
		maximum := service.TokenBillingMaximumQuote(row.reservation.Quote)
		quote = &maximum
		kind = "maximum"
	}
	command := quote.Command
	command.Normalize()
	hold := row.reservation.Quote.Command.BalanceCost
	if row.state == "settled" {
		if row.settled == nil {
			return nil, errors.New("token settlement receipt is missing")
		}
		original := row.settled.Command
		// Undo only money actually paid. The outstanding portion was an explicit
		// receivable, not a negative user balance and not a refundable payment.
		original.BalanceCost = service.QuantizeUsageBillingAmount(original.BalanceCost - row.outstanding)
		if err := r.reverseMediaEffects(ctx, tx, &original); err != nil {
			return nil, err
		}
		hold = 0
		// The reservation row, retained independently of dedup retention, proves
		// ownership. Replace only our conservative fingerprint for late usage.
		for _, table := range []string{"usage_billing_dedup", "usage_billing_dedup_archive"} {
			if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET request_fingerprint=$3 WHERE request_id=$1 AND api_key_id=$2", id, command.APIKeyID, command.RequestFingerprint); err != nil {
				return nil, err
			}
		}
	} else {
		claimed, err := r.claimUsageBillingKey(ctx, tx, &command)
		if err != nil {
			return nil, err
		}
		if !claimed {
			return nil, service.ErrUsageBillingRequestConflict
		}
	}
	// SQL NUMERIC computes the difference at the same scale as balance. Any
	// unexpected overage is a durable receivable rather than a dropped fee or
	// an overdraft. Other reservations' frozen funds are never spent.
	var balance, outstanding float64
	err = tx.QueryRowContext(ctx, `WITH funds AS (SELECT balance FROM users WHERE id=$3 FOR UPDATE)
		UPDATE users u SET balance=GREATEST(funds.balance+$1-$2,0),frozen_balance=COALESCE(u.frozen_balance,0)-$1,updated_at=NOW()
		FROM funds WHERE u.id=$3 AND COALESCE(u.frozen_balance,0)>=$1
		RETURNING u.balance,GREATEST($2-(funds.balance+$1),0)`, hold, command.BalanceCost, command.UserID).Scan(&balance, &outstanding)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("token reservation frozen balance invariant violated")
	}
	if err != nil {
		return nil, err
	}
	effects := command
	effects.BalanceCost = 0
	if err := r.applyUsageBillingEffects(ctx, tx, &effects, result); err != nil {
		return nil, err
	}
	result.Applied, result.NewBalance, result.OutstandingBalanceCost = true, &balance, outstanding
	usage := quote.Usage
	usage.RequestID, usage.UserID, usage.APIKeyID = id, command.UserID, command.APIKeyID
	if err := writeMediaUsageLog(ctx, tx, &usage); err != nil {
		return nil, err
	}
	data, err := json.Marshal(quote)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='settled',settlement_kind=$2,settled_quote=$3,outstanding_amount=$4,updated_at=NOW() WHERE id=$1`, id, kind, data, outstanding)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *usageBillingRepository) ReleaseToken(ctx context.Context, id string, onlyUndispatched bool) (*service.TokenBillingReservation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := readTokenBillingRow(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if row.state == "released" || row.state == "settled" || (onlyUndispatched && row.state != "reserved") {
		return nil, nil
	}
	if row.final != nil {
		return nil, service.ErrUsageBillingRequestConflict
	}
	hold := row.reservation.Quote.Command.BalanceCost
	res, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$1,frozen_balance=COALESCE(frozen_balance,0)-$1,updated_at=NOW()
		WHERE id=$2 AND COALESCE(frozen_balance,0)>=$1`, hold, row.reservation.UserID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, errors.New("token refund frozen balance invariant violated")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='released',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &row.reservation, nil
}

// User lock is held by the caller. Recharges pay old receivables before any new
// request is admitted, and the background worker also collects them while idle.
func collectTokenOutstanding(ctx context.Context, tx *sql.Tx, userID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM token_billing_reservations WHERE user_id=$1 AND outstanding_amount>0 ORDER BY created_at,id FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		_, err := tx.ExecContext(ctx, `WITH payment AS (SELECT LEAST(GREATEST(u.balance,0),r.outstanding_amount) AS amount FROM users u JOIN token_billing_reservations r ON r.user_id=u.id WHERE u.id=$1 AND r.id=$2),
		paid AS (UPDATE users u SET balance=u.balance-payment.amount,updated_at=NOW() FROM payment WHERE u.id=$1 RETURNING payment.amount)
		UPDATE token_billing_reservations r SET outstanding_amount=r.outstanding_amount-paid.amount,updated_at=NOW() FROM paid WHERE r.id=$2`, userID, id)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *usageBillingRepository) RecoverToken(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,state FROM token_billing_reservations
		WHERE (state IN ('reserved','dispatched') AND (lease_until<=NOW() OR final_quote IS NOT NULL))
		OR (state='settled' AND settlement_kind='maximum' AND final_quote IS NOT NULL AND NOT COALESCE((final_quote->>'maximum')::boolean,FALSE))
		OR outstanding_amount>0 ORDER BY updated_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	type item struct {
		id    string
		user  int64
		state string
	}
	items := []item{}
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.id, &i.user, &i.state); err != nil {
			_ = rows.Close()
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	users := []int64{}
	var firstErr error
	for _, i := range items {
		var err error
		switch i.state {
		case "reserved":
			// A dispatch racing this cleanup rechecks state under the same user
			// and reservation locks, so a stale sweeper cannot grant a refund.
			_, err = r.releaseExpiredToken(ctx, i.id)
		case "dispatched":
			_, err = r.SettleToken(ctx, i.id)
		case "settled":
			_, err = r.SettleToken(ctx, i.id)
			if err == nil {
				var tx *sql.Tx
				tx, err = r.db.BeginTx(ctx, nil)
				if err == nil {
					err = lockTokenBillingUser(ctx, tx, i.user)
					if err == nil {
						err = collectTokenOutstanding(ctx, tx, i.user)
					}
					if err == nil {
						err = tx.Commit()
					}
					_ = tx.Rollback()
				}
			}
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("token reservation %s: %w", i.id, err)
			}
		} else {
			users = append(users, i.user)
		}
	}
	return users, firstErr
}

func (r *usageBillingRepository) releaseExpiredToken(ctx context.Context, id string) (*service.TokenBillingReservation, error) {
	// ReleaseToken itself serializes the state transition. The expiration must
	// also be checked there, not just in the preceding recovery SELECT.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := readTokenBillingRow(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var expired bool
	if err := tx.QueryRowContext(ctx, `SELECT lease_until<=NOW() FROM token_billing_reservations WHERE id=$1`, id).Scan(&expired); err != nil {
		return nil, err
	}
	if row.state != "reserved" || !expired {
		return nil, nil
	}
	hold := row.reservation.Quote.Command.BalanceCost
	res, err := tx.ExecContext(ctx, `UPDATE users SET balance=balance+$1,frozen_balance=COALESCE(frozen_balance,0)-$1,updated_at=NOW() WHERE id=$2 AND COALESCE(frozen_balance,0)>=$1`, hold, row.reservation.UserID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, errors.New("token recovery frozen balance invariant violated")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE token_billing_reservations SET state='released',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &row.reservation, nil
}
