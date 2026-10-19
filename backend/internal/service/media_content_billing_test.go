//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func mediaContentBillingContext(method string) (*gin.Context, *httptest.ResponseRecorder, context.Context) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := WithMediaTaskContentBilling(context.Background(), "task-recovery", 71, 81, nil)
	c.Request = httptest.NewRequest(method, "/v1/videos/task-recovery/content", nil).WithContext(ctx)
	return c, rec, ctx
}

func TestMediaAPIContentDeliveredBeforeOutageRemainsCharged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &mediaRecoveryUpstream{responses: []*http.Response{
		mediaRecoveryResponse(200, "video/mp4", "paid-video-bytes"),
		mediaRecoveryResponse(503, "application/json", `{"error":"status unavailable"}`),
		mediaRecoveryResponse(503, "application/json", `{"error":"content unavailable"}`),
	}}
	repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
	svc, reservation := newMediaRecoveryService(upstream, repo, time.Now().Add(-time.Hour))
	c, rec, ctx := mediaContentBillingContext(http.MethodGet)
	account, err := svc.accountRepo.GetByID(ctx, 17)
	require.NoError(t, err)
	_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoContent, reservation.TaskID, nil, "")
	require.NoError(t, err)
	require.Equal(t, "paid-video-bytes", rec.Body.String())
	t.Logf("before outage: delivered=%q state=%q settlements=%d refunds=%d", rec.Body.String(), repo.state, repo.settleCalls, repo.refundCalls)
	require.NoError(t, svc.reconcilePendingMedia(ctx, reservation))
	t.Logf("after outage: delivered=%q state=%q settlements=%d refunds=%d", rec.Body.String(), repo.state, repo.settleCalls, repo.refundCalls)
	require.Equal(t, "settled", repo.state, "a later status/content outage cannot refund a delivered video")
	require.NotNil(t, repo.staged, "a restart must preserve the successful delivery")
	require.Zero(t, repo.refundCalls)
	t.Logf("delivered=%q state=%s settlements=%d refunds=%d", rec.Body.String(), repo.state, repo.settleCalls, repo.refundCalls)
}

type mediaContentErrorReader struct{ sent bool }

func (r *mediaContentErrorReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "truncated"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestMediaContentDoesNotChargeOrReleaseFailedUpstreamBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   io.Reader
		header http.Header
		limit  int64
	}{
		{name: "truncated", body: &mediaContentErrorReader{}},
		{name: "empty", body: strings.NewReader("")},
		{name: "wrong length", body: strings.NewReader("short"), header: http.Header{"Content-Length": {"99"}}},
		{name: "too large", body: strings.NewReader("oversized"), limit: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
			response := mediaRecoveryResponse(200, "video/mp4", "")
			response.Body = io.NopCloser(tc.body)
			for name, values := range tc.header {
				response.Header[name] = values
			}
			upstream := &mediaRecoveryUpstream{responses: []*http.Response{response}}
			svc, _ := newMediaRecoveryService(upstream, repo, time.Now())
			if tc.limit > 0 {
				svc.cfg.Gateway.UpstreamResponseReadMaxBytes = tc.limit
			}
			c, rec, ctx := mediaContentBillingContext(http.MethodGet)
			account, err := svc.accountRepo.GetByID(ctx, 17)
			require.NoError(t, err)
			_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoContent, "task-recovery", nil, "")
			require.Error(t, err)
			require.Empty(t, rec.Body.String())
			require.False(t, rec.Flushed)
			require.Empty(t, repo.state)
			require.Nil(t, repo.staged)
		})
	}
}

func TestMediaContentHeadAndRangeBilling(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		status             int
		wantCharge         bool
	}{
		{"head", http.MethodHead, "body-not-sent", 200, false},
		{"empty range", http.MethodGet, "", 206, false},
		{"byte range", http.MethodGet, "x", 206, true},
		{"no content", http.MethodGet, "", 204, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
			upstream := &mediaRecoveryUpstream{responses: []*http.Response{mediaRecoveryResponse(tc.status, "video/mp4", tc.body)}}
			svc, _ := newMediaRecoveryService(upstream, repo, time.Now())
			c, rec, ctx := mediaContentBillingContext(tc.method)
			c.Request.Header.Set("Range", "bytes=0-0")
			account, err := svc.accountRepo.GetByID(ctx, 17)
			require.NoError(t, err)
			_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoContent, "task-recovery", nil, "")
			if tc.status == 206 && tc.body == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantCharge {
				require.Equal(t, "settled", repo.state)
				require.Equal(t, tc.body, rec.Body.String())
			} else {
				require.Empty(t, repo.state)
				require.Empty(t, rec.Body.String())
			}
		})
	}
}

