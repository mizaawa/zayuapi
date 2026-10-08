package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatBillingUsesOutboundServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	policies := []struct {
		name       string
		requested  string
		action     string
		billed     string
		multiplier float64
	}{
		{name: "filtered priority", requested: "priority", action: BetaPolicyActionFilter, multiplier: 1},
		{name: "filtered flex", requested: "flex", action: BetaPolicyActionFilter, multiplier: 1},
		{name: "forced priority", requested: "flex", action: OpenAIFastPolicyActionForcePriority, billed: "priority", multiplier: 2},
		{name: "priority passthrough", requested: "priority", action: BetaPolicyActionPass, billed: "priority", multiplier: 2},
		{name: "flex passthrough", requested: "flex", action: BetaPolicyActionPass, billed: "flex", multiplier: 0.5},
	}
	for _, protocol := range []string{"chat", "responses-shaped chat", "messages", "raw chat", "responses via raw chat", "messages via raw chat"} {
		for _, stream := range []bool{false, true} {
			for _, policy := range policies {
				if protocol == "messages" && policy.requested != "priority" {
					continue
				}
				t.Run(fmt.Sprintf("%s/stream=%t/%s", protocol, stream, policy.name), func(t *testing.T) {
					settings := &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
						ServiceTier: policy.requested,
						Action:      policy.action,
						Scope:       BetaPolicyScopeAll,
					}}}
					policyService := newOpenAIGatewayServiceWithSettings(t, settings)
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					userRepo := &openAIRecordUsageUserRepoStub{}
					svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
					svc.settingService = policyService.settingService
					svc.cfg.Default.RateMultiplier = 1
					expectedTier, expectedMultiplier := policy.billed, policy.multiplier
					if protocol == "messages via raw chat" {
						expectedTier, expectedMultiplier = "", 1
					}
					account := &Account{
						ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
						Credentials: map[string]any{"api_key": "test-key"},
					}
					user := &User{ID: 1}
					apiKey := &APIKey{ID: 2, UserID: user.ID, User: user}
					body := fmt.Sprintf(`{"model":"gpt-5.4","service_tier":%q,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, policy.requested, stream)
					switch protocol {
					case "responses-shaped chat", "responses via raw chat":
						body = fmt.Sprintf(`{"model":"gpt-5.4","service_tier":%q,"input":"hello","stream":%t}`, policy.requested, stream)
					case "messages":
						body = fmt.Sprintf(`{"model":"gpt-5.4","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)
					case "messages via raw chat":
						body = fmt.Sprintf(`{"model":"gpt-5.4","max_tokens":64,"service_tier":%q,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, policy.requested, stream)
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set("api_key", apiKey)
					switch protocol {
					case "messages":
						c.Request.URL.Path = "/v1/messages"
						c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)
					case "messages via raw chat":
						c.Request.URL.Path = "/v1/messages"
					case "responses via raw chat":
						c.Request.URL.Path = "/v1/responses"
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"tier-billing-test"}},
						Body: io.NopCloser(strings.NewReader(
							`data: {"type":"response.completed","response":{"id":"resp_tier","model":"gpt-5.4","status":"completed","output":[{"type":"message","id":"msg_tier","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1000,"output_tokens":100,"total_tokens":1100}}}` + "\n\n")),
					}}
					if strings.Contains(protocol, "raw chat") {
						upstream.resp.Header.Set("Content-Type", "application/json")
						payload := `{"id":"chatcmpl_tier","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100}}`
						if stream {
							upstream.resp.Header.Set("Content-Type", "text/event-stream")
							payload = `data: {"id":"chatcmpl_tier","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100}}` + "\n\ndata: [DONE]\n\n"
						}
						upstream.resp.Body = io.NopCloser(strings.NewReader(payload))
					}
					svc.httpUpstream = upstream
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "messages":
						result, err = svc.ForwardAsAnthropic(context.Background(), c, account, []byte(body), "", "")
					case "raw chat":
						result, err = svc.forwardAsRawChatCompletions(context.Background(), c, account, []byte(body), "")
					case "responses via raw chat":
						result, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, []byte(body))
					case "messages via raw chat":
						result, err = svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, []byte(body), "")
					default:
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(body), "", "")
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, http.StatusOK, rec.Code)
					require.Equal(t, expectedTier, gjson.GetBytes(upstream.lastBody, "service_tier").String())
					if expectedTier == "" {
						require.Nil(t, result.ServiceTier)
					} else {
						require.NotNil(t, result.ServiceTier)
						require.Equal(t, expectedTier, *result.ServiceTier)
					}
					require.Equal(t, 1000, result.Usage.InputTokens)
					require.Equal(t, 100, result.Usage.OutputTokens)
					require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
						Result: result, APIKey: apiKey, User: user, Account: account,
					}))
					require.NotNil(t, usageRepo.lastLog)
					require.Equal(t, result.ServiceTier, usageRepo.lastLog.ServiceTier)
					// GPT-5.4 is $2.50/M input and $15/M output before the tier multiplier.
					expectedCost := 0.004 * expectedMultiplier
					require.InDelta(t, expectedCost, usageRepo.lastLog.ActualCost, 1e-10)
					require.InDelta(t, expectedCost, userRepo.lastAmount, 1e-10)
					require.Equal(t, 1, userRepo.deductCalls)
				})
			}
		}
	}
}
