//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mediaOutcomeRepository struct {
	UsageBillingRepository
	MediaBillingRepository
	staged       *MediaBillingQuote
	settled      *UsageBillingCommand
	refunds      int
	reserved     *MediaBillingReservation
	videoOutcome *MediaBillingQuote
	settleErr    error
}

func (r *mediaOutcomeRepository) StageMediaWorkbenchVideoOutcome(_ context.Context, _ string, quote *MediaBillingQuote) error {
	r.videoOutcome = quote
	return nil
}

func (r *mediaOutcomeRepository) BindMediaWorkbenchTask(context.Context, string, string, int64, int64) error {
	return nil
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
	if r.settleErr != nil {
		return nil, r.settleErr
	}
	r.settled = cmd
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestVideoWorkbenchDeliveryRetriesDurableSettlement(t *testing.T) {
	repo := &mediaOutcomeRepository{settleErr: errors.New("ledger unavailable")}
	s := &OpenAIGatewayService{usageBillingRepo: repo}
	r := &MediaBillingReservation{ID: "media:delivered", WorkbenchKind: "video", WorkbenchDelivery: true, WorkbenchTaskID: "local", Quote: MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:delivered", UserID: 7, APIKeyID: 9, BalanceCost: .6}}}
	store := &mediaDeliveryStore{task: VideoWorkbenchRecord{VideoWorkbenchTask: VideoWorkbenchTask{ID: "local", Status: "completed", Workbench: VideoWorkbenchMetadata{APIKeyID: 9}}, UserID: 7}}
	s.SetMediaBillingVideoWorkbench(NewVideoWorkbenchServiceWithSettings(store, nil, nil))
	require.Error(t, s.FinalizeMediaWorkbenchVideo(context.Background(), r, []byte(`{"status":"completed"}`)))
	require.NotNil(t, repo.staged, "delivery must be durable before attempting ledger writes")
	require.Nil(t, repo.settled)
	require.Zero(t, repo.refunds)
	// Recovery no longer needs an upstream call or a retained local task record.
	recovered := &MediaBillingReservation{ID: r.ID, WorkbenchDelivery: true, FinalQuote: repo.staged}
	repo.settleErr = nil
	s.SetMediaBillingVideoWorkbench(nil)
	require.NoError(t, s.reconcilePendingMedia(context.Background(), recovered))
	require.InDelta(t, .6, repo.settled.BalanceCost, 1e-8)
	require.Zero(t, repo.refunds)
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

type mediaDeliveryStore struct {
	VideoWorkbenchStore
	task          VideoWorkbenchRecord
	heartbeatWins bool
}

func (s *mediaDeliveryStore) Get(context.Context, string) (*VideoWorkbenchRecord, error) {
	task := s.task
	return &task, nil
}

func (s *mediaDeliveryStore) Finish(_ context.Context, task *VideoWorkbenchRecord, _ time.Duration, expiredBefore int64) (bool, error) {
	if s.heartbeatWins && expiredBefore > 0 {
		s.task.LeaseUntil = time.Now().Add(time.Minute).Unix()
		return false, nil
	}
	s.task = *task
	return true, nil
}

func TestVideoWorkbenchBillingWaitsForLocalDelivery(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "interrupted", "heartbeat"} {
		t.Run(outcome, func(t *testing.T) {
			repo := &mediaOutcomeRepository{}
			s := &OpenAIGatewayService{usageBillingRepo: repo}
			seconds, mode := 5, string(BillingModeVideo)
			r := &MediaBillingReservation{ID: "media:delivery", WorkbenchKind: "video", WorkbenchDelivery: true, WorkbenchTaskID: "local-task", Quote: MediaBillingQuote{
				Command: UsageBillingCommand{RequestID: "media:delivery", UserID: 7, APIKeyID: 9, BalanceCost: .5, APIKeyQuotaCost: .5},
				Usage:   UsageLog{ActualCost: .5, TotalCost: .5, BillingMode: &mode, VideoDurationSeconds: &seconds},
			}}
			store := &mediaDeliveryStore{task: VideoWorkbenchRecord{VideoWorkbenchTask: VideoWorkbenchTask{ID: "local-task", Status: "processing", Workbench: VideoWorkbenchMetadata{APIKeyID: 9}}, UserID: 7, LeaseUntil: time.Now().Add(time.Minute).Unix(), Deadline: time.Now().Add(time.Hour).Unix()}}
			s.SetMediaBillingVideoWorkbench(NewVideoWorkbenchServiceWithSettings(store, nil, nil))
			require.NoError(t, s.settleMediaStatus(context.Background(), r, []byte(`{"status":"completed","duration":3}`)))
			require.NotNil(t, repo.videoOutcome)
			require.Nil(t, repo.staged, "upstream completion cannot commit billing before local delivery")
			require.Nil(t, repo.settled)
			r.VideoOutcome = repo.videoOutcome
			encoded, err := json.Marshal(r)
			require.NoError(t, err)
			var restored MediaBillingReservation
			require.NoError(t, json.Unmarshal(encoded, &restored))
			require.NoError(t, s.reconcilePendingMedia(context.Background(), &restored))
			require.Nil(t, repo.settled)
			require.Zero(t, repo.refunds)
			switch outcome {
			case "completed", "failed":
				store.task.Status = outcome
			default:
				store.task.LeaseUntil = time.Now().Add(-time.Second).Unix()
				store.heartbeatWins = outcome == "heartbeat"
			}
			require.NoError(t, s.reconcilePendingMedia(context.Background(), &restored))
			if outcome == "completed" {
				require.NotNil(t, repo.settled)
				require.InDelta(t, .3, repo.settled.BalanceCost, 1e-8)
				require.Equal(t, 3, *repo.staged.Usage.VideoDurationSeconds)
				require.Zero(t, repo.refunds)
			} else if outcome == "heartbeat" {
				require.Nil(t, repo.settled)
				require.Zero(t, repo.refunds, "a renewed lease cannot be refunded by a stale reconciliation")
			} else {
				require.Nil(t, repo.settled)
				require.Equal(t, 1, repo.refunds)
			}
		})
	}
}

func TestMediaPrechargeDoesNotSnapshotUnconfiguredResolutionAsFree(t *testing.T) {
	const groupID int64 = 730
	for _, explicitFree := range []bool{false, true} {
		repo := &mediaOutcomeRepository{}
		s := newOpenAIRecordUsageServiceForTest(&openAIRecordUsageLogRepoStub{}, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		s.usageBillingRepo, s.billingCacheService = repo, nil
		price, zero := .1, 0.0
		group := &Group{ID: groupID, Platform: PlatformCustom, VideoRateIndependent: true, VideoRateMultiplier: 1, VideoPrice480P: &price}
		if explicitFree {
			group.VideoPrice720P = &zero
		}
		r, err := s.PrechargeMedia(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{Model: "grok-imagine-video", VideoCount: 1, VideoResolution: "480p", VideoDurationSeconds: 5},
			APIKey: &APIKey{ID: 101, GroupID: i64p(groupID), Group: group}, User: &User{ID: 201},
			Account: &Account{ID: 301, Platform: PlatformCustom},
		})
		require.NoError(t, err)
		require.NotContains(t, r.VideoPrices, "1080p")
		_, found := r.VideoPrices["720p"]
		require.Equal(t, explicitFree, found)
		require.NoError(t, s.settleMediaStatus(context.Background(), r, []byte(`{"status":"completed","duration":5,"resolution":"720p"}`)))
		if explicitFree {
			require.Zero(t, repo.settled.BalanceCost)
		} else {
			require.InDelta(t, .5, repo.settled.BalanceCost, 1e-8)
		}
	}
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
