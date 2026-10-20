package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectBatchHoldClaim(mock sqlmock.Sqlmock, requestID string, exists bool) {
	query := mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup\s+WHERE request_id = \$1 AND api_key_id = \$2`).WithArgs(requestID, int64(9))
	if exists {
		query.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
		return
	}
	query.WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup_archive\s+WHERE request_id = \$1 AND api_key_id = \$2`).WithArgs(requestID, int64(9)).WillReturnError(sql.ErrNoRows)
}

func TestExpiredBatchHoldTerminalOperation(t *testing.T) {
	for _, operation := range []string{"capture", "release"} {
		t.Run(operation, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			cmd := &service.BatchImageBalanceHoldCommand{BatchID: "batch", UserID: 7, APIKeyID: 9, HoldAmount: 1}
			cmd.RequestID = service.BatchImageReleaseRequestID("batch")
			if operation == "capture" {
				cmd.RequestID = service.BatchImageCaptureRequestID("batch")
				cmd.ActualAmount = .25
			}
			cmd.Normalize()
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(advisoryLockHash("batch_image_billing:batch")).WillReturnResult(sqlmock.NewResult(0, 1))
			expectBatchHoldClaim(mock, service.BatchImageCaptureRequestID("batch"), false)
			expectBatchHoldClaim(mock, service.BatchImageReleaseRequestID("batch"), false)
			if operation == "capture" {
				expectBatchHoldClaim(mock, service.BatchImageHoldRequestID("batch"), true)
			}
			mock.ExpectQuery(`SELECT user_id,amount FROM balance_hold_releases`).WithArgs("batch", int64(9)).WillReturnRows(sqlmock.NewRows([]string{"user_id", "amount"}).AddRow(7, 1))
			mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup .*RETURNING id`).WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).WithArgs(cmd.RequestID, cmd.APIKeyID).WillReturnError(sql.ErrNoRows)
			if operation == "capture" {
				// Expired money is available again. A late successful output bills
				// only its actual cost without touching other frozen funds.
				mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND balance >= \$1\s+RETURNING balance`).WithArgs(.25, int64(7)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(7.75))
			}
			mock.ExpectCommit()
			repo := &usageBillingRepository{db: db}
			var result *service.BatchImageBalanceHoldResult
			if operation == "capture" {
				result, err = repo.CaptureBatchImageBalance(context.Background(), cmd)
			} else {
				result, err = repo.ReleaseBatchImageBalance(context.Background(), cmd)
			}
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Nil(t, result.FrozenBalance)
			if operation == "capture" {
				require.NotNil(t, result.NewBalance)
				require.Equal(t, 7.75, *result.NewBalance)
			} else {
				require.Nil(t, result.NewBalance, "the timed refund must not be returned twice")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReleaseExpiredBatchHoldIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	item := expiredBalanceHold{id: "batch", userID: 7, keyID: 9}
	cutoff := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	for _, alreadyReleased := range []bool{false, true} {
		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(advisoryLockHash("batch_image_billing:batch")).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`(?s)SELECT COALESCE\(hold_amount,estimated_cost\),COALESCE\(request_hash,''\) FROM batch_image_jobs`).WithArgs(item.id, item.userID, item.keyID, service.BatchImageHoldRequestID(item.id), cutoff).WillReturnRows(sqlmock.NewRows([]string{"hold", "hash"}).AddRow(1, "payload"))
		expectBatchHoldClaim(mock, service.BatchImageCaptureRequestID(item.id), false)
		expectBatchHoldClaim(mock, service.BatchImageReleaseRequestID(item.id), false)
		marker := mock.ExpectQuery(`SELECT 1 FROM balance_hold_releases`).WithArgs(item.id, item.keyID)
		if alreadyReleased {
			marker.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
			mock.ExpectRollback()
		} else {
			marker.WillReturnError(sql.ErrNoRows)
			expectBatchHoldClaim(mock, service.BatchImageHoldRequestID(item.id), true)
			mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance \+ \$1,.*RETURNING balance, frozen_balance`).WithArgs(1.0, item.userID).WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen"}).AddRow(8, 2))
			mock.ExpectExec(`INSERT INTO balance_hold_releases`).WithArgs("batch_image", item.id, item.keyID, item.userID, 1.0).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		applied, err := (&usageBillingRepository{db: db}).releaseExpiredBatchHold(context.Background(), item, cutoff)
		require.NoError(t, err)
		require.Equal(t, !alreadyReleased, applied)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLegacyFinalQuoteOnlyUsesObservedUsage(t *testing.T) {
	item := expiredBalanceHold{id: "token:request", userID: 7, keyID: 9}
	quote := legacyTokenFinalQuote{Command: service.UsageBillingCommand{RequestID: item.id, UserID: item.userID, APIKeyID: item.keyID, BalanceCost: .25}}
	data, err := json.Marshal(quote)
	require.NoError(t, err)
	actual, err := parseLegacyTokenFinalQuote(data, item)
	require.NoError(t, err)
	require.NotNil(t, actual)
	require.Equal(t, .25, actual.Command.BalanceCost)
	quote.Maximum = true
	data, err = json.Marshal(quote)
	require.NoError(t, err)
	maximum, err := parseLegacyTokenFinalQuote(data, item)
	require.NoError(t, err)
	require.Nil(t, maximum)
	missing, err := parseLegacyTokenFinalQuote(nil, item)
	require.NoError(t, err)
	require.Nil(t, missing)
	quote.Maximum = false
	quote.Command.UserID++
	data, err = json.Marshal(quote)
	require.NoError(t, err)
	_, err = parseLegacyTokenFinalQuote(data, item)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestExpiredLegacyTokenHoldRefundAndActualSettlement(t *testing.T) {
	for _, hasActual := range []bool{false, true} {
		name := "refund-missing-outcome"
		if hasActual {
			name = "refund-and-charge-observed-usage"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			item := expiredBalanceHold{id: "token:request", userID: 7, keyID: 9}
			cutoff := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
			var final []byte
			quote := legacyTokenFinalQuote{Command: service.UsageBillingCommand{RequestID: item.id, UserID: item.userID, APIKeyID: item.keyID, BalanceCost: .25}}
			if hasActual {
				quote.Command.Normalize()
				final, err = json.Marshal(quote)
				require.NoError(t, err)
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM users WHERE id=\$1 FOR UPDATE`).WithArgs(item.userID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(item.userID))
			mock.ExpectQuery(`(?s)SELECT hold_amount,final_quote FROM token_billing_reservations`).WithArgs(item.id, item.userID, item.keyID, cutoff).WillReturnRows(sqlmock.NewRows([]string{"hold_amount", "final_quote"}).AddRow(1.0, final))
			mock.ExpectExec(`(?s)UPDATE users SET balance=balance\+\$1,frozen_balance=COALESCE\(frozen_balance,0\)-\$1`).WithArgs(1.0, item.userID).WillReturnResult(sqlmock.NewResult(0, 1))
			if hasActual {
				mock.ExpectExec(`SAVEPOINT legacy_token_usage_claim`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup .*RETURNING id`).WithArgs(item.id, item.keyID, quote.Command.RequestFingerprint).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).WithArgs(item.id, item.keyID).WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND balance >= \$1\s+RETURNING balance`).WithArgs(.25, item.userID).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(9.75))
				mock.ExpectExec(`RELEASE SAVEPOINT legacy_token_usage_claim`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`UPDATE token_billing_reservations SET state='settled'`).WithArgs(item.id, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			} else {
				mock.ExpectExec(`UPDATE token_billing_reservations SET state='released'`).WithArgs(item.id).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec(`INSERT INTO balance_hold_releases`).WithArgs("legacy_token", item.id, item.keyID, item.userID, 1.0).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
			applied, err := (&usageBillingRepository{db: db}).releaseExpiredLegacyTokenHold(context.Background(), item, cutoff)
			require.NoError(t, err)
			require.True(t, applied)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLegacyTokenRecoveryAlreadyBilledStillRefunds(t *testing.T) {
	for _, location := range []string{"live", "archive", "conflict"} {
		t.Run(location, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			item := expiredBalanceHold{id: "token:already-billed", userID: 7, keyID: 9}
			cutoff := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
			quote := legacyTokenFinalQuote{Command: service.UsageBillingCommand{RequestID: item.id, UserID: item.userID, APIKeyID: item.keyID, BalanceCost: .25}}
			quote.Command.Normalize()
			final, err := json.Marshal(quote)
			require.NoError(t, err)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM users WHERE id=\$1 FOR UPDATE`).WithArgs(item.userID).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(item.userID))
			mock.ExpectQuery(`(?s)SELECT hold_amount,final_quote FROM token_billing_reservations`).WithArgs(item.id, item.userID, item.keyID, cutoff).WillReturnRows(sqlmock.NewRows([]string{"hold_amount", "final_quote"}).AddRow(1.0, final))
			mock.ExpectExec(`(?s)UPDATE users SET balance=balance\+\$1,frozen_balance=COALESCE\(frozen_balance,0\)-\$1`).WithArgs(1.0, item.userID).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`SAVEPOINT legacy_token_usage_claim`).WillReturnResult(sqlmock.NewResult(0, 0))
			claim := mock.ExpectQuery(`(?s)INSERT INTO usage_billing_dedup .*RETURNING id`).WithArgs(item.id, item.keyID, quote.Command.RequestFingerprint)
			if location == "archive" {
				claim.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).WithArgs(item.id, item.keyID).WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(quote.Command.RequestFingerprint))
			} else {
				claim.WillReturnError(sql.ErrNoRows)
				fingerprint := quote.Command.RequestFingerprint
				if location == "conflict" {
					fingerprint = "different-charge"
				}
				mock.ExpectQuery(`(?s)SELECT request_fingerprint\s+FROM usage_billing_dedup\s+WHERE`).WithArgs(item.id, item.keyID).WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(fingerprint))
			}
			if location == "conflict" {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec(`ROLLBACK TO SAVEPOINT legacy_token_usage_claim`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`RELEASE SAVEPOINT legacy_token_usage_claim`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`UPDATE token_billing_reservations SET state='settled'`).WithArgs(item.id, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO balance_hold_releases`).WithArgs("legacy_token", item.id, item.keyID, item.userID, 1.0).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			applied, err := (&usageBillingRepository{db: db}).releaseExpiredLegacyTokenHold(context.Background(), item, cutoff)
			if location == "conflict" {
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.False(t, applied)
			} else {
				require.NoError(t, err)
				require.True(t, applied, "already billed usage must not block refunding the old hold")
			}
			require.NoError(t, mock.ExpectationsWereMet(), "matching prior usage must never be billed or logged again")
		})
	}
}
