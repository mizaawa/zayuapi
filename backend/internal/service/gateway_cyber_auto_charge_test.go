//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	claudeCyberRefusalSSE = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_cyber\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":412,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\",\"stop_sequence\":null,\"stop_details\":{\"type\":\"refusal\",\"category\":\"cyber\",\"explanation\":\"This request was declined because it could enable cyber harm.\"}},\"usage\":{\"output_tokens\":0}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	claudeCyberFallbackSSE = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fallback\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-8\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":412,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"fallback\",\"from\":{\"model\":\"claude-fable-5\"},\"to\":{\"model\":\"claude-opus-4-8\"},\"trigger\":{\"type\":\"refusal\",\"category\":\"cyber\"}}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null,\"stop_details\":null},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	claudeBioRefusalSSE = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_bio\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":30,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\",\"stop_sequence\":null,\"stop_details\":{\"type\":\"refusal\",\"category\":\"bio\"}},\"usage\":{\"output_tokens\":0}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	claudeTextAboutCyberSSE = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_text\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":30,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"A cyber refusal sets stop_details category cyber; cyber_policy is OpenAI's code.\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":20}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
)

var claudeCyberStreamCases = []struct {
	name string
	sse  string
	want bool
}{
	{"cyber refusal", claudeCyberRefusalSSE, true},
	{"server-side fallback after a cyber refusal", claudeCyberFallbackSSE, true},
	{"bio refusal", claudeBioRefusalSSE, false},
	{"answer that talks about cyber", claudeTextAboutCyberSSE, false},
}

func newClaudeCyberTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	beginUpstreamResponseModelObservation(c)
	return c
}

func claudeSSEResponse(sse string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
}

func TestAnthropicStreamsDetectClaudeCyberRefusal(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range claudeCyberStreamCases {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, tc.name), func(t *testing.T) {
				c := newClaudeCyberTestContext()
				select {
				case got := <-runAnthropicTerminalTestHandler(newMinimalGatewayService(), claudeSSEResponse(tc.sse), c, passthrough):
					require.NoError(t, got.err)
					require.NotNil(t, got.result)
				case <-time.After(3 * time.Second):
					t.Fatal("stream did not finish")
				}
				require.Equal(t, tc.want, UpstreamCyberSignalObserved(c))
			})
		}
	}
}

func TestAnthropicNonStreamingResponsesDetectClaudeCyberRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"cyber refusal", `{"id":"msg_1","type":"message","role":"assistant","model":"claude-fable-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined because it could enable cyber harm."},"usage":{"input_tokens":412,"output_tokens":0}}`, true},
		{"fallback after a cyber refusal", `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-4-8","content":[{"type":"fallback","from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"cyber"}},{"type":"text","text":"Hi"}],"stop_reason":"end_turn","stop_details":null,"usage":{"input_tokens":412,"output_tokens":264}}`, true},
		{"normal answer", `{"id":"msg_3","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"cyber"}],"stop_reason":"end_turn","stop_details":null,"usage":{"input_tokens":12,"output_tokens":7}}`, false},
	} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, tc.name), func(t *testing.T) {
				c := newClaudeCyberTestContext()
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(tc.body)),
				}
				svc := newMinimalGatewayService()
				var usage *ClaudeUsage
				var err error
				if passthrough {
					usage, err = svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 2}, "", "")
				} else {
					usage, err = svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, "claude-opus-5-5", "claude-opus-5-5")
				}
				require.NoError(t, err)
				require.NotNil(t, usage)
				require.Equal(t, tc.want, UpstreamCyberSignalObserved(c))
			})
		}
	}
}

func TestAnthropicConversionPathsDetectClaudeCyberRefusal(t *testing.T) {
	type converter func(svc *GatewayService, resp *http.Response, c *gin.Context) (*ForwardResult, error)
	for name, convert := range map[string]converter{
		"chat completions buffered": func(svc *GatewayService, resp *http.Response, c *gin.Context) (*ForwardResult, error) {
			return svc.handleCCBufferedFromAnthropic(resp, c, "claude-opus-5-5", "claude-opus-5-5", nil, time.Now())
		},
		"chat completions streaming": func(svc *GatewayService, resp *http.Response, c *gin.Context) (*ForwardResult, error) {
			return svc.handleCCStreamingFromAnthropic(resp, c, "claude-opus-5-5", "claude-opus-5-5", nil, time.Now(), true)
		},
		"responses buffered": func(svc *GatewayService, resp *http.Response, c *gin.Context) (*ForwardResult, error) {
			return svc.handleResponsesBufferedStreamingResponse(resp, c, "claude-opus-5-5", "claude-opus-5-5", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		},
		"responses streaming": func(svc *GatewayService, resp *http.Response, c *gin.Context) (*ForwardResult, error) {
			return svc.handleResponsesStreamingResponse(resp, c, "claude-opus-5-5", "claude-opus-5-5", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		},
	} {
		for _, tc := range claudeCyberStreamCases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				c := newClaudeCyberTestContext()
				result, err := convert(newMinimalGatewayService(), claudeSSEResponse(tc.sse), c)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tc.want, UpstreamCyberSignalObserved(c))
			})
		}
	}
}

