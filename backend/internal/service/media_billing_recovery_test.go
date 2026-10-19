//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type mediaRecoveryAccountRepo struct {
	AccountRepository
	account *Account
}

func (r mediaRecoveryAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}

type mediaRecoveryUpstream struct {
	responses  []*http.Response
	accountIDs []int64
	requests   []*http.Request
}

func (u *mediaRecoveryUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	u.requests = append(u.requests, req)
	if len(u.responses) == 0 {
		return nil, io.EOF
	}
	resp := u.responses[0]
	u.responses = u.responses[1:]
	return resp, nil
}

func (u *mediaRecoveryUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type mediaRecoveryBillingRepo struct {
	*mediaOutcomeRepository
	reservation *MediaBillingReservation
	state       string
	ownerID     int64
	keyID       int64
	settleCalls int
	refundCalls int
}

func (r *mediaRecoveryBillingRepo) FindMediaTask(_ context.Context, taskID string, userID, keyID int64) (*MediaBillingReservation, error) {
	if r.reservation == nil || r.reservation.TaskID != taskID || r.ownerID != userID || r.keyID != keyID {
		return nil, nil
	}
	return r.reservation, nil
}

func (r *mediaRecoveryBillingRepo) StageMediaOutcome(ctx context.Context, id string, quote *MediaBillingQuote) error {
	// Match the persistent repository: terminal outcomes cannot be overwritten
	// by a stale reconciliation attempt after a successful content delivery.
	if r.state == "settled" || r.state == "refunded" {
		return nil
	}
	return r.mediaOutcomeRepository.StageMediaOutcome(ctx, id, quote)
}

func (r *mediaRecoveryBillingRepo) SettleMedia(ctx context.Context, id string, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	if r.state == "settled" {
		return &UsageBillingApplyResult{}, nil
	}
	if r.state == "refunded" {
		return nil, ErrUsageBillingRequestConflict
	}
	r.settleCalls++
	r.state = "settled"
	return r.mediaOutcomeRepository.SettleMedia(ctx, id, cmd)
}

func (r *mediaRecoveryBillingRepo) RefundMedia(ctx context.Context, id string) (*MediaBillingReservation, error) {
	if r.state == "settled" || r.state == "refunded" {
		return nil, nil
	}
	r.refundCalls++
	r.state = "refunded"
	return r.mediaOutcomeRepository.RefundMedia(ctx, id)
}

func mediaRecoveryResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newMediaRecoveryService(upstream *mediaRecoveryUpstream, repo *mediaRecoveryBillingRepo, createdAt time.Time) (*OpenAIGatewayService, *MediaBillingReservation) {
	account := customVideoTestAccount()
	account.ID = 17
	account.Credentials["media_protocol"] = "sora"
	seconds, mode := 1, string(BillingModeVideo)
	reservation := &MediaBillingReservation{
		ID:       "media:task-recovery",
		TaskID:   "task-recovery",
		Endpoint: GrokMediaEndpointVideosGenerations,
		Quote: MediaBillingQuote{
			Command: UsageBillingCommand{RequestID: "media:task-recovery", UserID: 71, APIKeyID: 81, AccountID: account.ID, BalanceCost: .25},
			Usage:   UsageLog{Model: "grok-imagine-video", ActualCost: .25, TotalCost: .25, BillingMode: &mode, VideoDurationSeconds: &seconds, CreatedAt: createdAt},
		},
	}
	// Persisted admission metadata, also understood as an unknown field by
	// the baseline overlay used to reproduce the old delivery/refund bug.
	_ = json.Unmarshal([]byte(`{"content_delivery_guarded":true}`), reservation)
	repo.reservation = reservation
	repo.ownerID, repo.keyID = reservation.Quote.Command.UserID, reservation.Quote.Command.APIKeyID
	svc := &OpenAIGatewayService{
		accountRepo:      mediaRecoveryAccountRepo{account: account},
		usageBillingRepo: repo,
		httpUpstream:     upstream,
		cfg:              customVideoTestConfig(),
	}
	return svc, reservation
}

func TestMediaReconcileStatusFailureSettlesWhenVideoContentIsAvailable(t *testing.T) {
	upstream := &mediaRecoveryUpstream{responses: []*http.Response{
		mediaRecoveryResponse(http.StatusServiceUnavailable, "application/json", `{"error":{"code":"model_not_found"}}`),
		mediaRecoveryResponse(http.StatusOK, "video/mp4", "\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2mp41"),
	}}
	repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
	svc, reservation := newMediaRecoveryService(upstream, repo, time.Now().Add(-48*time.Hour))

	err := svc.reconcilePendingMedia(context.Background(), reservation)

	require.NoError(t, err)
	require.Equal(t, "settled", repo.state, "a successfully retrievable video must be charged, even after the reconciliation deadline")
	require.Equal(t, 1, repo.settleCalls)
	require.Zero(t, repo.refundCalls)
	require.Equal(t, []int64{17, 17}, upstream.accountIDs, "status and content must use the submission account")
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/proxy/v1/videos/task-recovery", upstream.requests[0].URL.Path)
	require.Equal(t, "/proxy/v1/videos/task-recovery/content", upstream.requests[1].URL.Path)
	require.InDelta(t, .25, repo.settled.BalanceCost, 1e-8, "without authoritative status metadata, settle the admitted quote")
}

func TestMediaReconcilePersistentStatusAndContentFailureEventuallyRefunds(t *testing.T) {
	upstream := &mediaRecoveryUpstream{responses: []*http.Response{
		mediaRecoveryResponse(http.StatusServiceUnavailable, "application/json", `{"error":{"code":"model_not_found"}}`),
		mediaRecoveryResponse(http.StatusServiceUnavailable, "application/json", `{"error":{"code":"content_unavailable"}}`),
	}}
	repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
	svc, reservation := newMediaRecoveryService(upstream, repo, time.Now().Add(-48*time.Hour))

	err := svc.reconcilePendingMedia(context.Background(), reservation)

	require.NoError(t, err)
	require.Equal(t, "refunded", repo.state)
	require.Equal(t, 1, repo.refundCalls)
	require.Zero(t, repo.settleCalls)
	require.GreaterOrEqual(t, len(upstream.accountIDs), 2)
	for _, accountID := range upstream.accountIDs {
		require.Equal(t, int64(17), accountID, "every status/content/fallback request must use the submission account")
	}
	require.Equal(t, "/proxy/v1/videos/task-recovery", upstream.requests[0].URL.Path)
	require.Equal(t, "/proxy/v1/videos/task-recovery/content", upstream.requests[1].URL.Path)
}

func TestMediaReconcileDoesNotRefundBeforeStatusOutageGracePeriod(t *testing.T) {
	upstream := &mediaRecoveryUpstream{responses: []*http.Response{
		mediaRecoveryResponse(http.StatusServiceUnavailable, "application/json", `{"error":{"code":"model_not_found"}}`),
	}}
	repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
	svc, reservation := newMediaRecoveryService(upstream, repo, time.Now().Add(-time.Minute))

	err := svc.reconcilePendingMedia(context.Background(), reservation)

	require.Error(t, err, "a recent, ambiguous upstream outage must be retried")
	require.Empty(t, repo.state)
	require.Zero(t, repo.refundCalls)
	require.Zero(t, repo.settleCalls)
	require.Len(t, upstream.requests, 1, "do not probe or refund immediately after a transient status failure")
}

func TestMediaTaskReconciliationKeepsTaskOwnershipAndTerminalIdempotency(t *testing.T) {
	quote := MediaBillingQuote{Command: UsageBillingCommand{RequestID: "media:task-recovery", UserID: 71, APIKeyID: 81, BalanceCost: .25}}
	reservation := &MediaBillingReservation{ID: quote.Command.RequestID, TaskID: "task-recovery", Quote: quote}
	repo := &mediaRecoveryBillingRepo{
		mediaOutcomeRepository: &mediaOutcomeRepository{},
		reservation:            reservation,
		ownerID:                71,
		keyID:                  81,
	}
	svc := &OpenAIGatewayService{usageBillingRepo: repo}
	body := []byte(`{"id":"task-recovery","status":"completed","duration":1}`)

	handled, err := svc.ReconcileMediaTask(context.Background(), "task-recovery", 72, 81, body)
	require.NoError(t, err)
	require.False(t, handled, "a different user cannot reconcile this task")
	handled, err = svc.ReconcileMediaTask(context.Background(), "task-recovery", 71, 82, body)
	require.NoError(t, err)
	require.False(t, handled, "a different API key cannot reconcile this task")
	for range 2 {
		handled, err = svc.ReconcileMediaTask(context.Background(), "task-recovery", 71, 81, body)
		require.NoError(t, err)
		require.True(t, handled)
	}
	require.Equal(t, "settled", repo.state)
	require.Equal(t, 1, repo.settleCalls)
	require.Zero(t, repo.refundCalls)
	require.NotNil(t, repo.staged)
	require.True(t, json.Valid(body))
}

func TestMediaReconcileBoundsMissingAccountTimestampAndTaskHolds(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		createdAt                   time.Time
		missingAccount, missingTask bool
	}{
		{name: "deleted account", createdAt: time.Now().Add(-time.Hour), missingAccount: true},
		{name: "missing quote timestamp"},
		{name: "lost task binding", createdAt: time.Now().Add(-48 * time.Hour), missingTask: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &mediaRecoveryUpstream{responses: []*http.Response{
				mediaRecoveryResponse(503, "application/json", `{"error":"status unavailable"}`),
				mediaRecoveryResponse(503, "application/json", `{"error":"content unavailable"}`),
			}}
			repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
			svc, r := newMediaRecoveryService(upstream, repo, tc.createdAt)
			if tc.missingAccount {
				svc.accountRepo = mediaRecoveryAccountRepo{}
			}
			if tc.missingTask {
				r.TaskID = ""
			}
			require.NoError(t, svc.reconcilePendingMedia(context.Background(), r))
			require.Equal(t, "refunded", repo.state)
		})
	}
}

