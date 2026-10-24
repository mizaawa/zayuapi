package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConfiguredPoolHTTPStatusRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const errorBody = `{"error":{"type":"upstream_error","message":"temporary relay rejection"}}`
	for _, route := range []struct {
		name, platform, path string
		call                 func(*gin.Context, *Account, *http.Response, []byte) error
	}{
		{"OpenAI Responses", PlatformOpenAI, "/v1/responses", func(c *gin.Context, a *Account, r *http.Response, _ []byte) error {
			_, err := (&OpenAIGatewayService{}).handleErrorResponse(c.Request.Context(), r, c, a, nil)
			return err
		}},
		{"OpenAI Chat compatibility", PlatformOpenAI, "/v1/chat/completions", func(c *gin.Context, a *Account, r *http.Response, _ []byte) error {
			_, err := (&OpenAIGatewayService{}).handleCompatErrorResponse(r, c, a, writeChatCompletionsError)
			return err
		}},
		{"OpenAI Anthropic compatibility", PlatformOpenAI, "/v1/messages", func(c *gin.Context, a *Account, r *http.Response, _ []byte) error {
			_, err := (&OpenAIGatewayService{}).handleCompatErrorResponse(r, c, a, writeChatCompletionsError)
			return err
		}},
		{"OpenAI passthrough", PlatformOpenAI, "/v1/responses", func(c *gin.Context, a *Account, r *http.Response, body []byte) error {
			return (&OpenAIGatewayService{}).handleErrorResponsePassthrough(c.Request.Context(), r, c, a, nil, body)
		}},
		{"Custom passthrough", PlatformCustom, "/v1/chat/completions", func(c *gin.Context, a *Account, r *http.Response, body []byte) error {
			return (&OpenAIGatewayService{}).handleCustomRawErrorResponse(c.Request.Context(), r, c, a, nil, body)
		}},
		{"Anthropic", PlatformAnthropic, "/v1/messages", func(c *gin.Context, a *Account, r *http.Response, _ []byte) error {
			_, err := (&GatewayService{}).handleErrorResponse(c.Request.Context(), r, c, a)
			return err
		}},
	} {
		t.Run(route.name, func(t *testing.T) {
			for _, test := range []struct {
				name, body string
				configure  func(*gin.Context, *Account)
				wantRetry  bool
			}{
				{name: "explicit 456", body: errorBody, wantRetry: true},
				{name: "not configured", body: errorBody, configure: func(_ *gin.Context, a *Account) { delete(a.Credentials, "pool_mode_retry_status_codes") }},
				{name: "explicit empty", body: errorBody, configure: func(_ *gin.Context, a *Account) { a.Credentials["pool_mode_retry_status_codes"] = []any{} }},
				{name: "not opted into 456", body: errorBody, configure: func(_ *gin.Context, a *Account) { a.Credentials["pool_mode_retry_status_codes"] = []any{429} }},
				{name: "pool disabled", body: errorBody, configure: func(_ *gin.Context, a *Account) { a.Credentials["pool_mode"] = false }},
				{name: "retry budget zero", body: errorBody, configure: func(_ *gin.Context, a *Account) { a.Credentials["pool_mode_retry_count"] = 0 }},
				{name: "SSE started", body: errorBody, configure: func(c *gin.Context, _ *Account) { _, _ = c.Writer.Write([]byte("data: started\n\n")); c.Writer.Flush() }},
				{name: "committed", body: errorBody, configure: func(c *gin.Context, _ *Account) { MarkResponseCommitted(c) }},
				{name: "cancelled", body: errorBody, configure: func(c *gin.Context, _ *Account) {
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				}},
				{name: "prepaid workbench", body: errorBody, configure: func(c *gin.Context, _ *Account) {
					c.Request = c.Request.WithContext(WithMediaWorkbenchPrepayment(c.Request.Context(), &MediaBillingReservation{ID: "paid-task", WorkbenchKind: "video"}))
				}},
				{name: "media endpoint", body: errorBody, configure: func(c *gin.Context, _ *Account) { c.Request.URL.Path = "/v1/images/generations" }},
				{name: "Responses image intent", body: errorBody, configure: func(c *gin.Context, _ *Account) {
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					SetOpenAIImageIntentHint(c, true)
				}},
				{name: "model unavailable", body: `{"error":{"code":"model_not_found","message":"Model does not exist"}}`},
				{name: "context length", body: `{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`},
				{name: "Anthropic prompt length", body: `{"error":{"message":"prompt is too long: 250000 tokens"}}`},
				{name: "invalid request", body: `{"error":{"type":"invalid_request_error","message":"invalid messages"}}`},
				{name: "content policy", body: `{"error":{"code":"content_policy_violation","message":"request rejected"}}`},
				{name: "cyber policy", body: `{"error":{"code":"cyber_policy","message":"flagged for cyber policy"}}`},
			} {
				t.Run(test.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, route.path, nil)
					account := &Account{ID: 17, Name: "configured pool", Platform: route.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{
						"pool_mode": true, "pool_mode_retry_status_codes": []any{456}, "pool_mode_retry_count": 2,
					}}
					if test.configure != nil {
						test.configure(c, account)
					}
					body := []byte(test.body)
					resp := &http.Response{StatusCode: 456, Header: http.Header{"Request-Id": {"upstream-456"}, "Retry-After": {"2"}}, Body: io.NopCloser(bytes.NewReader(body))}
					err := route.call(c, account, resp, body)
					require.Error(t, err)
					var failoverErr *UpstreamFailoverError
					require.Equal(t, test.wantRetry, errors.As(err, &failoverErr), "configured status must obey request safety guards")
					if !test.wantRetry {
						return
					}
					require.Equal(t, 456, failoverErr.StatusCode)
					require.True(t, failoverErr.RetryableOnSameAccount)
					require.Equal(t, "2", failoverErr.ResponseHeaders.Get("Retry-After"))
					require.Equal(t, body, failoverErr.ResponseBody)
					require.False(t, c.Writer.Written())
					require.False(t, IsResponseCommitted(c))
					require.Equal(t, 456, c.GetInt(OpsUpstreamStatusCodeKey))
					events, exists := c.Get(OpsUpstreamErrorsKey)
					require.True(t, exists)
					upstreamEvents, ok := events.([]*OpsUpstreamErrorEvent)
					require.True(t, ok)
					require.Len(t, upstreamEvents, 1)
					require.Equal(t, "upstream-456", upstreamEvents[0].UpstreamRequestID)
				})
			}
		})
	}
}

