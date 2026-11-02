//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func customTransparentTestAccount() *Account {
	return &Account{
		ID:          9001,
		Name:        "custom-test",
		Platform:    PlatformCustom,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "sk-custom-secret",
			"base_url": "http://custom-upstream.test",
		},
	}
}

func customTransparentTestService(upstream *httpUpstreamRecorder) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:           false,
			AllowInsecureHTTP: true,
		}}},
		httpUpstream: upstream,
	}
}

func customTransparentTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer client-key-must-not-leak")
	c.Request.Header.Set("X-Client-Trace", "ignored-by-safe-header-policy")
	return c, recorder
}

func TestForwardCustomTransparentResponsesPreservesBodyAndUsesResponsesEndpoint(t *testing.T) {
	body := []byte(`{"model":"vendor/arbitrary-42","stream":false,"vendor_extension":{"keep":true},"input":[{"type":"input_text","text":"hello"}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"custom-resp-1"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"id":"resp_custom","object":"response","model":"vendor/arbitrary-42","output":[],"usage":{"input_tokens":2,"output_tokens":1}}`))),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/responses", body)

	result, err := svc.Forward(context.Background(), c, customTransparentTestAccount(), body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, body, upstream.lastBody)
	require.Equal(t, "http://custom-upstream.test/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-custom-secret", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "resp_custom", result.ResponseID)
	require.Equal(t, 2, result.Usage.InputTokens)
	require.Equal(t, `{"id":"resp_custom","object":"response","model":"vendor/arbitrary-42","output":[],"usage":{"input_tokens":2,"output_tokens":1}}`, recorder.Body.String())
}

func TestForwardCustomTransparentResponsesAppliesAccountModelMapping(t *testing.T) {
	body := []byte(`{"model":"monitor-alias","stream":false,"input":"hello","vendor_extension":{"keep":true}}`)
	upstreamBody := []byte(`{"id":"resp_custom_mapped","object":"response","model":"provider-model","output":[]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	}}
	account := customTransparentTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"monitor-alias": "provider-model"}
	svc := customTransparentTestService(upstream)
	c, _ := customTransparentTestContext("/v1/responses", body)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "monitor-alias", result.Model)
	require.Equal(t, "provider-model", result.UpstreamModel)
	require.JSONEq(t, `{"model":"provider-model","stream":false,"input":"hello","vendor_extension":{"keep":true}}`, string(upstream.lastBody))
}

// A streaming Chat Completions upstream reports usage only when
// stream_options.include_usage is set. Without it the request could only be
// billed at $0, so the Custom path forces it exactly like the native path and
// leaves every other field untouched.
func TestForwardAsChatCompletionsCustomTransparentForcesStreamUsage(t *testing.T) {
	body := []byte(`{"model":"vendor/chat-42","stream":true,"messages":[{"role":"user","content":"hello"}],"vendor_extension":{"opaque":[1,2,3]}}`)
	upstreamBody := []byte("data: {\"id\":\"chat_custom\",\"object\":\"chat.completion.chunk\",\"model\":\"vendor/chat-42\",\"choices\":[]}\n\ndata: [DONE]\n\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"custom-chat-1"}},
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, customTransparentTestAccount(), body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Equal(t, `{"opaque":[1,2,3]}`, gjson.GetBytes(upstream.lastBody, "vendor_extension").Raw)
	require.Equal(t, "vendor/chat-42", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "http://custom-upstream.test/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-custom-secret", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, upstreamBody, recorder.Body.Bytes())
}

func TestForwardAsChatCompletionsCustomTransparentAppliesAccountModelMapping(t *testing.T) {
	body := []byte(`{"model":"monitor-alias","stream":false,"messages":[{"role":"user","content":"hello"}],"vendor_extension":{"keep":true}}`)
	upstreamBody := []byte(`{"id":"chat_custom_mapped","object":"chat.completion","model":"provider-model","choices":[]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	}}
	account := customTransparentTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"monitor-alias": "provider-model"}
	svc := customTransparentTestService(upstream)
	c, _ := customTransparentTestContext("/v1/chat/completions", body)

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "monitor-alias", result.Model)
	require.Equal(t, "provider-model", result.UpstreamModel)
	require.JSONEq(t, `{"model":"provider-model","stream":false,"messages":[{"role":"user","content":"hello"}],"vendor_extension":{"keep":true}}`, string(upstream.lastBody))
}

