//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchBillingUpstream struct {
	service.HTTPUpstream
	statuses []int
	body     string
	err      error
	calls    int
}

type workbenchBillingAccountRepo struct {
	openAIImagesFailoverAccountRepo
}

func (workbenchBillingAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	return nil
}

func (u *workbenchBillingUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	if u.err != nil {
		return nil, u.err
	}
	status := u.statuses[min(u.calls-1, len(u.statuses)-1)]
	body := `{"error":{"type":"server_error","message":"provider failed"}}`
	if status == http.StatusOK {
		body = `{"data":[{"url":"https://images.example/result.png"}],"usage":{"input_tokens":7,"output_tokens":2}}`
	}
	if u.body != "" {
		body = u.body
	}
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"provider-reused-id"}},
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

func newWorkbenchBillingHandler(t *testing.T, platform string, upstream *workbenchBillingUpstream) (*OpenAIGatewayHandler, *partialUsageBillingRepo, *service.APIKey) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	billingCfg := *cfg
	billingCfg.RunMode = config.RunModeStandard
	accounts := make([]service.Account, 2)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(i + 1), Name: "billing-test", Platform: platform, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Priority: i,
			Credentials: map[string]any{"api_key": "test-only-key", "base_url": "https://api.example.com"},
		}
	}
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 16)}
	ledger := &partialUsageBillingRepo{}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gw := service.NewOpenAIGatewayService(
		workbenchBillingAccountRepo{openAIImagesFailoverAccountRepo{accounts: accounts}}, usage, ledger, nil, nil, nil, nil, &billingCfg, nil, nil,
		service.NewBillingService(&billingCfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(gw, service.NewConcurrencyService(nil), billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	groupID := int64(2)
	price := 0.25
	key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID,
		Group: &service.Group{ID: groupID, Platform: platform, AllowImageGeneration: true, ImageWorkbenchEnabled: true,
			RateMultiplier: 2, ImagePrice1K: &price},
		User: &service.User{ID: 7, Status: service.StatusActive, Balance: 100},
	}
	if platform == service.PlatformCustom {
		key.Group.Platform = service.PlatformComposite
	}
	return h, ledger, key
}

func workbenchBillingContext(key *service.APIKey, platform, taskID string) (*gin.Context, *httptest.ResponseRecorder) {
	model := "gpt-image-2"
	if platform == service.PlatformGrok {
		model = "grok-imagine-image"
	}
	body := fmt.Sprintf(`{"model":%q,"prompt":"test","size":"1024x1024","n":1}`, model)
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-reused-id")
	if platform == service.PlatformCustom {
		ctx = service.WithCompositeRouteDecision(ctx, service.CompositeRouteDecision{
			Matched: true, GroupID: key.Group.ID, PublicModel: model,
			TargetPlatform: service.PlatformCustom, UpstreamModel: model,
		})
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
	if taskID != "" {
		c.Set(imageWorkbenchTaskIDContextKey, taskID)
	}
	return c, rec
}

func TestImageWorkbenchPrechargesAndRefundsFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformCustom} {
		for _, scenario := range []struct {
			name     string
			statuses []int
			err      error
		}{
			{name: "success", statuses: []int{200}},
			{name: "rejected upstream", statuses: []int{400}},
			{name: "all failover attempts fail", statuses: []int{500}},
			{name: "failover then success", statuses: []int{500, 200}},
			{name: "upstream timeout", err: context.DeadlineExceeded},
		} {
			t.Run(platform+"/"+scenario.name, func(t *testing.T) {
				upstream := &workbenchBillingUpstream{statuses: scenario.statuses, err: scenario.err}
				h, ledger, key := newWorkbenchBillingHandler(t, platform, upstream)
				c, rec := workbenchBillingContext(key, platform, "imgtask_independent_1")
				if platform == service.PlatformGrok {
					h.GrokImages(c)
				} else {
					h.Images(c)
				}
				require.Positive(t, upstream.calls, rec.Body.String())
				if scenario.name == "success" || scenario.name == "failover then success" {
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				} else {
					require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
				}
				if scenario.name == "failover then success" {
					require.Equal(t, 2, upstream.calls)
				}
				if scenario.name != "success" && scenario.name != "failover then success" {
					require.Empty(t, ledger.commands)
					require.Equal(t, 1, ledger.refunded)
					require.Empty(t, ledger.reservations)
					return
				}
				require.Len(t, ledger.commands, 1, rec.Body.String())
				require.True(t, strings.HasPrefix(ledger.commands[0].RequestID, "media:"))
				require.InDelta(t, 0.5, ledger.commands[0].BalanceCost, 1e-9)
			})
		}
	}
}