func TestMediaReconcileStaleNonterminalAndMalformedStatusCannotHoldForever(t *testing.T) {
	for _, status := range []string{`{"status":"processing"}`, `{"status":"unrecognized"}`, ``} {
		repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
		upstream := &mediaRecoveryUpstream{responses: []*http.Response{
			mediaRecoveryResponse(200, "application/json", status),
			mediaRecoveryResponse(503, "application/json", `{"error":"no content"}`),
		}}
		svc, r := newMediaRecoveryService(upstream, repo, time.Now().Add(-48*time.Hour))
		require.NoError(t, svc.reconcilePendingMedia(context.Background(), r))
		require.Equal(t, "refunded", repo.state)
	}
}

func TestMediaReconcileLegacyDeliveryHistoryCannotBeAssumedUndelivered(t *testing.T) {
	for _, missingAccount := range []bool{false, true} {
		upstream := &mediaRecoveryUpstream{responses: []*http.Response{
			mediaRecoveryResponse(503, "application/json", `{"error":"status unavailable"}`),
			mediaRecoveryResponse(503, "application/json", `{"error":"content unavailable"}`),
		}}
		repo := &mediaRecoveryBillingRepo{mediaOutcomeRepository: &mediaOutcomeRepository{}}
		svc, r := newMediaRecoveryService(upstream, repo, time.Now().Add(-48*time.Hour))
		_ = json.Unmarshal([]byte(`{"content_delivery_guarded":false}`), r)
		if missingAccount {
			svc.accountRepo = mediaRecoveryAccountRepo{}
		}
		require.NoError(t, svc.reconcilePendingMedia(context.Background(), r))
		require.Equal(t, "settled", repo.state)
		require.Zero(t, repo.refundCalls)
		require.InDelta(t, .25, repo.settled.BalanceCost, 1e-8)
	}
}
