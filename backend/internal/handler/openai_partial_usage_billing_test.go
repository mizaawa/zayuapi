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
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type partialUsageUpstream struct {
	service.HTTPUpstream
	payload string
	calls   int
}

func (u *partialUsageUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"upstream-partial"}},
		Body:       io.NopCloser(strings.NewReader(u.payload)),
	}, nil
}

type partialUsageBillingRepo struct {
	service.UsageBillingRepository
	commands []*service.UsageBillingCommand
}

func (r *partialUsageBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands = append(r.commands, cmd)
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestOpenAIHandlersBillCommittedPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"responses", "responses_passthrough", "chat", "messages", "responses_priority", "responses_passthrough_priority", "chat_priority", "messages_priority"} {
		baseProtocol := strings.TrimSuffix(protocol, "_priority")
		priority := strings.HasSuffix(protocol, "_priority")
		for _, beforeOutput := range []bool{false, true} {
			name := protocol + "/after_output"
			if beforeOutput {
				name = protocol + "/before_output"
			}
			t.Run(name, func(t *testing.T) {
				failed := `data: {"type":"response.failed","response":{"id":"resp_partial","model":"gpt-5.1","status":"failed","error":{"type":"server_error","code":"server_error","message":"upstream processing failed"},"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}}` + "\n\n"
				payload := failed
				if !beforeOutput {
					payload = `data: {"type":"response.created","response":{"id":"resp_partial","model":"gpt-5.1","status":"in_progress","output":[]}}` + "\n\n" +
						`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}` + "\n\n" + failed
				}
				upstream := &partialUsageUpstream{payload: payload}
				usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
				billingRepo := &partialUsageBillingRepo{}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				cfg.Gateway.MaxLineSize = 1024 * 1024
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				account := service.Account{
					ID: 1, Name: "partial", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "test-token", "base_url": "https://api.openai.com"},
					Extra:       map[string]any{"openai_passthrough": baseProtocol == "responses_passthrough"},
				}
				billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				// Eligibility uses simple mode; RecordUsage runs standard billing against the receipt stub.
				billingCfg := *cfg
				billingCfg.RunMode = config.RunModeStandard
				gateway := service.NewOpenAIGatewayService(
					openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}, usageRepo, billingRepo,
					nil, nil, nil, nil, &billingCfg, nil, nil, service.NewBillingService(&billingCfg, nil),
					nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
				)
				pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
					WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
				})
				pool.Stop()
				h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billingCache,
					service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), pool, nil, nil, nil, cfg)
				groupID := int64(2)
				key := &service.APIKey{ID: 99, GroupID: &groupID, FailoverActive: true,
					Group: &service.Group{ID: 2, Platform: service.PlatformOpenAI, RateMultiplier: 4, AllowMessagesDispatch: true},
					User:  &service.User{ID: 100, Status: service.StatusActive, Balance: 100},
				}
				path, body := "/v1/responses", `{"model":"gpt-5.1","stream":true,"input":"hello"}`
				switch baseProtocol {
				case "chat":
					path, body = "/v1/chat/completions", `{"model":"gpt-5.1","stream":true,"messages":[{"role":"user","content":"hello"}]}`
				case "messages":
					path, body = "/v1/messages", `{"model":"gpt-5.1","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`
				}
				if priority && baseProtocol != "messages" {
					body = strings.TrimSuffix(body, "}") + `,"service_tier":"priority"}`
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				ctx := service.WithAPIKeyFailoverAttempt(context.WithValue(t.Context(), ctxkey.ClientRequestID, "partial-request"))
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				if priority && baseProtocol == "messages" {
					c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)
				}
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})
				switch baseProtocol {
				case "chat":
					h.ChatCompletions(c)
				case "messages":
					h.Messages(c)
				default:
					h.Responses(c)
				}
				require.Equal(t, 1, upstream.calls, recorder.Body.String())
				if beforeOutput {
					require.Empty(t, billingRepo.commands)
					require.Empty(t, usageRepo.created)
					return
				}
				require.Contains(t, recorder.Body.String(), "partial")
				require.Len(t, billingRepo.commands, 1)
				cmd := billingRepo.commands[0]
				require.Equal(t, "client:partial-request", cmd.RequestID)
				require.Equal(t, service.HashUsageRequestPayload([]byte(body)), cmd.RequestPayloadHash)
				require.Positive(t, cmd.BalanceCost)
				select {
				case usage := <-usageRepo.created:
					require.Equal(t, 7, usage.InputTokens)
					require.Equal(t, 2, usage.OutputTokens)
					require.Equal(t, int64(2), *usage.GroupID)
					require.Equal(t, float64(4), usage.RateMultiplier)
					require.Equal(t, usage.ActualCost, cmd.BalanceCost)
					if priority {
						require.NotNil(t, usage.ServiceTier)
						require.Equal(t, "priority", *usage.ServiceTier)
						want, err := service.NewBillingService(&billingCfg, nil).CalculateCostWithServiceTier(
							"gpt-5.1", service.UsageTokens{InputTokens: 7, OutputTokens: 2}, 4, "priority")
						require.NoError(t, err)
						require.Equal(t, service.QuantizeUsageBillingAmount(want.ActualCost), cmd.BalanceCost)
					}
				default:
					t.Fatal("missing partial usage log")
				}
			})
		}
	}
}

func TestOpenAIForwardPartialUsageGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	result := &service.OpenAIForwardResult{Stream: true, Usage: service.OpenAIUsage{InputTokens: 7}}
	err := errors.New("stream failed")
	require.False(t, openAIForwardHasBillablePartialUsage(c, result, err, c.Writer.Size()))
	_, writeErr := c.Writer.WriteString("data: partial\n\n")
	require.NoError(t, writeErr)
	require.True(t, openAIForwardHasBillablePartialUsage(c, result, err, -1))
	require.False(t, openAIForwardHasBillablePartialUsage(c, result, &service.UpstreamFailoverError{}, -1))
	service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Message: "blocked"})
	require.False(t, openAIForwardHasBillablePartialUsage(c, result, err, -1))
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.JSON(http.StatusBadGateway, gin.H{"error": "buffered failure"})
	require.False(t, openAIForwardHasBillablePartialUsage(c, result, err, -1))
}