func TestImageWorkbenchBillingUsesIndependentTaskIDsAndSkipsUnexecutedTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &workbenchBillingUpstream{statuses: []int{200}}
	h, ledger, key := newWorkbenchBillingHandler(t, service.PlatformOpenAI, upstream)
	for i := range 3 {
		c, _ := workbenchBillingContext(key, service.PlatformOpenAI, fmt.Sprintf("imgtask_%d", i))
		h.Images(c)
	}
	require.Len(t, ledger.commands, 3)
	ids := map[string]bool{}
	for _, cmd := range ledger.commands {
		require.False(t, ids[cmd.RequestID])
		ids[cmd.RequestID] = true
		require.True(t, strings.HasPrefix(cmd.RequestID, "media:"))
		require.InDelta(t, 0.5, cmd.BalanceCost, 1e-9)
	}
	key.Group.AllowImageGeneration = false
	c, _ := workbenchBillingContext(key, service.PlatformOpenAI, "imgtask_blocked")
	h.Images(c)
	require.Len(t, ledger.commands, 3)
	require.Equal(t, 3, upstream.calls)
	key.Group.AllowImageGeneration = true
	upstream.err = errors.New("network failed")
	c, _ = workbenchBillingContext(key, service.PlatformOpenAI, "")
	c.Request.Header.Set(imageWorkbenchHeader, "true")
	h.Images(c)
	require.Len(t, ledger.commands, 3, "a client header cannot forge trusted task billing context")
}

func TestImageWorkbenchBillingRetainsUsageFromFailedResponses(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformCustom} {
		for _, status := range []int{400, 500} {
			t.Run(fmt.Sprintf("%s/%d", platform, status), func(t *testing.T) {
				upstream := &workbenchBillingUpstream{
					statuses: []int{status},
					body:     `{"error":{"type":"server_error","message":"failed"},"usage":{"input_tokens":7,"output_tokens":2}}`,
				}
				h, ledger, key := newWorkbenchBillingHandler(t, platform, upstream)
				c, _ := workbenchBillingContext(key, platform, "imgtask_usage_failure")
				if platform == service.PlatformGrok {
					h.GrokImages(c)
				} else {
					h.Images(c)
				}
				require.Empty(t, ledger.commands)
				require.Equal(t, 1, ledger.refunded)
			})
		}
	}
}

func TestImageWorkbenchBillingDoesNotChargeUnrequestedExtraImages(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok, service.PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			upstream := &workbenchBillingUpstream{
				statuses: []int{http.StatusOK},
				body:     `{"data":[{"url":"https://images.example/1.png"},{"url":"https://images.example/2.png"},{"url":"https://images.example/3.png"}]}`,
			}
			h, ledger, key := newWorkbenchBillingHandler(t, platform, upstream)
			c, rec := workbenchBillingContext(key, platform, "imgtask_one_image")
			if platform == service.PlatformGrok {
				h.GrokImages(c)
			} else {
				h.Images(c)
			}
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, ledger.commands, 1)
			require.Equal(t, 1, ledger.commands[0].ImageCount)
			require.InDelta(t, 0.5, ledger.commands[0].BalanceCost, 1e-9)
		})
	}
}
