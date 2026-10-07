//go:build unit

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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIRecordUsageServiceTierRequiresUpstreamConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name         string
		upstreamTier *string
		factor       float64
	}{
		{name: "request flex alone", factor: 1},
		{name: "upstream default overrides request", upstreamTier: optionalTrimmedStringPtr("default"), factor: 1},
		{name: "upstream flex", upstreamTier: optionalTrimmedStringPtr("flex"), factor: 0.5},
		{name: "upstream priority", upstreamTier: optionalTrimmedStringPtr("priority"), factor: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID:           "tier-security",
					Model:               "gpt-5.4",
					ServiceTier:         optionalTrimmedStringPtr("flex"),
					UpstreamServiceTier: tc.upstreamTier,
					Usage:               OpenAIUsage{InputTokens: 100, OutputTokens: 50},
					Duration:            time.Second,
				},
				APIKey: &APIKey{ID: 1}, User: &User{ID: 2}, Account: &Account{ID: 3},
			})
			require.NoError(t, err)
			base, err := svc.billingService.CalculateCost("gpt-5.4", UsageTokens{InputTokens: 100, OutputTokens: 50}, 1)
			require.NoError(t, err)
			require.InDelta(t, base.TotalCost*tc.factor, usageRepo.lastLog.TotalCost, 1e-10)
			require.Equal(t, tc.upstreamTier, usageRepo.lastLog.ServiceTier)
			require.Equal(t, 1, userRepo.deductCalls)
		})
	}
}

func TestUpstreamServiceTierObservationUsesFinalResponse(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"service_tier":"flex"}}`), "response.created")
	require.Nil(t, observer.ServiceTier())
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"service_tier":"default"}}`), "response.completed")
	require.Equal(t, "default", *observer.ServiceTier())
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{}}`), "response.completed")
	require.Nil(t, observer.ServiceTier())
	observer.ObserveOpenAI([]byte(`{"service_tier":"flex","object":"chat.completion.chunk"}`), "")
	require.Equal(t, "flex", *observer.ServiceTier())
	observer.ObserveOpenAI([]byte(`{"service_tier":"priority","object":"response"}`), "")
	require.Equal(t, "priority", *observer.ServiceTier())
}

func TestRawChatForwardCapturesUpstreamServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, upstreamTier := range []string{"", "default", "flex"} {
			t.Run(fmt.Sprintf("stream=%t/tier=%s", stream, upstreamTier), func(t *testing.T) {
				requestBody := []byte(fmt.Sprintf(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"stream":%t,"service_tier":"flex"}`, stream))
				responseBody := `{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}`
				if upstreamTier != "" {
					responseBody += fmt.Sprintf(`,"service_tier":%q`, upstreamTier)
				}
				responseBody += "}"
				contentType := "application/json"
				if stream {
					responseBody = "data: " + responseBody + "\n\ndata: [DONE]\n\n"
					contentType = "text/event-stream"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))
				result, err := svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), requestBody, "")
				require.NoError(t, err)
				require.Equal(t, optionalTrimmedStringPtr(upstreamTier), result.UpstreamServiceTier)
				require.Equal(t, "flex", *result.ServiceTier)
			})
		}
	}
}
