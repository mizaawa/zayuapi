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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageFailureUsageSurvivesOAuthForwarding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		stream bool
		body   string
	}{
		{name: "HTTP rejection", status: 400, body: `{"error":{"type":"invalid_request_error","message":"rejected"},"usage":{"input_tokens":7,"output_tokens":2}}`},
		{name: "HTTP failover", status: 500, body: `{"error":{"type":"server_error","message":"failed"},"usage":{"input_tokens":7,"output_tokens":2}}`},
		{name: "buffered failure event", status: 200, body: "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"message\":\"failed\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n"},
		{name: "streamed failure event", status: 200, stream: true, body: "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"message\":\"failed\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2","prompt":"test","stream":%t}`, tc.stream))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(string(body)))
			c.Request.Header.Set("Content-Type", "application/json")
			svc := &OpenAIGatewayService{httpUpstream: &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: tc.status, Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(tc.body)),
			}}}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-only-token"}}
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, OpenAIUsage{InputTokens: 7, OutputTokens: 2}, result.Usage)
			require.Zero(t, result.ImageCount, "reported usage must not turn a failure into a successful image result")
		})
	}
}

func TestImageFailureToolUsageRequiresTerminalEvent(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, event := range []string{"response.failed", "response.incomplete", "response.output_item.added"} {
		var usage OpenAIUsage
		svc.parseOpenAIImagesSSEUsageBytes([]byte(fmt.Sprintf(`{"type":%q,"response":{"tool_usage":{"image_gen":{"input_tokens":7,"output_tokens":2,"output_tokens_details":{"image_tokens":2}}}}}`, event)), &usage)
		if event == "response.output_item.added" {
			require.Zero(t, usage)
		} else {
			require.Equal(t, OpenAIUsage{InputTokens: 7, OutputTokens: 2, ImageOutputTokens: 2}, usage)
		}
	}
}
