//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestExpiredBatchHoldLateCapturePreservesOtherHolds(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := &usageBillingRepository{db: integrationDB}
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@expiry.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "expiry", Status: service.StatusAPIKeyActive})
	batchID, otherID := uuid.NewString(), uuid.NewString()
	for id, hold := range map[string]float64{batchID: 1, otherID: 2} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO batch_image_jobs
			(batch_id,user_id,api_key_id,provider,model,item_count,hold_amount)
			VALUES ($1,$2,$3,'gemini','test-image',1,$4)`, id, user.ID, key.ID, hold)
		require.NoError(t, err)
		_, err = repo.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(id), UserID: user.ID, APIKeyID: key.ID, BatchID: id, HoldAmount: hold})
		require.NoError(t, err)
	}
	assertMoney := func(balance, frozen float64) {
		t.Helper()
		var actualBalance, actualFrozen float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", user.ID).Scan(&actualBalance, &actualFrozen))
		require.InDelta(t, balance, actualBalance, 1e-9)
		require.InDelta(t, frozen, actualFrozen, 1e-9)
	}
	assertMoney(7, 3)
	item := expiredBalanceHold{id: batchID, userID: user.ID, keyID: key.ID}
	cutoff := time.Now().Add(time.Minute)
	for _, expected := range []bool{true, false} {
		applied, err := repo.releaseExpiredBatchHold(ctx, item, cutoff)
		require.NoError(t, err)
		require.Equal(t, expected, applied)
	}
	assertMoney(8, 2)
	for _, expected := range []bool{true, false} {
		result, err := repo.CaptureBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageCaptureRequestID(batchID), UserID: user.ID, APIKeyID: key.ID, BatchID: batchID, HoldAmount: 1, ActualAmount: .25})
		require.NoError(t, err)
		require.Equal(t, expected, result.Applied)
	}
	assertMoney(7.75, 2)
	result, err := repo.ReleaseBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(batchID), UserID: user.ID, APIKeyID: key.ID, BatchID: batchID, HoldAmount: 1})
	require.NoError(t, err)
	require.False(t, result.Applied, "a refund after successful capture must be a no-op")
	assertMoney(7.75, 2)
}

func TestLegacyAlreadyBilledRecoveryPostgres(t *testing.T) {
	ctx := context.Background()
	// Current migrations intentionally do not create the removed token-hold
	// table. Reproduce just its recovery fields in the disposable test DB.
	var exists bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT to_regclass('token_billing_reservations') IS NOT NULL").Scan(&exists))
	if exists {
		t.Skip("requires a test database without the optional legacy table")
	}
	_, err := integrationDB.ExecContext(ctx, `CREATE TABLE token_billing_reservations (
		id TEXT PRIMARY KEY,user_id BIGINT NOT NULL,api_key_id BIGINT NOT NULL,
		hold_amount NUMERIC(20,8) NOT NULL,final_quote JSONB,state TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		settlement_kind TEXT,settled_quote JSONB,outstanding_amount NUMERIC(20,8) NOT NULL DEFAULT 0)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DROP TABLE token_billing_reservations")
		require.NoError(t, err)
	})
	client := testEntClient(t)
	repo := &usageBillingRepository{db: integrationDB}
	for _, location := range []string{"live", "archive", "conflict"} {
		t.Run(location, func(t *testing.T) {
			user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@legacy.test", PasswordHash: "hash", Balance: 8.75, Status: service.StatusActive})
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "legacy", Status: service.StatusAPIKeyActive})
			item := expiredBalanceHold{id: "token:" + uuid.NewString(), userID: user.ID, keyID: key.ID}
			quote := legacyTokenFinalQuote{Command: service.UsageBillingCommand{RequestID: item.id, UserID: user.ID, APIKeyID: key.ID, BalanceCost: .25}}
			quote.Command.Normalize()
			data, err := json.Marshal(quote)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE users SET frozen_balance=1 WHERE id=$1", user.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `INSERT INTO token_billing_reservations(id,user_id,api_key_id,hold_amount,final_quote,state) VALUES($1,$2,$3,1,$4,'dispatched')`, item.id, user.ID, key.ID, data)
			require.NoError(t, err)
			table := "usage_billing_dedup"
			if location == "archive" {
				table += "_archive"
			}
			fingerprint := quote.Command.RequestFingerprint
			if location == "conflict" {
				fingerprint = "different-charge"
			}
			_, err = integrationDB.ExecContext(ctx, "INSERT INTO "+table+"(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,$3,NOW())", item.id, key.ID, fingerprint)
			require.NoError(t, err)
			cutoff := time.Now().Add(time.Minute)
			applied, err := repo.releaseExpiredLegacyTokenHold(ctx, item, cutoff)
			var balance, frozen float64
			var state string
			if location == "conflict" {
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.False(t, applied)
			} else {
				require.NoError(t, err)
				require.True(t, applied)
				again, err := repo.releaseExpiredLegacyTokenHold(ctx, item, cutoff)
				require.NoError(t, err)
				require.False(t, again)
			}
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", user.ID).Scan(&balance, &frozen))
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT state FROM token_billing_reservations WHERE id=$1", item.id).Scan(&state))
			if location == "conflict" {
				require.InDelta(t, 8.75, balance, 1e-9)
				require.InDelta(t, 1, frozen, 1e-9)
				require.Equal(t, "dispatched", state)
			} else {
				require.InDelta(t, 9.75, balance, 1e-9)
				require.Zero(t, frozen)
				require.Equal(t, "settled", state)
			}
			var receipts, logs int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2 UNION ALL SELECT 1 FROM usage_billing_dedup_archive WHERE request_id=$1 AND api_key_id=$2) d`, item.id, key.ID).Scan(&receipts))
			require.Equal(t, 1, receipts, "archived charges must not leave a second active receipt")
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2", item.id, key.ID).Scan(&logs))
			require.Zero(t, logs, "recovery must not write a second usage log")
		})
	}
}