func TestConfiguredPoolHTTPStatusRetrySkipsImageTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBody := []byte(`{"model":"gpt-test","tools":[{"type":"image_generation"}],"input":"draw a bird"}`)
	responseBody := []byte(`{"error":{"message":"temporary relay rejection"}}`)
	for _, kind := range []string{"responses", "passthrough", "custom"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			account := &Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"pool_mode": true, "pool_mode_retry_status_codes": []any{456},
			}}
			resp := &http.Response{StatusCode: 456, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(responseBody))}
			svc := &OpenAIGatewayService{}
			var err error
			switch kind {
			case "responses":
				_, err = svc.handleErrorResponse(c.Request.Context(), resp, c, account, requestBody)
			case "passthrough":
				err = svc.handleErrorResponsePassthrough(c.Request.Context(), resp, c, account, requestBody, responseBody)
			case "custom":
				account.Platform = PlatformCustom
				err = svc.handleCustomRawErrorResponse(c.Request.Context(), resp, c, account, requestBody, responseBody)
			}
			var failoverErr *UpstreamFailoverError
			require.Error(t, err)
			require.False(t, errors.As(err, &failoverErr), "image tools must not enter text-only configured retries")
		})
	}
}

type configuredPool456Upstream struct {
	HTTPUpstream
	calls int
}

func (u *configuredPool456Upstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: 456, Header: http.Header{"Request-Id": {"anthropic-456"}}, Body: io.NopCloser(bytes.NewBufferString(`{"error":{"message":"temporary relay rejection"}}`))}, nil
}

func TestConfiguredPoolHTTPStatusRetryAnthropicPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	account := newAnthropicAPIKeyAccountForTest()
	account.Credentials["pool_mode"] = true
	account.Credentials["pool_mode_retry_status_codes"] = []any{456}
	// A custom handled-code list must not add a second transport retry budget.
	account.Credentials["custom_error_codes_enabled"] = true
	account.Credentials["custom_error_codes"] = []any{float64(401)}
	upstream := &configuredPool456Upstream{}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	_, err := svc.forwardAnthropicAPIKeyPassthrough(c.Request.Context(), c, account, []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hello"}]}`), "claude-test", "claude-test", false, time.Now())
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 456, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.Equal(t, 1, upstream.calls)
	require.False(t, c.Writer.Written())
}

func TestUpstream456PassthroughPreservesProvenance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, compatibility := range []bool{false, true} {
		for _, header := range []string{"X-Request-Id", "Request-Id"} {
			for _, skipMonitoring := range []bool{false, true} {
				t.Run(header+"/"+map[bool]string{false: "responses", true: "compat"}[compatibility]+"/"+map[bool]string{false: "visible", true: "skip"}[skipMonitoring], func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					rule := newNonFailoverPassthroughRule(429, "busy", 456, "provider busy")
					rule.SkipMonitoring = skipMonitoring
					rules := &ErrorPassthroughService{}
					rules.setLocalCache([]*model.ErrorPassthroughRule{rule})
					BindErrorPassthroughService(c, rules)
					resp := &http.Response{StatusCode: 429, Header: http.Header{header: {"upstream-429"}}, Body: io.NopCloser(bytes.NewBufferString(`{"error":{"message":"upstream busy"}}`))}
					account := &Account{ID: 17, Name: "trace account", Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
					svc := &OpenAIGatewayService{}
					var err error
					if compatibility {
						_, err = svc.handleCompatErrorResponse(resp, c, account, writeChatCompletionsError)
					} else {
						_, err = svc.handleErrorResponse(c.Request.Context(), resp, c, account, nil)
					}
					require.Error(t, err)
					require.Equal(t, 456, recorder.Code)
					require.Equal(t, 429, c.GetInt(OpsUpstreamStatusCodeKey))
					require.Equal(t, skipMonitoring, c.GetBool(OpsSkipPassthroughKey))
					value, exists := c.Get(OpsUpstreamErrorsKey)
					require.True(t, exists, "mapped client status must retain the original upstream attempt")
					events, ok := value.([]*OpsUpstreamErrorEvent)
					require.True(t, ok)
					require.Len(t, events, 1)
					require.Equal(t, 429, events[0].UpstreamStatusCode)
					require.Equal(t, "upstream-429", events[0].UpstreamRequestID)
					require.Equal(t, int64(17), events[0].AccountID)
				})
			}
		}
	}
}
