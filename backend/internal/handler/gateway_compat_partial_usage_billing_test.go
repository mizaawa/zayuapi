//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compatPartialUsageUpstream struct{ partialUsageUpstream }

func (u *compatPartialUsageUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type compatPartialUsageAccountRepo struct {
	openAIImagesFailoverAccountRepo
}

func (r compatPartialUsageAccountRepo) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return r.accounts, nil
}

func (r compatPartialUsageAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]service.Account, error) {
	var accounts []service.Account
	for _, platform := range platforms {
		accounts = append(accounts, r.accountsForPlatform(platform)...)
	}
	return accounts, nil
}

func TestGatewayCompatHandlersBillOnlyCommittedPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "chat"} {
		for _, stream := range []bool{false, true} {
			for _, partial := range []bool{false, true} {
				t.Run(protocol+"/"+map[bool]string{false: "buffered", true: "stream"}[stream]+"/"+map[bool]string{false: "before_output", true: "partial"}[partial], func(t *testing.T) {
					failure := "event: error\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"Upstream overloaded"}}` + "\n\n"
					payload := failure
					if partial {
						payload = "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":11,"cache_read_input_tokens":3}}}` + "\n\n" +
							"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}` + "\n\n" +
							"event: message_delta\ndata: " + `{"type":"message_delta","delta":{},"usage":{"output_tokens":2}}` + "\n\n" + failure
					}
					upstream := &compatPartialUsageUpstream{partialUsageUpstream: partialUsageUpstream{payload: payload}}
					usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
					billingRepo := &partialUsageBillingRepo{}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					cfg.Default.RateMultiplier = 1
					cfg.Security.URLAllowlist.AllowInsecureHTTP = true
					billingCfg := *cfg
					billingCfg.RunMode = config.RunModeStandard
					group := &service.Group{ID: 2, Status: service.StatusActive, Platform: service.PlatformAnthropic, RateMultiplier: 4}
					account := service.Account{
						ID: 1, Name: "partial", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, Concurrency: 2,
						Credentials: map[string]any{"api_key": "test-token", "base_url": "https://api.anthropic.com"},
					}
					billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					t.Cleanup(billingCache.Stop)
					gateway := service.NewGatewayService(
						compatPartialUsageAccountRepo{openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}},
						&fakeGroupRepo{group: group}, usageRepo, billingRepo, nil, nil, nil, nil, &billingCfg, nil, nil,
						service.NewBillingService(&billingCfg, nil), nil, billingCache, nil, upstream, &service.DeferredService{},
						nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					)
					pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
						WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
					})
					pool.Stop()
					h := NewGatewayHandler(gateway, nil, nil, nil, nil, service.NewConcurrencyService(nil), billingCache, nil,
						service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), pool, nil, nil, nil, cfg, nil)
					groupID := int64(2)
					key := &service.APIKey{ID: 99, GroupID: &groupID, Group: group, FailoverActive: true,
						User: &service.User{ID: 100, Status: service.StatusActive, Balance: 100}}
					path, body := "/v1/responses", `{"model":"claude-sonnet-4.5","stream":STREAM,"input":"hello"}`
					if protocol == "chat" {
						path, body = "/v1/chat/completions", `{"model":"claude-sonnet-4.5","stream":STREAM,"messages":[{"role":"user","content":"hello"}]}`
					}
					body = strings.Replace(body, "STREAM", map[bool]string{false: "false", true: "true"}[stream], 1)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					ctx := service.WithAPIKeyFailoverAttempt(context.WithValue(t.Context(), ctxkey.ClientRequestID, "compat-partial-request"))
					c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})
					if protocol == "chat" {
						h.ChatCompletions(c)
					} else {
						h.Responses(c)
					}
					require.Equal(t, 1, upstream.calls, recorder.Body.String())
					require.True(t, service.APIKeyFailoverAttemptFailed(ctx))
					require.NotContains(t, recorder.Body.String(), "response.completed")
					if !stream || !partial {
						require.Empty(t, billingRepo.commands)
						require.Empty(t, usageRepo.created)
						require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
						return
					}
					require.Len(t, billingRepo.commands, 1)
					cmd := billingRepo.commands[0]
					require.Equal(t, "client:compat-partial-request", cmd.RequestID)
					require.Equal(t, service.HashUsageRequestPayload([]byte(body)), cmd.RequestPayloadHash)
					require.Positive(t, cmd.BalanceCost)
					select {
					case usage := <-usageRepo.created:
						require.Equal(t, 11, usage.InputTokens)
						require.Equal(t, 2, usage.OutputTokens)
						require.Equal(t, 3, usage.CacheReadTokens)
						require.Equal(t, int64(2), *usage.GroupID)
						require.Equal(t, float64(4), usage.RateMultiplier)
						require.Equal(t, usage.ActualCost, cmd.BalanceCost)
					default:
						t.Fatal("missing partial usage log")
					}
					if protocol == "responses" {
						require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
					} else {
						require.Equal(t, 1, strings.Count(recorder.Body.String(), `"error":`))
					}
				})
			}
		}
	}
}
