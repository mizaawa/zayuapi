//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardAsRawChatCompletions_StreamReadErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	metadata := `data: {"id":"chatcmpl_partial","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n"
	content := `data: {"id":"chatcmpl_partial","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n"
	usage := `data: {"id":"chatcmpl_partial","object":"chat.completion.chunk","model":"gpt-5.6-sol","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":4}}}` + "\n\n"

	tests := []struct {
		name         string
		payload      string
		largeRequest bool
		wantFailover bool
		wantError    bool
	}{
		{name: "before output", wantFailover: true, wantError: true},
		{name: "buffered metadata before output", payload: metadata, largeRequest: true, wantFailover: true, wantError: true},
		{name: "after output retains usage", payload: content + usage, wantError: true},
		{name: "close error after done", payload: content + usage + "data: [DONE]\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"sol","messages":[{"role":"user","content":"hello"}],"stream":true,"service_tier":"priority","reasoning_effort":"high"}`)
			if tt.largeRequest {
				body = largeRawChatCompletionsBody()
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"rid_raw_partial"},
				},
				Body: io.NopCloser(io.MultiReader(strings.NewReader(tt.payload), iotest.ErrReader(io.ErrUnexpectedEOF))),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := rawChatCompletionsTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"sol": "gpt-5.6-sol"}

			result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
			var failoverErr *UpstreamFailoverError
			if tt.wantFailover {
				require.ErrorAs(t, err, &failoverErr)
				require.Nil(t, result)
				require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
				require.Equal(t, "rid_raw_partial", failoverErr.ResponseHeaders.Get("X-Request-Id"))
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
				return
			}
			if tt.wantError {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				require.False(t, errors.As(err, &failoverErr))
				code, _, ok := OpenAIUpstreamStreamReadErrorDetails(err)
				require.True(t, ok)
				require.Equal(t, OpenAIUpstreamStreamReadErrorCode, code)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, result)
			require.Equal(t, 12, result.Usage.InputTokens)
			require.Equal(t, 6, result.Usage.OutputTokens)
			require.Equal(t, 4, result.Usage.CacheReadInputTokens)
			require.Equal(t, "rid_raw_partial", result.RequestID)
			require.Equal(t, "sol", result.Model)
			require.Equal(t, "gpt-5.6-sol", result.BillingModel)
			require.Equal(t, "gpt-5.6-sol", result.UpstreamModel)
			require.Equal(t, grokChatRawEndpoint, result.UpstreamEndpoint)
			require.NotNil(t, result.ServiceTier)
			require.Equal(t, "priority", *result.ServiceTier)
			require.NotNil(t, result.ReasoningEffort)
			require.Equal(t, "high", *result.ReasoningEffort)
			require.NotNil(t, result.FirstTokenMs)
			require.True(t, result.Stream)
			require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
			expectedPayload := strings.ReplaceAll(tt.payload, `"model":"gpt-5.6-sol"`, `"model":"sol"`)
			require.Equal(t, expectedPayload, rec.Body.String())
		})
	}
}
