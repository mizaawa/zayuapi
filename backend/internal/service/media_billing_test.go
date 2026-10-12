//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type mediaOutcomeRepository struct {
	UsageBillingRepository
	MediaBillingRepository
	staged   *MediaBillingQuote
	settled  *UsageBillingCommand
	refunds  int
	reserved *MediaBillingReservation
}

func (r *mediaOutcomeRepository) ReserveMedia(_ context.Context, reservation *MediaBillingReservation) error {
	r.reserved = reservation
	return nil
}

func (r *mediaOutcomeRepository) StageMediaOutcome(_ context.Context, _ string, quote *MediaBillingQuote) error {
	r.staged = quote
	return nil
}
func (r *mediaOutcomeRepository) SettleMedia(_ context.Context, _ string, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.settled = cmd
	return &UsageBillingApplyResult{Applied: true}, nil
}
func (r *mediaOutcomeRepository) RefundMedia(context.Context, string) (*MediaBillingReservation, error) {
	r.refunds++
	return nil, nil
}

func TestMediaSettlementUsesActualDurationAndAdmissionPrice(t *testing.T) {
	repo := &mediaOutcomeRepository{}
	s := &OpenAIGatewayService{usageBillingRepo: repo}
	seconds, mode, tier := 4, string(BillingModeVideo), "720p"
	quote := MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:test", BalanceCost: .4, APIKeyQuotaCost: .4}, Usage: UsageLog{ActualCost: .4, TotalCost: .4, BillingMode: &mode, VideoDurationSeconds: &seconds, VideoResolution: &tier}}
	r := &MediaBillingReservation{ID: "media:test", Quote: quote, VideoPrices: map[string]MediaBillingQuote{"720p": quote}}
	require.NoError(t, s.settleMediaStatus(context.Background(), r, []byte(`{"status":"done","video":{"duration":3,"resolution":"720p"}}`)))
	require.NotNil(t, repo.staged)
	require.InDelta(t, .3, repo.settled.BalanceCost, 1e-8)
	require.Equal(t, 3, *repo.staged.Usage.VideoDurationSeconds)
	require.Equal(t, 4, *r.Quote.Usage.VideoDurationSeconds)
	require.NoError(t, s.reconcilePendingMedia(context.Background(), &MediaBillingReservation{ID: r.ID, FinalQuote: repo.staged}))
	require.InDelta(t, .3, repo.settled.BalanceCost, 1e-8)
}

func TestMediaReconciliationDoesNotRefundUnknownOutcome(t *testing.T) {
	repo := &mediaOutcomeRepository{}
	s := &OpenAIGatewayService{usageBillingRepo: repo}
	require.Error(t, s.reconcilePendingMedia(context.Background(), &MediaBillingReservation{ID: "media:unknown"}))
	require.Zero(t, repo.refunds)
	require.NoError(t, s.reconcilePendingMedia(context.Background(), &MediaBillingReservation{ID: "media:failed", RefundPending: true}))
	require.Equal(t, 1, repo.refunds)
}

func TestEightYesMediaPrechargePersistsProtocolAndExactModel(t *testing.T) {
	const groupID int64 = 729
	const model = "videos-standard-720p"
	repo := &mediaOutcomeRepository{}
	s := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	s.usageBillingRepo = repo
	s.billingCacheService = nil
	s.resolver = newCustomVideoChannelPricingResolverForTest(groupID, model, BillingModeVideo, .1)
	reservation, err := s.PrechargeMedia(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{Model: model, BillingModel: model, VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 4},
		APIKey: &APIKey{ID: 101, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformCustom, VideoRateIndependent: true, VideoRateMultiplier: 1}},
		User:   &User{ID: 201}, Account: &Account{ID: 301, Platform: PlatformCustom, Credentials: map[string]any{"media_protocol": "8yes"}},
	})
	require.NoError(t, err)
	require.Same(t, reservation, repo.reserved)
	require.Equal(t, "8yes", reservation.MediaProtocol)
	require.Equal(t, model, reservation.Quote.Usage.Model)
	require.Equal(t, "720p", *reservation.Quote.Usage.VideoResolution)
	require.InDelta(t, .4, reservation.Quote.Command.BalanceCost, 1e-8)
	require.Empty(t, reservation.VideoPrices)
	encoded, err := json.Marshal(reservation)
	require.NoError(t, err)
	var restored MediaBillingReservation
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.Equal(t, "8yes", restored.MediaProtocol)
}

func TestEightYesMediaSettlementIgnoresConflictingResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode BillingMode
		want float64
	}{
		{"per-second", BillingModeVideo, .3},
		{"per-call", BillingModePerRequest, .4},
		{"per-clip", BillingModePerRequest, .4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mediaOutcomeRepository{}
			s := &OpenAIGatewayService{usageBillingRepo: repo}
			seconds, mode, tier, stats := 4, string(tc.mode), "720p", .4
			quote := MediaBillingQuote{
				Command: UsageBillingCommand{RequestID: "media:8yes", Model: "videos-standard-720p", BalanceCost: .4, APIKeyQuotaCost: .4, APIKeyRateLimitCost: .4, AccountQuotaCost: .4, UserPlatformQuotaCost: .4},
				Usage:   UsageLog{Model: "videos-standard-720p", ActualCost: .4, TotalCost: .4, AccountStatsCost: &stats, BillingMode: &mode, VideoDurationSeconds: &seconds, VideoResolution: &tier},
			}
			other := quote
			other.Command.BalanceCost, other.Usage.ActualCost = 4, 4
			other.Usage.VideoResolution = ptr("1080p")
			reservation := &MediaBillingReservation{ID: "media:8yes", MediaProtocol: "8yes", Quote: quote, VideoPrices: map[string]MediaBillingQuote{"1080p": other}}
			before, err := json.Marshal(reservation)
			require.NoError(t, err)
			var restored MediaBillingReservation
			require.NoError(t, json.Unmarshal(before, &restored))
			for range 2 {
				require.NoError(t, s.settleMediaStatus(context.Background(), &restored, []byte(`{"status":"completed","model":"upstream-internal","resolution":"1080p","duration":3}`)))
				require.InDelta(t, tc.want, repo.settled.BalanceCost, 1e-8)
				require.InDelta(t, tc.want, repo.settled.APIKeyQuotaCost, 1e-8)
				require.InDelta(t, tc.want, repo.settled.APIKeyRateLimitCost, 1e-8)
				require.InDelta(t, tc.want, repo.settled.AccountQuotaCost, 1e-8)
				require.InDelta(t, tc.want, repo.settled.UserPlatformQuotaCost, 1e-8)
				require.InDelta(t, tc.want, *repo.staged.Usage.AccountStatsCost, 1e-8)
				require.Equal(t, "720p", *repo.staged.Usage.VideoResolution)
				require.Equal(t, "videos-standard-720p", repo.staged.Usage.Model)
				require.Equal(t, 3, *repo.staged.Usage.VideoDurationSeconds)
			}
			after, err := json.Marshal(&restored)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "settlement must not mutate the persisted price snapshot")
		})
	}
}
