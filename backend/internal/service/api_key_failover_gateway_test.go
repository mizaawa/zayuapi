package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type apiKeyFailoverAntigravityUpstream struct {
	HTTPUpstream
	calls  int
	status int
	body   string
	err    error
}

func (u *apiKeyFailoverAntigravityUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{StatusCode: u.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(u.body)), Request: req}, nil
}

func TestAPIKeyFailoverAntigravityInternalRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name         string
		status       int
		body         string
		err          error
		missingModel bool
	}{
		{name: "unavailable", status: http.StatusServiceUnavailable, body: `{"error":{"message":"upstream unavailable"}}`},
		{name: "bad request", status: http.StatusBadRequest, body: `{"error":{"message":"invalid request"}}`},
		{name: "missing model", status: http.StatusNotFound, body: `{"error":{"message":"model claude-test not found"}}`, missingModel: true},
		{name: "network failure", err: errors.New("connection failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := WithAPIKeyFailoverAttempt(t.Context())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			upstream := &apiKeyFailoverAntigravityUpstream{status: test.status, body: test.body, err: test.err}
			params := antigravityRetryLoopParams{
				ctx: ctx, c: c, account: &Account{ID: 1, Platform: PlatformAntigravity, Type: AccountTypeOAuth},
				action: "generateContent", accessToken: "test-token", body: []byte(`{}`), httpUpstream: upstream,
			}
			svc := &AntigravityGatewayService{}
			result, err := svc.antigravityRetryLoop(params)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.status, result.resp.StatusCode)
				require.NoError(t, result.resp.Body.Close())
			}
			require.Equal(t, 1, upstream.calls)
			require.True(t, APIKeyFailoverAttemptFailed(ctx))
			require.Equal(t, test.missingModel, APIKeyFailoverAttemptModelUnavailable(ctx))
			_, err = svc.antigravityRetryLoop(params)
			require.ErrorIs(t, err, ErrAPIKeyFailoverAttemptExhausted)
			require.Equal(t, 1, upstream.calls)
		})
	}
}

type apiKeyFailoverAnthropicUpstream struct {
	HTTPUpstream
	calls int
}

func (u *apiKeyFailoverAnthropicUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"overloaded_error","message":"Upstream overloaded"}}`)),
	}, nil
}

func TestAPIKeyFailoverAnthropicInternalRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := WithAPIKeyFailoverAttempt(t.Context())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
	upstream := &apiKeyFailoverAnthropicUpstream{}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	_, err := svc.forwardAnthropicAPIKeyPassthrough(ctx, c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`), "claude-test", "claude-test", false, time.Now())
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "Upstream overloaded")
	require.Equal(t, 1, upstream.calls, "one handler invocation must not consume Anthropic's internal retry budget")
}

func TestAPIKeyFailoverStreamingModelUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := WithAPIKeyFailoverAttempt(t.Context())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: " + `{"type":"response.failed","response":{"id":"resp_missing","error":{"type":"invalid_request_error","code":"model_not_found","message":"The model gpt-missing does not exist"}}}` + "\n\n")),
	}
	_, err := svc.handleStreamingResponsePassthrough(ctx, response, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, time.Now(), "gpt-missing", "gpt-missing")
	require.Error(t, err)
	require.True(t, APIKeyFailoverAttemptFailed(ctx))
	require.True(t, APIKeyFailoverAttemptModelUnavailable(ctx), "a semantic missing-model failure must not activate failover even over HTTP 200")
}

func TestAPIKeyFailoverNonStreamingMalformedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openai := &OpenAIGatewayService{cfg: &config.Config{}}
	anthropic := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}
	openaiAccount := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	anthropicAccount := &Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	handlers := []struct {
		name string
		call func(context.Context, *http.Response, *gin.Context) error
	}{
		{"OpenAI", func(ctx context.Context, resp *http.Response, c *gin.Context) error {
			_, err := openai.handleNonStreamingResponse(ctx, resp, c, openaiAccount, "gpt-test", "gpt-test")
			return err
		}},
		{"OpenAI passthrough", func(ctx context.Context, resp *http.Response, c *gin.Context) error {
			_, err := openai.handleNonStreamingResponsePassthrough(ctx, resp, c, openaiAccount, "gpt-test", "gpt-test")
			return err
		}},
		{"Anthropic", func(ctx context.Context, resp *http.Response, c *gin.Context) error {
			_, err := anthropic.handleNonStreamingResponse(ctx, resp, c, anthropicAccount, "claude-test", "claude-test")
			return err
		}},
		{"Anthropic passthrough", func(ctx context.Context, resp *http.Response, c *gin.Context) error {
			_, err := anthropic.handleNonStreamingResponseAnthropicAPIKeyPassthrough(ctx, resp, c, anthropicAccount, "claude-test", "claude-test")
			return err
		}},
	}
	for _, handler := range handlers {
		t.Run(handler.name, func(t *testing.T) {
			for _, failure := range []string{"invalid JSON", "body read failure"} {
				t.Run(failure, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					ctx := WithAPIKeyFailoverAttempt(t.Context())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
					var body io.Reader = strings.NewReader("<html>Upstream connection failed</html>")
					if failure == "body read failure" {
						body = iotest.ErrReader(io.ErrUnexpectedEOF)
					}
					response := &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(body),
					}
					err := handler.call(ctx, response, c)
					require.Error(t, err)
					require.True(t, APIKeyFailoverAttemptFailed(ctx), "a failed HTTP 200 response must consume an upstream attempt")
					require.False(t, APIKeyFailoverAttemptModelUnavailable(ctx))
					require.False(t, c.Writer.Written(), "the middleware must retain the option to retry before output")
				})
			}
		})
	}
}

func TestAPIKeyFailoverNonStreamingReadErrorPreservesMissingModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := WithAPIKeyFailoverAttempt(t.Context())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	RecordAPIKeyFailoverUpstreamFailure(ctx, http.StatusNotFound, []byte(`{"error":{"code":"model_not_found"}}`))
	_, err := ReadUpstreamResponseBody(iotest.ErrReader(io.ErrUnexpectedEOF), nil, c, nil)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.True(t, APIKeyFailoverAttemptModelUnavailable(ctx), "body errors must not erase the upstream missing-model exception")
}

func TestAPIKeyFailoverNonStreamingMissingModelExclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := WithAPIKeyFailoverAttempt(t.Context())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"model_not_found","message":"The model gpt-missing does not exist"}}`)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	_, err := svc.handleNonStreamingResponse(ctx, response, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "gpt-missing", "gpt-missing")
	require.Error(t, err)
	require.True(t, APIKeyFailoverAttemptFailed(ctx))
	require.True(t, APIKeyFailoverAttemptModelUnavailable(ctx), "a provider missing-model error remains excluded when its HTTP 200 body lacks a normal response")
}

func TestAPIKeyFailoverMalformedPassthroughIsOptIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	const body = "<html>Gateway error</html>"
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	_, err := svc.handleNonStreamingResponsePassthrough(c.Request.Context(), response, c, &Account{ID: 1, Platform: PlatformOpenAI}, "gpt-test", "gpt-test")
	require.NoError(t, err)
	require.Equal(t, body, recorder.Body.String(), "keys without failover retain raw passthrough behavior")
}
