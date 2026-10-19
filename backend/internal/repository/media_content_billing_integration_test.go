//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMediaContentDurableSettlementArbitratesRefund(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@video-delivery.test", PasswordHash: "hash", Balance: 1, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "delivery", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "video-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	charged := 0
	for range 25 {
		id := "media:" + uuid.NewString()
		r := &service.MediaBillingReservation{ID: id, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .01, APIKeyQuotaCost: .01, ChargedAt: time.Now()}, Usage: service.UsageLog{CreatedAt: time.Now()}}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		require.NoError(t, repo.BindMediaTask(ctx, id, "video-"+uuid.NewString(), service.GrokMediaEndpointVideosGenerations))
		var wg sync.WaitGroup
		var settleErr, refundErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			settleErr = repo.StageMediaOutcome(ctx, id, &r.Quote)
			if settleErr == nil {
				_, settleErr = repo.SettleMedia(ctx, id, &r.Quote.Command)
			}
		}()
		go func() {
			defer wg.Done()
			refundErr = repo.StageMediaOutcome(ctx, id, nil)
			if refundErr == nil {
				_, refundErr = repo.RefundMedia(ctx, id)
			}
		}()
		wg.Wait()
		require.True(t, settleErr == nil || errors.Is(settleErr, service.ErrUsageBillingRequestConflict), "settlement: %v", settleErr)
		require.True(t, refundErr == nil || errors.Is(refundErr, service.ErrUsageBillingRequestConflict), "refund: %v", refundErr)
		var state string
		require.NoError(t, integrationDB.QueryRow(`SELECT state FROM media_billing_reservations WHERE id=$1`, id).Scan(&state))
		if state == "settled" {
			charged++
			refunded, err := repo.RefundMedia(ctx, id)
			require.NoError(t, err)
			require.Nil(t, refunded)
		} else {
			require.Equal(t, "refunded", state)
			_, err := repo.SettleMedia(ctx, id, &r.Quote.Command)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		}
		var events int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM media_billing_events WHERE reservation_id=$1 AND stage IN ('settled','refunded')`, id).Scan(&events))
		require.Equal(t, 1, events, "only one terminal balance transition")
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 1-float64(charged)*.01, balance, 1e-8)
	t.Logf("terminal races=25 charged=%d refunded=%d balance=%.8f", charged, 25-charged, balance)
}

func TestMediaPendingRecoveryFindsOldUnboundReservationWithoutQuoteTimestamp(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@video-old.test", PasswordHash: "hash", Balance: 1, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "old", Status: service.StatusAPIKeyActive})
	account := mustCreateAccount(t, client, &service.Account{Name: "old-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	id := "media:" + uuid.NewString()
	r := &service.MediaBillingReservation{ID: id, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .1, ChargedAt: time.Now()}}}
	require.NoError(t, repo.ReserveMedia(ctx, r))
	_, err := integrationDB.Exec(`UPDATE media_billing_reservations SET created_at=NOW()-INTERVAL '48 hours',next_check_at=NOW() WHERE id=$1`, id)
	require.NoError(t, err)
	pending, err := repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	var recovered *service.MediaBillingReservation
	for _, p := range pending {
		if p.ID == id {
			recovered = p
			break
		}
	}
	require.NotNil(t, recovered, "lost task bindings must still enter bounded recovery")
	require.Empty(t, recovered.TaskID)
	require.True(t, recovered.Quote.Usage.CreatedAt.Before(time.Now().Add(-47*time.Hour)))
	_, err = repo.RefundMedia(ctx, id)
	require.NoError(t, err)
}
