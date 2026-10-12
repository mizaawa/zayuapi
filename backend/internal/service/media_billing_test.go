//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type mediaOutcomeRepository struct {
	UsageBillingRepository
	MediaBillingRepository
	staged  *MediaBillingQuote
	settled *UsageBillingCommand
	refunds int
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
