//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVideoDeliverySettlementPrefersPersistedOutcomeOverStaleSnapshot(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.MediaBillingRepository)
	delivery := repo.(service.MediaWorkbenchBillingRepository)
	user := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@video-snapshot.test", PasswordHash: "hash", Balance: 10, Status: service.StatusActive})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + uuid.NewString(), Name: "video snapshot", Status: service.StatusAPIKeyActive, Quota: 10})
	account := mustCreateAccount(t, client, &service.Account{Name: "video snapshot " + uuid.NewString(), Type: service.AccountTypeAPIKey})
	id := "media:" + uuid.NewString()
	mode, seconds, resolution := string(service.BillingModeVideo), 5, "720p"
	r := &service.MediaBillingReservation{
		ID: id, WorkbenchKind: "video", WorkbenchDelivery: true,
		Quote: service.MediaBillingQuote{
			Command: service.UsageBillingCommand{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, AccountType: service.AccountTypeAPIKey, BalanceCost: .5, APIKeyQuotaCost: .5, ChargedAt: time.Now()},
			Usage: service.UsageLog{RequestID: id, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID,
				Model: "snapshot-video", ActualCost: .5, TotalCost: .5, BillingMode: &mode,
				VideoCount: 1, VideoDurationSeconds: &seconds, VideoResolution: &resolution, CreatedAt: time.Now()},
		},
	}
	require.NoError(t, repo.ReserveMedia(ctx, r))
	localID := "vidtask_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, delivery.BindMediaWorkbenchTask(ctx, id, localID, user.ID, key.ID))
	require.NoError(t, repo.DispatchMediaQueue(ctx, id, user.ID, key.ID, account.ID))
	require.NoError(t, repo.BindMediaTask(ctx, id, "upstream-"+uuid.NewString(), service.GrokMediaEndpointVideosGenerations))

	items, err := repo.PendingMedia(ctx, 1000)
	require.NoError(t, err)
	var stale *service.MediaBillingReservation
	for _, item := range items {
		if item.ID == id {
			stale = item
		}
	}
	require.NotNil(t, stale)
	require.Nil(t, stale.VideoOutcome)
	actual := r.Quote
	actual.Command.BalanceCost, actual.Command.APIKeyQuotaCost, actual.Command.RequestFingerprint = .3, .3, ""
	actual.Usage.ActualCost, actual.Usage.TotalCost = .3, .3
	actualSeconds := 3
	actual.Usage.VideoDurationSeconds = &actualSeconds
	require.NoError(t, delivery.StageMediaWorkbenchVideoOutcome(ctx, id, &actual))

	// A recovery worker fetched this reservation before the runner persisted
	// the actual duration. After observing completed delivery it must not lock
	// the older admission estimate into final_quote.
	require.NoError(t, repo.StageMediaOutcome(ctx, id, &stale.Quote))
	for attempt := 0; attempt < 3; attempt++ {
		command := stale.Quote.Command
		result, err := repo.SettleMedia(ctx, id, &command)
		require.NoError(t, err)
		require.Equal(t, attempt == 0, result.Applied)
	}
	var balance, frozen, quota, billed, total float64
	var billedSeconds int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=$1`, user.ID).Scan(&balance, &frozen))
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, key.ID).Scan(&quota))
	require.NoError(t, integrationDB.QueryRow(`SELECT actual_cost,total_cost,video_duration_seconds FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, id, key.ID).Scan(&billed, &total, &billedSeconds))
	t.Logf("admission=.50000000 actual=.30000000; balance=%.8f frozen=%.8f quota=%.8f billed=%.8f seconds=%d", balance, frozen, quota, billed, billedSeconds)
	require.InDelta(t, 9.7, balance, 1e-8, "the stale recovery worker must use the persisted actual quote")
	require.Zero(t, frozen)
	require.InDelta(t, .3, quota, 1e-8)
	require.InDelta(t, .3, billed, 1e-8)
	require.InDelta(t, .3, total, 1e-8)
	require.Equal(t, 3, billedSeconds)
}
