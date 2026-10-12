//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func (r *partialUsageBillingRepo) ReserveMedia(_ context.Context, reservation *service.MediaBillingReservation) error {
	if r.reservations == nil {
		r.reservations = map[string]*service.MediaBillingReservation{}
	}
	r.reservations[reservation.ID] = reservation
	return nil
}
func (r *partialUsageBillingRepo) StageMediaOutcome(context.Context, string, *service.MediaBillingQuote) error {
	return nil
}
func (r *partialUsageBillingRepo) SettleMedia(ctx context.Context, id string, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	delete(r.reservations, id)
	return r.Apply(ctx, cmd)
}
func (r *partialUsageBillingRepo) RefundMedia(_ context.Context, id string) (*service.MediaBillingReservation, error) {
	reservation := r.reservations[id]
	delete(r.reservations, id)
	if reservation != nil {
		r.refunded++
	}
	return reservation, nil
}
func (r *partialUsageBillingRepo) BindMediaTask(context.Context, string, string, service.GrokMediaEndpoint) error {
	return r.bindMediaErr
}
func (r *partialUsageBillingRepo) FindMediaTask(context.Context, string, int64, int64) (*service.MediaBillingReservation, error) {
	return nil, nil
}
func (r *partialUsageBillingRepo) PendingMedia(context.Context, int) ([]*service.MediaBillingReservation, error) {
	return nil, nil
}

func TestVideoPrechargeRetainsAcceptedWorkWhenPersistenceFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name    string
		status  int
		body    string
		bindErr error
		refund  bool
	}{
		{name: "task binding fails", status: http.StatusOK, body: `{"id":"video_accepted","status":"queued"}`, bindErr: errors.New("database unavailable")},
		{name: "accepted without task id", status: http.StatusOK, body: `{"status":"queued"}`},
		{name: "explicit failure without task id", status: http.StatusOK, body: `{"status":"failed"}`, refund: true},
		{name: "upstream rejects task", status: http.StatusBadRequest, body: `{"error":{"message":"invalid input"}}`, refund: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upstream := &workbenchBillingUpstream{statuses: []int{scenario.status}, body: scenario.body}
			h, ledger, key := newWorkbenchBillingHandler(t, service.PlatformCustom, upstream)
			ledger.bindMediaErr = scenario.bindErr
			price := 0.01
			key.Group.VideoPrice480P = &price
			c, rec := workbenchBillingContext(key, service.PlatformCustom, "")
			ctx := service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
				Matched: true, GroupID: key.Group.ID, PublicModel: "grok-imagine-video",
				TargetPlatform: service.PlatformCustom, UpstreamModel: "grok-imagine-video",
			})
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"grok-imagine-video","prompt":"test","duration":4,"resolution":"480p"}`)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			h.CustomVideoGeneration(c)
			require.Equal(t, 1, upstream.calls, rec.Body.String())
			require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
			require.Empty(t, ledger.commands)
			if scenario.refund {
				require.Equal(t, 1, ledger.refunded)
				require.Empty(t, ledger.reservations)
			} else {
				require.Zero(t, ledger.refunded)
				require.Len(t, ledger.reservations, 1)
			}
		})
	}
}
