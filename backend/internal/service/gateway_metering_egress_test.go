//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCustomMeteringMatchesDeliveredProtocol(t *testing.T) {
	const success = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"see https://public.example/help\"}\r\n\r\n"
	const metered = "event: response.completed\r\ndata: {\"type\":\"response.completed\",\r\ndata: \"response\":{\"id\":\"resp_ok\",\"usage\":{\"input_tokens\":8,\"output_tokens\":3}}}\r\n\r\n"
	for _, tc := range []struct {
		name              string
		stream, truncated bool
		contentType, body string
	}{
		{"buffered SSE", false, false, "text/event-stream", success + metered},
		{"buffered mislabeled SSE", false, false, "application/json", success + metered},
		{"stream interrupted after complete events", true, true, "text/event-stream", success + metered},
		{"stream requested but buffered JSON returned", true, false, "application/json", `{"usage":{"input_tokens":8,"output_tokens":3},"id":"resp_ok"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reader io.Reader = strings.NewReader(tc.body)
			if tc.truncated {
				reader = io.MultiReader(reader, passthroughErrReadCloser{err: io.ErrUnexpectedEOF})
			}
			up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(reader)}}
			request, _ := json.Marshal(map[string]any{"model": "gpt-5.1", "input": "hello", "stream": tc.stream})
			c, w := customTransparentTestContext(customResponsesEndpoint, request)
			result, err := customTransparentTestService(up).Forward(t.Context(), c, customTransparentTestAccount(), request)
			if tc.truncated {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, result)
			require.Equal(t, 8, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.Equal(t, "resp_ok", result.ResponseID)
			require.Equal(t, tc.body, w.Body.String())
		})
	}
}

func TestCustomErrorEgressSeparation(t *testing.T) {
	const msg = `{"error":{"message":"invalid field; fixture-credential at https://private.test/path"},"vendor_code":77}`
	request := []byte(`{"model":"gpt-5.1","input":"test"}`)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		stream bool
	}{
		{"direct", 422, msg, false}, {"exhausted", 503, msg, false}, {"HTML", 422, "<html>private.test</html>", false},
		{"stream error", 200, "event: error\r\ndata: {\"error\":\r\ndata: {\"message\":\"invalid field at https://private.test using fixture-credential\"}}\r\n\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request
			headers := http.Header{"Location": {"https://private.test"}, "Www-Authenticate": {"Bearer realm=private.test"}, "Retry-After": {"3"}, "Content-Type": {"text/html"}}
			if tc.stream {
				req = []byte(`{"model":"gpt-5.1","input":"test","stream":true}`)
				headers.Set("Content-Type", "text/event-stream")
			}
			a := customTransparentTestAccount()
			a.Credentials["api_key"] = "fixture-credential"
			up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: headers, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := customTransparentTestService(up)
			c, w := customTransparentTestContext(customResponsesEndpoint, req)
			_, err := svc.Forward(t.Context(), c, a, req)
			if tc.stream {
				require.NoError(t, err)
				require.Contains(t, w.Body.String(), "event: error")
			} else {
				require.Error(t, err)
			}
			if tc.status == 503 {
				var fail *UpstreamFailoverError
				require.ErrorAs(t, err, &fail)
				require.Contains(t, string(fail.ResponseBody), "private.test")
				require.Empty(t, w.Body.String())
				require.True(t, svc.WriteCustomFailoverResponse(c, fail))
			}
			require.Equal(t, tc.status, w.Code)
			require.NotContains(t, w.Body.String(), "private.test")
			require.NotContains(t, w.Body.String(), "fixture-credential")
			require.Empty(t, w.Header().Get("Location"))
			require.Empty(t, w.Header().Get("WWW-Authenticate"))
			if !tc.stream {
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				require.True(t, json.Valid(w.Body.Bytes()))
			}
			if tc.body == msg {
				require.Contains(t, w.Body.String(), "invalid field")
				require.Contains(t, w.Body.String(), "vendor_code")
			}
		})
	}
}

func TestGeminiOriginIsNotPublished(t *testing.T) {
	const body = `{"error":{"status":"INVALID_ARGUMENT","message":"invalid field: fixture-credential at https://private.test"}}`
	a := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-credential"}}
	for _, get := range []bool{false, true} {
		up := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 400, Header: http.Header{"Www-Authenticate": {"Bearer realm=private.test, error=insufficient_scope"}, "Location": {"https://private.test"}}, Body: io.NopCloser(strings.NewReader(body))}}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: up}
		var public []byte
		if get {
			up.response.StatusCode = 403
			res, err := svc.ForwardAIStudioGET(t.Context(), a, "/v1beta/models")
			require.NoError(t, err)
			require.True(t, res.InsufficientScope)
			require.Empty(t, res.Headers.Get("WWW-Authenticate"))
			require.Empty(t, res.Headers.Get("Location"))
			public = res.Body
		} else {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/test:generateContent", nil)
			_, err := svc.ForwardNative(t.Context(), c, a, "gemini-2.5-flash", "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"test"}]}]}`))
			require.Error(t, err)
			require.Equal(t, 400, w.Code)
			public = w.Body.Bytes()
		}
		require.True(t, json.Valid(public))
		require.Contains(t, string(public), "invalid field")
		require.NotContains(t, string(public), "private.test")
		require.NotContains(t, string(public), "fixture-credential")
	}
}

func TestBillingUsesServerIdentityForSeparateSettlements(t *testing.T) {
	logs := &openAIRecordUsageLogRepoStub{}
	ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	key := &APIKey{ID: 2, User: &User{ID: 1}}
	a := &Account{ID: 3}
	trace := "shared-trace"
	for _, id := range []string{"a", "b"} {
		ctx := context.WithValue(t.Context(), ctxkey.ClientRequestID, id)
		svc.RecordCyberPolicyUsageLog(ctx, CyberPolicyUsageInput{APIKey: key, Account: a, RequestID: trace, Model: "gpt-5.1", InputTokens: 8, OutputTokens: 3})
		require.Equal(t, "client:"+id, ledger.lastCmd.RequestID)
		require.Positive(t, ledger.lastCmd.BalanceCost)
	}
	for _, id := range []string{"ws:connection:1", "ws:connection:2", "ws:connection:2"} {
		err := svc.RecordUsage(t.Context(), &OpenAIRecordUsageInput{BillingRequestID: id, Result: &OpenAIForwardResult{RequestID: trace, OpenAIWSMode: true, Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 8, OutputTokens: 3}}, APIKey: key, User: key.User, Account: a})
		require.NoError(t, err)
		require.Equal(t, id, ledger.lastCmd.RequestID)
		require.Equal(t, id, logs.lastLog.RequestID)
	}
}
