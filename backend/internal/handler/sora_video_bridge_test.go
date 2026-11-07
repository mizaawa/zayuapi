//go:build unit

package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSoraBridgePrechargeRetainsAcceptedTasksAndRejectsInvalidInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, response, request string
		upstreamStatus          int
		bindErr                 error
		calls, refunded         int
	}{
		{name: "accepted without id", response: `{"status":"queued"}`, upstreamStatus: 200, calls: 1},
		{name: "binding unavailable", response: `{"id":"accepted-task","status":"new-provider-state"}`, upstreamStatus: 200, calls: 1, bindErr: errors.New("database unavailable")},
		{name: "failed without id", response: `{"status":"failed"}`, upstreamStatus: 200, calls: 1, refunded: 1},
		{name: "upstream rejected", response: `{"error":{"message":"invalid input"}}`, upstreamStatus: 400, calls: 1, refunded: 1},
		{name: "invalid input", request: `{"model":"grok-imagine-video","prompt":"waves","seconds":0,"size":"854x480"}`, calls: 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			upstream := &workbenchBillingUpstream{statuses: []int{scenario.upstreamStatus}, body: scenario.response}
			h, ledger, key := newWorkbenchBillingHandler(t, service.PlatformCustom, upstream)
			ledger.bindMediaErr = scenario.bindErr
			price := 0.01
			key.Group.VideoPrice480P = &price
			c, rec := workbenchBillingContext(key, service.PlatformCustom, "")
			ctx := service.WithSoraVideoBridge(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
				Matched: true, GroupID: key.Group.ID, PublicModel: "grok-imagine-video",
				TargetPlatform: service.PlatformCustom, UpstreamModel: "grok-imagine-video",
			}))
			body := scenario.request
			if body == "" {
				body = `{"model":"grok-imagine-video","prompt":"waves","seconds":"4","size":"854x480"}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/sora/videos", strings.NewReader(body)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			h.CustomVideoGeneration(c)
			require.Equal(t, scenario.calls, upstream.calls, rec.Body.String())
			require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
			require.Equal(t, scenario.refunded, ledger.refunded)
			if scenario.calls == 0 {
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Empty(t, ledger.reservations)
			} else if scenario.refunded == 0 {
				require.Len(t, ledger.reservations, 1)
			} else {
				require.Empty(t, ledger.reservations)
			}
			require.Empty(t, ledger.commands)
		})
	}
}