func TestMediaContentRefusesBytesAfterRefundOrFailedSettlement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		refunded  bool
		settleErr error
	}{
		{name: "already refunded", refunded: true},
		{name: "ledger unavailable", settleErr: errors.New("ledger unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{settleErr: tc.settleErr}}
			upstream := &mediaRecoveryUpstream{responses: []*http.Response{mediaRecoveryResponse(200, "video/mp4", "never-free")}}
			svc, _ := newMediaRecoveryService(upstream, repo, time.Now())
			if tc.refunded {
				repo.state = "refunded"
			}
			c, rec, ctx := mediaContentBillingContext(http.MethodGet)
			account, err := svc.accountRepo.GetByID(ctx, 17)
			require.NoError(t, err)
			_, err = svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoContent, "task-recovery", nil, "")
			require.Error(t, err)
			require.Empty(t, rec.Body.String())
		})
	}
}

// Model the same first-terminal-outcome rule as the repository, under a lock,
// so a refund racing the final upstream read can never yield free bytes.
type mediaContentRaceRepo struct {
	UsageBillingRepository
	MediaBillingRepository
	mu          sync.Mutex
	reservation *MediaBillingReservation
	state       string
	final       *MediaBillingQuote
}

func (r *mediaContentRaceRepo) FindMediaTask(_ context.Context, task string, user, key int64) (*MediaBillingReservation, error) {
	if task != r.reservation.TaskID || user != r.reservation.Quote.Command.UserID || key != r.reservation.Quote.Command.APIKeyID {
		return nil, nil
	}
	return r.reservation, nil
}
func (r *mediaContentRaceRepo) StageMediaOutcome(_ context.Context, _ string, q *MediaBillingQuote) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "refunded" || r.state == "settled" {
		return nil
	}
	if q != nil {
		if r.state != "refund pending" {
			r.final = q
		}
	} else if r.final == nil {
		r.state = "refund pending"
	}
	return nil
}
func (r *mediaContentRaceRepo) SettleMedia(context.Context, string, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "refunded" || r.state == "refund pending" {
		return nil, ErrUsageBillingRequestConflict
	}
	r.state = "settled"
	return &UsageBillingApplyResult{Applied: true}, nil
}
func (r *mediaContentRaceRepo) RefundMedia(context.Context, string) (*MediaBillingReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "settled" {
		return nil, nil
	}
	if r.final != nil {
		return nil, ErrUsageBillingRequestConflict
	}
	r.state = "refunded"
	return r.reservation, nil
}

func TestMediaContentConcurrentRefundArbitratesBeforeFirstByte(t *testing.T) {
	for range 50 {
		r := &mediaContentRaceRepo{reservation: &MediaBillingReservation{ID: "media:race", TaskID: "task-recovery", Quote: MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:race", UserID: 71, APIKeyID: 81, BalanceCost: .25}}}}
		svc := &OpenAIGatewayService{usageBillingRepo: r}
		c, rec, ctx := mediaContentBillingContext(http.MethodGet)
		start := make(chan struct{})
		done := make(chan struct{})
		go func() { defer close(done); <-start; _ = svc.RefundMedia(ctx, r.reservation.ID) }()
		close(start)
		err := svc.writeMediaTaskContentResponse(ctx, c, mediaRecoveryResponse(200, "video/mp4", "video"))
		<-done
		r.mu.Lock()
		state := r.state
		r.mu.Unlock()
		if rec.Body.Len() > 0 {
			require.NoError(t, err)
			require.Equal(t, "settled", state)
		} else {
			require.Error(t, err)
			require.Equal(t, "refunded", state)
		}
	}
}
