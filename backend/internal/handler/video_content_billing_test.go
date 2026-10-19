//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoContentHandlerLedger struct {
	*partialUsageBillingRepo
	reservation  *service.MediaBillingReservation
	state        string
	final        *service.MediaBillingQuote
	settleErr    error
	settlements  int
	beforeSettle func()
}

func (r *videoContentHandlerLedger) ReserveMedia(_ context.Context, reservation *service.MediaBillingReservation) error {
	r.reservation = reservation
	return nil
}
func (r *videoContentHandlerLedger) BindMediaTask(_ context.Context, id, task string, endpoint service.GrokMediaEndpoint) error {
	r.reservation.TaskID = task
	r.reservation.Endpoint = endpoint
	return nil
}
func (r *videoContentHandlerLedger) FindMediaTask(_ context.Context, task string, user, key int64) (*service.MediaBillingReservation, error) {
	if r.reservation == nil || task != r.reservation.TaskID || user != r.reservation.Quote.Command.UserID || key != r.reservation.Quote.Command.APIKeyID {
		return nil, nil
	}
	return r.reservation, nil
}
func (r *videoContentHandlerLedger) StageMediaOutcome(_ context.Context, _ string, q *service.MediaBillingQuote) error {
	if r.state == "refunded" || r.state == "settled" {
		return nil
	}
	if q != nil {
		r.final = q
	} else if r.final == nil {
		r.state = "refund pending"
	}
	return nil
}
func (r *videoContentHandlerLedger) SettleMedia(context.Context, string, *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if r.beforeSettle != nil {
		r.beforeSettle()
	}
	if r.state == "refunded" || r.state == "refund pending" {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if r.settleErr != nil {
		return nil, r.settleErr
	}
	if r.state != "settled" {
		r.settlements++
	}
	r.state = "settled"
	return &service.UsageBillingApplyResult{Applied: true}, nil
}
func (r *videoContentHandlerLedger) RefundMedia(context.Context, string) (*service.MediaBillingReservation, error) {
	if r.state == "settled" {
		return nil, nil
	}
	if r.final != nil {
		return nil, service.ErrUsageBillingRequestConflict
	}
	r.state = "refunded"
	return r.reservation, nil
}

type videoContentHandlerUpstream struct {
	service.HTTPUpstream
	responses []*http.Response
	calls     int
}

func (u *videoContentHandlerUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	if len(u.responses) == 0 {
		return nil, io.EOF
	}
	r := u.responses[0]
	u.responses = u.responses[1:]
	return r, nil
}
func videoContentHandlerResponse(contentType, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func newVideoContentBillingHandler(t *testing.T, upstream *videoContentHandlerUpstream) (*OpenAIGatewayHandler, *videoContentHandlerLedger, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	billingCfg := *cfg
	billingCfg.RunMode = config.RunModeStandard
	account := service.Account{ID: 1, Name: "video", Platform: service.PlatformCustom, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{2}, Credentials: map[string]any{"api_key": "test-only-key", "base_url": "https://api.example.com", "media_protocol": "sora"}}
	ledger := &videoContentHandlerLedger{partialUsageBillingRepo: &partialUsageBillingRepo{}}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gw := service.NewOpenAIGatewayService(workbenchBillingAccountRepo{openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}, &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 16)}, ledger, nil, nil, nil, &openAIHTTPResponseOwnerErrorCache{}, &billingCfg, nil, nil, service.NewBillingService(&billingCfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	t.Cleanup(gw.CloseOpenAIWSPool)
	h := NewOpenAIGatewayHandler(gw, service.NewConcurrencyService(nil), billingCache, service.NewAPIKeyService(workbenchBillingAPIKeyRepo{}, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	groupID := int64(2)
	price := .25
	key := &service.APIKey{ID: 81, UserID: 71, GroupID: &groupID, Group: &service.Group{ID: 2, Platform: service.PlatformComposite, RateMultiplier: 1, VideoPrice480P: &price}, User: &service.User{ID: 71, Status: service.StatusActive, Balance: 100}}
	ledger.reservation = &service.MediaBillingReservation{ID: "media:video", TaskID: "video-owned", GroupID: 2, Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: "media:video", UserID: 71, APIKeyID: 81, AccountID: 1, BalanceCost: .25}, Usage: service.UsageLog{CreatedAt: time.Now()}}}
	return h, ledger, key
}

func videoContentOwnerContext(key *service.APIKey, method, task string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, "/v1/videos/"+task+"/content", nil)
	c.Params = gin.Params{{Key: "request_id", Value: task}}
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
	return c, rec
}

func TestVideoContentHandlerSettlesBeforeFirstByteAndPreservesOwnership(t *testing.T) {
	for _, tc := range []struct {
		name       string
		user, key  int64
		wantStatus int
	}{
		{"owner", 71, 81, 200}, {"different user", 72, 81, 404}, {"different key", 71, 82, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &videoContentHandlerUpstream{responses: []*http.Response{videoContentHandlerResponse("video/mp4", "delivered-video")}}
			h, ledger, key := newVideoContentBillingHandler(t, upstream)
			key.UserID, key.ID = tc.user, tc.key
			c, rec := videoContentOwnerContext(key, http.MethodGet, "video-owned")
			ledger.beforeSettle = func() { require.Empty(t, rec.Body.String(), "billing must be final before client bytes") }
			h.CustomVideoContent(c)
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantStatus == 200 {
				require.Equal(t, "delivered-video", rec.Body.String())
				require.Equal(t, 1, ledger.settlements)
			} else {
				require.Zero(t, upstream.calls)
				require.Zero(t, ledger.settlements)
			}
		})
	}
}

func TestVideoContentHandlerRejectsRefundedTaskBeforeReleasingBytes(t *testing.T) {
	upstream := &videoContentHandlerUpstream{responses: []*http.Response{videoContentHandlerResponse("video/mp4", "must-not-leak")}}
	h, ledger, key := newVideoContentBillingHandler(t, upstream)
	ledger.state = "refunded"
	c, rec := videoContentOwnerContext(key, http.MethodGet, "video-owned")
	h.CustomVideoContent(c)
	require.GreaterOrEqual(t, rec.Code, 400)
	require.NotContains(t, rec.Body.String(), "must-not-leak")
	require.Zero(t, ledger.settlements)
}

func TestVideoCompletedCreationDoesNotPublishMetadataBeforeSettlement(t *testing.T) {
	upstream := &videoContentHandlerUpstream{responses: []*http.Response{videoContentHandlerResponse("application/json", `{"id":"video-owned","status":"completed","video":{"url":"https://cdn.example/paid.mp4"},"duration":4}`)}}
	h, ledger, key := newVideoContentBillingHandler(t, upstream)
	ledger.reservation = nil
	ledger.settleErr = errors.New("ledger unavailable")
	c, rec := videoContentOwnerContext(key, http.MethodPost, "video-owned")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"grok-imagine-video","prompt":"test","duration":4,"resolution":"480p"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CustomVideoGeneration(c)
	require.Equal(t, 1, upstream.calls)
	require.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "https://cdn.example/paid.mp4")
	require.NotNil(t, ledger.final, "the success quote survives a ledger outage")
	require.NotEqual(t, "refunded", ledger.state)
}
