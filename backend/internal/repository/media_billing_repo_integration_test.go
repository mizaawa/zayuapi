//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
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

func TestVideoWorkbenchDeliveryBillingDurability(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	deliveryRepo := repo.(service.MediaWorkbenchBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@video-delivery.test", PasswordHash: "hash", Balance: 2, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "delivery", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "delivery-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		mode, seconds := string(service.BillingModeVideo), 5
		r := &service.MediaBillingReservation{ID: id, WorkbenchKind: "video", WorkbenchDelivery: true, Quote: service.MediaBillingQuote{
			Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .6, APIKeyQuotaCost: .6, ChargedAt: time.Now()},
			Usage:   service.UsageLog{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "grok-imagine-video", ActualCost: .6, TotalCost: .6, BillingMode: &mode, VideoCount: 1, VideoDurationSeconds: &seconds, CreatedAt: time.Now()},
		}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		localID := "vidtask_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.ErrorIs(t, deliveryRepo.BindMediaWorkbenchTask(ctx, r.ID, localID, user.ID+1, key.ID), service.ErrUsageBillingRequestConflict)
		require.NoError(t, deliveryRepo.BindMediaWorkbenchTask(ctx, r.ID, localID, user.ID, key.ID))
		require.NoError(t, repo.DispatchMediaQueue(ctx, r.ID, user.ID, key.ID, account.ID))
		require.NoError(t, repo.BindMediaTask(ctx, r.ID, "upstream-"+uuid.NewString(), service.GrokMediaEndpointVideosGenerations))
		return r
	}
	failed, delivered := newHold(), newHold()
	final := delivered.Quote
	final.Command.BalanceCost, final.Command.APIKeyQuotaCost, final.Command.RequestFingerprint = .36, .36, ""
	final.Usage.ActualCost, final.Usage.TotalCost = .36, .36
	for _, r := range []*service.MediaBillingReservation{failed, delivered} {
		quote := r.Quote
		quote.Command.BalanceCost, quote.Command.APIKeyQuotaCost, quote.Command.RequestFingerprint = .36, .36, ""
		require.NoError(t, deliveryRepo.StageMediaWorkbenchVideoOutcome(ctx, r.ID, &quote))
		_, err := repo.SettleMedia(ctx, r.ID, &quote.Command)
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict, "provider completion alone cannot settle a workbench task")
	}
	var balance, quota float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, .8, balance, 1e-8)
	items, err := repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	for _, r := range []*service.MediaBillingReservation{failed, delivered} {
		var recovered *service.MediaBillingReservation
		for _, item := range items {
			if item.ID == r.ID {
				recovered = item
			}
		}
		require.NotNil(t, recovered)
		require.True(t, recovered.WorkbenchDelivery)
		require.True(t, service.ValidVideoWorkbenchTaskID(recovered.WorkbenchTaskID))
		require.NotNil(t, recovered.VideoOutcome)
		require.Nil(t, recovered.FinalQuote, "the persisted candidate must remain refundable")
		require.InDelta(t, .36, recovered.VideoOutcome.Command.BalanceCost, 1e-8)
	}
	// A content 403 can refund even though the provider already completed.
	require.NoError(t, repo.StageMediaOutcome(ctx, failed.ID, nil))
	for range 3 {
		_, err = repo.RefundMedia(ctx, failed.ID)
		require.NoError(t, err)
	}
	// A successful local save stages its terminal outcome before ledger writes.
	require.NoError(t, repo.StageMediaOutcome(ctx, delivered.ID, &final))
	_, err = repo.RefundMedia(ctx, delivered.ID)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	_, err = integrationDB.Exec(`UPDATE media_billing_reservations SET next_check_at=NOW() WHERE id=$1`, delivered.ID)
	require.NoError(t, err)
	items, err = repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	var recovered *service.MediaBillingReservation
	for _, item := range items {
		if item.ID == delivered.ID {
			recovered = item
		}
	}
	require.NotNil(t, recovered)
	require.NotNil(t, recovered.FinalQuote)
	for i := range 3 {
		result, err := repo.SettleMedia(ctx, recovered.ID, &recovered.FinalQuote.Command)
		require.NoError(t, err)
		require.Equal(t, i == 0, result.Applied)
	}
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, key.ID).Scan(&quota))
	require.InDelta(t, 1.64, balance, 1e-8)
	require.InDelta(t, .36, quota, 1e-8)
	for _, r := range []*service.MediaBillingReservation{failed, delivered} {
		var count int
		var stage string
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM media_billing_events WHERE reservation_id=$1`, r.ID).Scan(&count))
		require.Equal(t, 3, count)
		require.NoError(t, integrationDB.QueryRow(`SELECT stage FROM media_billing_events WHERE reservation_id=$1 ORDER BY ordinal DESC LIMIT 1`, r.ID).Scan(&stage))
		if r.ID == failed.ID {
			require.Equal(t, "refunded", stage)
		} else {
			require.Equal(t, "settled", stage)
		}
	}
}

func TestMediaWorkbenchPaymentEventsAndStatistics(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@payment.test", PasswordHash: "hash", Balance: .10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "payment", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "payment-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		mode := "image"
		size := "1K"
		r := &service.MediaBillingReservation{ID: id, WorkbenchKind: "image", Quote: service.MediaBillingQuote{
			Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .03, APIKeyQuotaCost: .03, ChargedAt: time.Now()},
			Usage:   service.UsageLog{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, Model: "fixture-image", ActualCost: .03, TotalCost: .03, BillingMode: &mode, ImageCount: 1, ImageSize: &size, CreatedAt: time.Now()},
		}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		return r
	}
	success, failure := newHold(), newHold()
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, .04, balance, 1e-8, "queued tasks are already paid")
	cleanup := &usageCleanupRepository{sql: integrationDB}
	cutoff := time.Now().Add(time.Minute)
	_, err := cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{StartTime: time.Now().Add(-time.Hour), EndTime: cutoff, UserID: &user.ID}, 10000)
	require.NoError(t, err)
	_, err = cleanup.DeleteUsageLogsBeforeBatch(ctx, cutoff, 10000)
	require.NoError(t, err)
	var logID int64
	require.NoError(t, integrationDB.QueryRow(`SELECT MAX(id) FROM usage_logs WHERE user_id=$1`, user.ID).Scan(&logID))
	_, err = cleanup.DeleteUsageLogsThroughIDBatch(ctx, logID, 10000)
	require.NoError(t, err)
	require.NoError(t, (&usageLogRepository{sql: integrationDB}).Delete(ctx, logID))
	require.NoError(t, (&dashboardAggregationRepository{sql: integrationDB}).CleanupUsageLogs(ctx, cutoff))
	var pendingLogs int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE user_id=$1`, user.ID).Scan(&pendingLogs))
	require.Equal(t, 2, pendingLogs, "all cleanup paths preserve pending payment logs")
	final := success.Quote
	final.Command.BalanceCost, final.Command.APIKeyQuotaCost, final.Command.RequestFingerprint = .02, .02, ""
	final.Usage.ActualCost, final.Usage.TotalCost = .02, .02
	require.NoError(t, repo.StageMediaOutcome(ctx, success.ID, &final))
	for i := 0; i < 4; i++ {
		_, err := repo.SettleMedia(ctx, success.ID, &final.Command)
		require.NoError(t, err)
		_, err = repo.RefundMediaQueue(ctx, failure.ID, false)
		require.NoError(t, err)
	}
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.InDelta(t, .08, balance, 1e-8)
	for _, r := range []*service.MediaBillingReservation{success, failure} {
		var count int
		require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM media_billing_events WHERE reservation_id=$1`, r.ID).Scan(&count))
		require.Equal(t, 3, count)
	}
	var count int
	var cost float64
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*),SUM(actual_cost) FROM usage_logs WHERE user_id=$1`, user.ID).Scan(&count, &cost))
	require.Equal(t, 2, count)
	require.InDelta(t, .02, cost, 1e-8, "audit events do not triple billed totals")
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(requests),SUM(actual_cost) FROM usage_counters WHERE user_id=$1 AND bucket_date='0001-01-01'`, user.ID).Scan(&count, &cost))
	require.Equal(t, 2, count)
	require.InDelta(t, .02, cost, 1e-8)
	logs := []service.UsageLog{success.Quote.Usage, failure.Quote.Usage}
	logRepo := &usageLogRepository{sql: integrationDB}
	require.NoError(t, logRepo.hydrateMediaBillingEvents(ctx, logs))
	require.Len(t, logs[0].BillingEvents, 3)
	require.Equal(t, "settled", logs[0].BillingEvents[2].Stage)
	require.Equal(t, "refunded", logs[1].BillingEvents[2].Stage)
	forged := []service.UsageLog{{RequestID: success.ID, UserID: user.ID + 1, APIKeyID: key.ID}}
	require.NoError(t, logRepo.hydrateMediaBillingEvents(ctx, forged))
	require.Empty(t, forged[0].BillingEvents)
	deleted, err := cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{StartTime: time.Now().Add(-time.Hour), EndTime: cutoff, UserID: &user.ID}, 10000)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted, "finalized payments can be cleaned normally")
}

func TestMediaWorkbenchQueueDispatchRefundRace(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@race.test", PasswordHash: "hash", Balance: 1, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "race", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "race-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	newHold := func() *service.MediaBillingReservation {
		id := "media:" + uuid.NewString()
		r := &service.MediaBillingReservation{ID: id, WorkbenchKind: "video", Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .03, APIKeyQuotaCost: .03, ChargedAt: time.Now()}}}
		require.NoError(t, repo.ReserveMedia(ctx, r))
		return r
	}
	r := newHold()
	_, err := integrationDB.Exec(`UPDATE media_billing_reservations SET queue_lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, r.ID)
	require.NoError(t, err)
	items, err := repo.PendingMedia(ctx, 100)
	require.NoError(t, err)
	found := false
	for _, item := range items {
		if item.ID == r.ID {
			found = true
		}
	}
	require.True(t, found, "restart recovery finds expired queued holds")
	require.NoError(t, repo.RenewMediaQueue(ctx, r.ID, user.ID, key.ID))
	refunded, err := repo.RefundMediaQueue(ctx, r.ID, true)
	require.NoError(t, err)
	require.Nil(t, refunded, "stale expiry snapshot cannot refund renewed lease")
	for _, args := range [][3]int64{{user.ID + 1, key.ID, account.ID}, {user.ID, key.ID + 1, account.ID}, {user.ID, key.ID, account.ID + 1}} {
		require.ErrorIs(t, repo.DispatchMediaQueue(ctx, r.ID, args[0], args[1], args[2]), service.ErrUsageBillingRequestConflict)
	}
	require.NoError(t, repo.DispatchMediaQueue(ctx, r.ID, user.ID, key.ID, account.ID))
	refunded, err = repo.RefundMediaQueue(ctx, r.ID, false)
	require.NoError(t, err)
	require.Nil(t, refunded, "deletion does not refund an already dispatched generation")
	_, err = repo.RefundMedia(ctx, r.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.DispatchMediaQueue(ctx, r.ID, user.ID, key.ID, account.ID), service.ErrUsageBillingRequestConflict)
	for i := 0; i < 12; i++ {
		r := newHold()
		var wg sync.WaitGroup
		var dispatchErr error
		var refund *service.MediaBillingReservation
		var refundErr error
		wg.Add(2)
		go func() { defer wg.Done(); dispatchErr = repo.DispatchMediaQueue(ctx, r.ID, user.ID, key.ID, account.ID) }()
		go func() { defer wg.Done(); refund, refundErr = repo.RefundMediaQueue(ctx, r.ID, false) }()
		wg.Wait()
		require.NoError(t, refundErr)
		if dispatchErr == nil {
			require.Nil(t, refund)
		} else {
			require.ErrorIs(t, dispatchErr, service.ErrUsageBillingRequestConflict)
			require.NotNil(t, refund)
		}
		for j := 0; j < 4; j++ {
			_, err := repo.RefundMedia(ctx, r.ID)
			require.NoError(t, err)
		}
	}
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.Equal(t, 1.0, balance, "refund replays never increase the starting balance")
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
