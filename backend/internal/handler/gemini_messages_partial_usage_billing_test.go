//go:build unit

package handler

import (
	"context"
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

type geminiPartialUsageReader struct{ io.Reader }

func (r geminiPartialUsageReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

type geminiPartialUsageUpstream struct {
	service.HTTPUpstream
	payload string
	calls   int
}

func (u *geminiPartialUsageUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(geminiPartialUsageReader{strings.NewReader(u.payload)}),
	}, nil
}

func TestGeminiMessagesBillsMeteredPartialStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var baseline float64
	for _, wire := range []string{"messages", "chat/completions", "responses", "native"} {
		for _, metered := range []bool{false, true} {
			t.Run(wire+"/"+map[bool]string{false: "no_usage", true: "metered_partial"}[metered], func(t *testing.T) {
				payload := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}\n\n"
				if metered {
					payload += "data: {\"usageMetadata\":{\"promptTokenCount\":200012,\"cachedContentTokenCount\":7,\"toolUsePromptTokenCount\":2}}\n\n"
					payload += "data: {\"usageMetadata\":{\"candidatesTokenCount\":7,\"thoughtsTokenCount\":3}}\n\n"
				}
				upstream := &geminiPartialUsageUpstream{payload: payload}
				usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
				billingRepo := &partialUsageBillingRepo{}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				billingCfg := *cfg
				billingCfg.RunMode = config.RunModeStandard
				group := &service.Group{ID: 2, Status: service.StatusActive, Platform: service.PlatformGemini, RateMultiplier: 4}
				account := service.Account{
					ID: 1, Name: "gemini-partial", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 2,
					Credentials: map[string]any{"api_key": "test-token"},
				}
				accounts := compatPartialUsageAccountRepo{openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}
				groups := &fakeGroupRepo{group: group}
				billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				gateway := service.NewGatewayService(
					accounts, groups, usageRepo, billingRepo, nil, nil, nil, nil, &billingCfg, nil, nil,
					service.NewBillingService(&billingCfg, nil), nil, billingCache, nil, upstream, &service.DeferredService{},
					nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				)
				gemini := service.NewGeminiMessagesCompatService(accounts, groups, nil, nil, nil, nil, upstream, nil, cfg, nil)
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
				})
				pool.Stop()
				h := NewGatewayHandler(gateway, nil, gemini, nil, nil, service.NewConcurrencyService(nil), billingCache, nil,
					service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), pool, nil, nil, nil, cfg, nil)
				groupID := int64(2)
				key := &service.APIKey{ID: 99, GroupID: &groupID, Group: group, FailoverActive: true,
					User: &service.User{ID: 100, Status: service.StatusActive, Balance: 100}}
				body := `{"model":"gemini-3.6-flash","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
				path := "/v1/" + wire
				if wire == "responses" {
					body = `{"model":"gemini-3.6-flash","stream":true,"input":"hi"}`
				} else if wire == "native" {
					path = "/v1beta/models/gemini-3.6-flash:streamGenerateContent"
					body = `{"contents":[{"parts":[{"text":"hi"}]}]}`
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				ctx := context.WithValue(t.Context(), ctxkey.ClientRequestID, "gemini-partial-request")
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})
				switch wire {
				case "native":
					c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-3.6-flash:streamGenerateContent"}}
					h.GeminiV1BetaModels(c)
				case "responses":
					h.Responses(c)
				case "chat/completions":
					h.ChatCompletions(c)
				default:
					h.Messages(c)
				}
				require.Equal(t, 1, upstream.calls, recorder.Body.String())
				if !metered {
					require.Empty(t, billingRepo.commands)
					require.Empty(t, usageRepo.created)
					return
				}
				require.Len(t, billingRepo.commands, 1)
				cmd := billingRepo.commands[0]
				require.Equal(t, "client:gemini-partial-request", cmd.RequestID)
				require.Equal(t, service.HashUsageRequestPayload([]byte(body)), cmd.RequestPayloadHash)
				require.Positive(t, cmd.BalanceCost)
				if baseline == 0 {
					baseline = cmd.BalanceCost
				} else {
					require.Equal(t, baseline, cmd.BalanceCost)
				}
				select {
				case usage := <-usageRepo.created:
					require.Equal(t, 200007, usage.InputTokens)
					require.Equal(t, 7, usage.CacheReadTokens)
					require.Equal(t, 10, usage.OutputTokens)
					require.Equal(t, float64(4), usage.RateMultiplier)
					require.Equal(t, usage.ActualCost, cmd.BalanceCost)
				default:
					t.Fatal("missing partial usage log")
				}
			})
		}
	}
}
