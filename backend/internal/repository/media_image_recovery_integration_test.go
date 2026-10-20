//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImagePrepaymentRecoveryIncludesDispatchedTasks(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@image-recovery.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "image recovery", Status: service.StatusAPIKeyActive})
	account := mustCreateAccount(t, client, &service.Account{Name: "image recovery " + uuid.NewString(), Type: service.AccountTypeAPIKey})
	id := "media:" + uuid.NewString()
	r := &service.MediaBillingReservation{ID: id, WorkbenchKind: "image", Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .5, APIKeyQuotaCost: .5, ChargedAt: time.Now()}}}
	require.NoError(t, repo.ReserveMedia(ctx, r))
	_, err := integrationDB.Exec(`UPDATE media_billing_reservations SET quote=jsonb_set(quote,'{image_task_id}',to_jsonb($2::text)),dispatch_started_at=NOW(),queue_lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, id, "imgtask_recovery")
	require.NoError(t, err)
	items, err := repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	found := false
	for _, item := range items {
		if item.ID == id {
			found = true
		}
	}
	require.True(t, found, "an image dispatched before a restart must be recoverable")
}

func TestImagePrepaymentDurableOutcomeSettlementAndRefund(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	images := repo.(service.MediaImageBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@image-outcome.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "image outcome", Status: service.StatusAPIKeyActive})
	account := mustCreateAccount(t, client, &service.Account{Name: "image outcome " + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newPayment := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		r := &service.MediaBillingReservation{ID: id, WorkbenchKind: "image", Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .5, APIKeyQuotaCost: .5, ChargedAt: time.Now()}}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		taskID := "imgtask_" + uuid.NewString()
		require.ErrorIs(t, images.BindMediaImageTask(ctx, id, taskID, user.ID+1, key.ID), service.ErrUsageBillingRequestConflict)
		require.NoError(t, images.BindMediaImageTask(ctx, id, taskID, user.ID, key.ID))
		require.NoError(t, repo.DispatchMediaQueue(ctx, id, user.ID, key.ID, account.ID))
		require.ErrorIs(t, images.BindMediaImageTask(ctx, id, taskID+"other", user.ID, key.ID), service.ErrUsageBillingRequestConflict)
		return r
	}
	delivered, failed := newPayment(), newPayment()
	for _, r := range []*service.MediaBillingReservation{delivered, failed} {
		actual := r.Quote
		actual.Command.BalanceCost, actual.Command.APIKeyQuotaCost, actual.Command.RequestFingerprint = .2, .2, ""
		require.NoError(t, images.StageMediaImageOutcome(ctx, r.ID, &actual))
		require.NoError(t, images.StageMediaImageOutcome(ctx, r.ID, &actual), "identical retries preserve the candidate")
		changed := actual
		changed.Command.BalanceCost = .9
		require.ErrorIs(t, images.StageMediaImageOutcome(ctx, r.ID, &changed), service.ErrUsageBillingRequestConflict)
	}
	items, err := repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	var recovered *service.MediaBillingReservation
	for _, r := range items {
		if r.ID == delivered.ID {
			recovered = r
		}
	}
	require.NotNil(t, recovered)
	require.NotEmpty(t, recovered.ImageTaskID)
	require.NotNil(t, recovered.ImageOutcome)
	require.InDelta(t, .2, recovered.ImageOutcome.Command.BalanceCost, 1e-8)
	// The provider's successful result is still refundable if upload fails.
	for range 3 {
		_, err = repo.RefundMedia(ctx, failed.ID)
		require.NoError(t, err)
	}
	require.NoError(t, repo.StageMediaOutcome(ctx, delivered.ID, recovered.ImageOutcome))
	_, err = repo.RefundMedia(ctx, delivered.ID)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	for i := range 3 {
		result, err := repo.SettleMedia(ctx, delivered.ID, &recovered.ImageOutcome.Command)
		require.NoError(t, err)
		require.Equal(t, i == 0, result.Applied)
	}
	var balance, frozen, quota float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, key.ID).Scan(&quota))
	require.InDelta(t, 9.8, balance, 1e-8)
	require.Zero(t, frozen, "ordinary workbench prepayment is not a batch-image freeze")
	require.InDelta(t, .2, quota, 1e-8)
	t.Logf("balance=%0.8f frozen=%0.8f quota=%0.8f; one actual charge, one full refund", balance, frozen, quota)
}
