//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBatchImageConcurrentCaptureRefundPreservesOtherHolds(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@batch.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "batch", Status: service.StatusAPIKeyActive})
	batchID, otherID := uuid.NewString(), uuid.NewString()
	for id, hold := range map[string]float64{batchID: 1, otherID: 2} {
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO batch_image_jobs
			(batch_id,user_id,api_key_id,provider,model,item_count,hold_amount)
			VALUES ($1,$2,$3,'gemini','test-image',1,$4)`, id, user.ID, key.ID, hold)
		require.NoError(t, err)
	}
	command := func(id, requestID string, hold, actual float64) *service.BatchImageBalanceHoldCommand {
		return &service.BatchImageBalanceHoldCommand{RequestID: requestID, UserID: user.ID, APIKeyID: key.ID, BatchID: id, HoldAmount: hold, ActualAmount: actual}
	}
	_, err := repo.ReserveBatchImageBalance(ctx, command(batchID, service.BatchImageHoldRequestID(batchID), 1, 0))
	require.NoError(t, err)
	_, err = repo.ReserveBatchImageBalance(ctx, command(otherID, service.BatchImageHoldRequestID(otherID), 2, 0))
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, err := repo.CaptureBatchImageBalance(ctx, command(batchID, service.BatchImageCaptureRequestID(batchID), 1, .25))
				results <- err
			} else {
				_, err := repo.ReleaseBatchImageBalance(ctx, command(batchID, service.BatchImageReleaseRequestID(batchID), 1, 0))
				results <- err
			}
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.True(t, err == nil || errors.Is(err, service.ErrUsageBillingRequestConflict), "unexpected billing result: %v", err)
	}
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 2, frozen, 1e-8, "the other batch hold must remain untouched")
	require.True(t, balance == 7.75 || balance == 8, "capture or refund may win, but never both: balance=%v", balance)
}

func TestBatchImageHoldScopeAndPostpaidBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@scope.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "scope", Status: service.StatusAPIKeyActive})
	batchID := uuid.NewString()
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO batch_image_jobs
		(batch_id,user_id,api_key_id,provider,model,item_count,hold_amount)
		VALUES ($1,$2,$3,'gemini','test-image',1,1.1234567890)`, batchID, user.ID, key.ID)
	require.NoError(t, err)
	command := func() *service.BatchImageBalanceHoldCommand {
		return &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(batchID), UserID: user.ID, APIKeyID: key.ID, BatchID: batchID, HoldAmount: 1.1234567890}
	}
	for _, kind := range []string{"foreign-request", "missing-job", "wrong-user", "wrong-key", "wrong-amount"} {
		t.Run(kind, func(t *testing.T) {
			cmd := command()
			switch kind {
			case "foreign-request":
				cmd.RequestID = "chat:" + uuid.NewString()
			case "missing-job":
				cmd.BatchID = uuid.NewString()
				cmd.RequestID = service.BatchImageHoldRequestID(cmd.BatchID)
			case "wrong-user":
				cmd.UserID++
			case "wrong-key":
				cmd.APIKeyID++
			case "wrong-amount":
				cmd.HoldAmount = 2
			}
			_, err := repo.ReserveBatchImageBalance(ctx, cmd)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		})
	}
	assertMoney := func(balance, frozen float64) {
		t.Helper()
		var actualBalance, actualFrozen float64
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance,frozen_balance FROM users WHERE id=$1", user.ID).Scan(&actualBalance, &actualFrozen))
		require.InDelta(t, balance, actualBalance, 1e-9)
		require.InDelta(t, frozen, actualFrozen, 1e-9)
	}
	assertMoney(10, 0)
	for _, protocol := range []string{"openai", "anthropic"} {
		cmd := &service.UsageBillingCommand{RequestID: protocol + ":" + uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, BalanceCost: .25}
		first, err := repo.Apply(ctx, cmd)
		require.NoError(t, err)
		require.True(t, first.Applied)
		retry, err := repo.Apply(ctx, cmd)
		require.NoError(t, err)
		require.False(t, retry.Applied)
	}
	assertMoney(9.5, 0)
	first, err := repo.ReserveBatchImageBalance(ctx, command())
	require.NoError(t, err, "ten-decimal pricing snapshot must match the eight-decimal money ledger")
	require.True(t, first.Applied)
	second, err := repo.ReserveBatchImageBalance(ctx, command())
	require.NoError(t, err)
	require.False(t, second.Applied)
	assertMoney(8.37654321, 1.12345679)
	_, err = repo.ReleaseBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(batchID), UserID: user.ID, APIKeyID: key.ID, BatchID: batchID, HoldAmount: command().HoldAmount})
	require.NoError(t, err)
	assertMoney(9.5, 0)
}
