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