func TestForwardAsAnthropicCustomTransparentPreservesMessagesBodyAndResponse(t *testing.T) {
	body := []byte(`{"model":"vendor/claude-compatible","max_tokens":32,"stream":false,"system":[{"type":"text","text":"keep this"}],"metadata":{"vendor_flag":true}}`)
	responseBody := []byte(`{"id":"msg_custom_1","type":"message","role":"assistant","model":"vendor/claude-compatible","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":4,"output_tokens":6}}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"custom-msg-1"}},
		Body:       io.NopCloser(bytes.NewReader(responseBody)),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/messages", body)
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	c.Request.Header.Set("anthropic-beta", "vendor-beta")

	result, err := svc.ForwardAsAnthropic(context.Background(), c, customTransparentTestAccount(), body, "", "")

	require.NoError(t, err)
	require.Equal(t, body, upstream.lastBody)
	require.Equal(t, "http://custom-upstream.test/v1/messages", upstream.lastReq.URL.String())
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "sk-custom-secret", upstream.lastReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", upstream.lastReq.Header.Get("anthropic-version"))
	require.Equal(t, "vendor-beta", upstream.lastReq.Header.Get("anthropic-beta"))
	require.Equal(t, responseBody, recorder.Body.Bytes())
	require.Equal(t, "msg_custom_1", result.ResponseID)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
	require.Equal(t, customMessagesEndpoint, result.UpstreamEndpoint)
}

func TestForwardAsAnthropicCustomTransparentAppliesAccountModelMapping(t *testing.T) {
	body := []byte(`{"model":"monitor-alias","max_tokens":16,"stream":false,"messages":[{"role":"user","content":"hello"}]}`)
	upstreamBody := []byte(`{"id":"msg_custom_mapped","type":"message","role":"assistant","model":"provider-model","content":[{"type":"text","text":"hello"}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	}}
	account := customTransparentTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"monitor-alias": "provider-model"}
	svc := customTransparentTestService(upstream)
	c, _ := customTransparentTestContext("/v1/messages", body)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "monitor-alias", result.Model)
	require.Equal(t, "provider-model", result.UpstreamModel)
	require.JSONEq(t, `{"model":"provider-model","max_tokens":16,"stream":false,"messages":[{"role":"user","content":"hello"}]}`, string(upstream.lastBody))
}

func TestForwardAsAnthropicCustomTransparentPreservesSSEAndAnthropicUsage(t *testing.T) {
	body := []byte(`{"model":"vendor/claude-compatible","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	upstreamBody := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_custom_stream\",\"model\":\"vendor/claude-compatible\",\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\ndata: [DONE]\n\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader(upstreamBody)),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/messages", body)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, customTransparentTestAccount(), body, "", "")

	require.NoError(t, err)
	require.Equal(t, upstreamBody, recorder.Body.Bytes())
	require.Equal(t, "sk-custom-secret", upstream.lastReq.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", upstream.lastReq.Header.Get("anthropic-version"))
	require.Equal(t, "msg_custom_stream", result.ResponseID)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 9, result.Usage.OutputTokens)
	require.True(t, result.Stream)
}

func TestForwardCustomTransparentPreservesNonFailoverErrorResponse(t *testing.T) {
	requestBody := []byte(`{"model":"vendor/model","input":"bad"}`)
	responseBody := []byte(`{"error":{"type":"vendor_validation","message":"opaque details"},"vendor_code":991}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Header: http.Header{
			"Content-Type": []string{"application/problem+json"},
			"X-Request-Id": []string{"vendor-request-1"},
		},
		Body: io.NopCloser(bytes.NewReader(responseBody)),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/responses", requestBody)

	result, err := svc.Forward(context.Background(), c, customTransparentTestAccount(), requestBody)

	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "vendor-request-1", recorder.Header().Get("X-Request-Id"))
	require.Equal(t, responseBody, recorder.Body.Bytes())
}

func TestForwardCustomTransparentMarksFailoverErrorForFinalRawResponse(t *testing.T) {
	requestBody := []byte(`{"model":"vendor/model","input":"hello"}`)
	responseBody := []byte(`{"error":{"type":"vendor_capacity","message":"try another account"}}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Retry-After":  []string{"9"},
		},
		Body: io.NopCloser(bytes.NewReader(responseBody)),
	}}
	svc := customTransparentTestService(upstream)
	c, recorder := customTransparentTestContext("/v1/responses", requestBody)

	result, err := svc.Forward(context.Background(), c, customTransparentTestAccount(), requestBody)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.True(t, failoverErr.RawResponsePassthrough)
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.Equal(t, responseBody, failoverErr.ResponseBody)
	require.Equal(t, "9", failoverErr.ResponseHeaders.Get("Retry-After"))
	require.Empty(t, recorder.Body.Bytes())
}