func claudeCyberChargeAPIKey(enabled bool, subscription bool) *APIKey {
	group := &Group{
		ID:                     17,
		Platform:               PlatformAnthropic,
		RateMultiplier:         1,
		SubscriptionType:       SubscriptionTypeStandard,
		CyberAutoChargeEnabled: enabled,
		CyberAutoChargeAmount:  0.5,
	}
	if subscription {
		group.SubscriptionType = SubscriptionTypeSubscription
	}
	return &APIKey{ID: 2, User: &User{ID: 1}, GroupID: &group.ID, Group: group}
}

func recordClaudeUsageForCyberTest(t *testing.T, apiKey *APIKey, cyberBlocked bool, subscription *UserSubscription) (*openAIRecordUsageLogRepoStub, *openAIRecordUsageBillingRepoStub) {
	t.Helper()
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: fmt.Sprintf("rid-claude-cyber-%t", cyberBlocked),
			Model:     "claude-sonnet-4",
			Usage:     ClaudeUsage{InputTokens: 412, OutputTokens: 30},
			Stream:    true,
			Duration:  time.Second,
		},
		APIKey:       apiKey,
		User:         apiKey.User,
		Account:      &Account{ID: 3},
		Subscription: subscription,
		CyberBlocked: cyberBlocked,
	}))
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, billingRepo.calls)
	return usageRepo, billingRepo
}

func TestGatewayRecordUsage_ClaudeCyberRefusalAddsGroupCharge(t *testing.T) {
	plainLog, plainBilling := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(true, false), false, nil)
	cyberLog, cyberBilling := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(true, false), true, nil)

	require.Greater(t, plainLog.lastLog.ActualCost, 0.0)
	require.NotEqual(t, RequestTypeCyberBlocked, plainLog.lastLog.RequestType, "requests without a cyber block are untouched")
	require.Equal(t, RequestTypeCyberBlocked, cyberLog.lastLog.RequestType)
	require.True(t, cyberLog.lastLog.Stream, "the stream flag is kept")
	require.InDelta(t, plainLog.lastLog.TotalCost, cyberLog.lastLog.TotalCost, 1e-12, "the charge has no upstream cost")
	require.InDelta(t, plainLog.lastLog.ActualCost+0.5, cyberLog.lastLog.ActualCost, 1e-12)
	require.InDelta(t, plainBilling.lastCmd.BalanceCost+0.5, cyberBilling.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, plainBilling.lastCmd.AccountQuotaCost, cyberBilling.lastCmd.AccountQuotaCost, 1e-12)
}

func TestGatewayRecordUsage_ClaudeCyberRefusalWithoutChargeIsOnlyLabelled(t *testing.T) {
	plainLog, _ := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(false, false), false, nil)
	cyberLog, cyberBilling := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(false, false), true, nil)

	require.Equal(t, RequestTypeCyberBlocked, cyberLog.lastLog.RequestType)
	require.InDelta(t, plainLog.lastLog.ActualCost, cyberLog.lastLog.ActualCost, 1e-12)
	require.InDelta(t, plainLog.lastLog.ActualCost, cyberBilling.lastCmd.BalanceCost, 1e-12)
}

func TestGatewayRecordUsage_ClaudeCyberChargeBillsSubscription(t *testing.T) {
	subscription := &UserSubscription{ID: 9}
	plainLog, _ := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(true, true), false, subscription)
	_, cyberBilling := recordClaudeUsageForCyberTest(t, claudeCyberChargeAPIKey(true, true), true, subscription)

	require.NotNil(t, cyberBilling.lastCmd.SubscriptionID)
	require.Equal(t, int64(9), *cyberBilling.lastCmd.SubscriptionID)
	require.InDelta(t, plainLog.lastLog.ActualCost+0.5, cyberBilling.lastCmd.SubscriptionCost, 1e-12)
	require.Zero(t, cyberBilling.lastCmd.BalanceCost)
}

func TestGatewayRecordUsageWithLongContext_CarriesCyberBlock(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	apiKey := claudeCyberChargeAPIKey(true, false)

	require.NoError(t, svc.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{
		Result:       &ForwardResult{RequestID: "rid-long-context-cyber", Model: "claude-sonnet-4", Duration: time.Second},
		APIKey:       apiKey,
		User:         apiKey.User,
		Account:      &Account{ID: 3},
		CyberBlocked: true,
	}))

	require.Equal(t, RequestTypeCyberBlocked, usageRepo.lastLog.RequestType)
	require.InDelta(t, 0.5, usageRepo.lastLog.ActualCost, 1e-12, "a zero-token refusal is billed the charge alone")
	require.InDelta(t, 0.5, billingRepo.lastCmd.BalanceCost, 1e-12)
}
