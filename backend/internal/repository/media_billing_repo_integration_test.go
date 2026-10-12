//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMediaPrechargeConcurrentBalanceAndRefund(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@media.test", PasswordHash: "hash", Balance: 0.10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "media", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "media-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	var wg sync.WaitGroup
	accepted := make(chan *service.MediaBillingReservation, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "media:" + uuid.NewString()
			r := &service.MediaBillingReservation{ID: id, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: 0.03, APIKeyQuotaCost: 0.03, APIKeyRateLimitCost: 0.03, ChargedAt: time.Now()}}}
			if err := repo.ReserveMedia(ctx, r); err == nil {
				accepted <- r
			} else {
				require.ErrorIs(t, err, service.ErrInsufficientBalance)
			}
		}()
	}
	wg.Wait()
	close(accepted)
	require.Len(t, accepted, 3)
	var balance, quota float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 0.01, balance, 1e-8)
	for r := range accepted {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(id string) { defer wg.Done(); _, err := repo.RefundMedia(ctx, id); require.NoError(t, err) }(r.ID)
		}
	}
	wg.Wait()
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 0.10, balance, 1e-8)
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, key.ID).Scan(&quota))
	require.Zero(t, quota)
}

func TestMediaPrechargeSettlementAndOwnership(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@media.test", PasswordHash: "hash", Balance: 1, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "media", Status: service.StatusAPIKeyActive, Quota: 0.05})
	account := mustCreateAccount(t, client, &service.Account{Name: "media-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		return &service.MediaBillingReservation{ID: id, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: 0.04, APIKeyQuotaCost: 0.04, ChargedAt: time.Now()}}}
	}
	r := newHold()
	require.NoError(t, repo.ReserveMedia(ctx, r))
	require.ErrorIs(t, repo.ReserveMedia(ctx, newHold()), service.ErrInsufficientBalance)
	task := fmt.Sprintf("task-%s", uuid.NewString())
	require.NoError(t, repo.BindMediaTask(ctx, r.ID, task, service.GrokMediaEndpointVideosGenerations))
	other, err := repo.FindMediaTask(ctx, task, user.ID+1, key.ID)
	require.NoError(t, err)
	require.Nil(t, other)
	command := r.Quote.Command
	command.BalanceCost, command.APIKeyQuotaCost = 0.02, 0.02
	command.RequestFingerprint = ""
	first, err := repo.SettleMedia(ctx, r.ID, &command)
	require.NoError(t, err)
	require.True(t, first.Applied)
	second, err := repo.SettleMedia(ctx, r.ID, &command)
	require.NoError(t, err)
	require.False(t, second.Applied)
	refunded, err := repo.RefundMedia(ctx, r.ID)
	require.NoError(t, err)
	require.Nil(t, refunded)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, 0.98, balance, 1e-8)
}

func TestMediaPrechargeDurableOutcomes(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@media.test", PasswordHash: "hash", Balance: 1, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "media", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "media-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		r := &service.MediaBillingReservation{ID: id, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .04, APIKeyQuotaCost: .04, ChargedAt: time.Now()}}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		return r
	}
	success, failure, unknown := newHold(), newHold(), newHold()
	final := success.Quote
	final.Command.BalanceCost, final.Command.APIKeyQuotaCost, final.Command.RequestFingerprint = .02, .02, ""
	require.NoError(t, repo.StageMediaOutcome(ctx, success.ID, &final))
	require.NoError(t, repo.StageMediaOutcome(ctx, failure.ID, nil))
	// Age alone cannot establish that the provider failed or never ran.
	_, err := integrationDB.Exec(`UPDATE media_billing_reservations SET created_at=NOW()-INTERVAL '4 hours' WHERE id=$1`, unknown.ID)
	require.NoError(t, err)
	items, err := repo.PendingMedia(ctx, 100)
	require.NoError(t, err)
	var confirmed int
	for _, item := range items {
		require.NotEqual(t, unknown.ID, item.ID)
		if item.ID == success.ID {
			require.NotNil(t, item.FinalQuote)
			_, err := repo.RefundMedia(ctx, item.ID)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			applied, err := repo.SettleMedia(ctx, item.ID, &item.FinalQuote.Command)
			require.NoError(t, err)
			require.True(t, applied.Applied)
			confirmed++
		}
		if item.ID == failure.ID {
			require.True(t, item.RefundPending)
			_, err := repo.RefundMedia(ctx, item.ID)
			require.NoError(t, err)
			confirmed++
		}
	}
	require.Equal(t, 2, confirmed)
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, .94, balance, 1e-8)
	_, err = repo.RefundMedia(ctx, unknown.ID)
	require.NoError(t, err)
}

func TestMediaPrechargeSubscriptionUsesSubscriptionFunds(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@media.test", PasswordHash: "hash", Balance: -1, Status: service.StatusActive})
	limit := .05
	group := mustCreateGroup(t, client, &service.Group{Name: "media-sub-" + uuid.NewString(), Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, GroupID: &group.ID, Key: "sk-" + uuid.NewString(), Name: "media", Status: service.StatusAPIKeyActive, Quota: 10})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, GroupID: group.ID})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		return &service.MediaBillingReservation{ID: id, GroupID: group.ID, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, SubscriptionID: &subscription.ID, SubscriptionCost: .04, APIKeyQuotaCost: .04, ChargedAt: time.Now()}}}
	}
	r := newHold()
	require.NoError(t, repo.ReserveMedia(ctx, r))
	require.ErrorIs(t, repo.ReserveMedia(ctx, newHold()), service.ErrDailyLimitExceeded)
	_, err := repo.RefundMedia(ctx, r.ID)
	require.NoError(t, err)
	var balance, used float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd FROM user_subscriptions WHERE id=$1`, subscription.ID).Scan(&used))
	require.Equal(t, -1.0, balance)
	require.Zero(t, used)
}
