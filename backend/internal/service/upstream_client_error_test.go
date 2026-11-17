package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayBadRequestRedactsProviderSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "legacy-custom-upstream-credential"
	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": secret}}
	body := `{"type":"error","error":{"type":"invalid_request_error","message":"temperature must be <= 1; rejected legacy-custom-upstream-credential at https://private-provider.example/v1/messages?key=legacy-custom-upstream-credential; contact owner@provider.example"},"debug":{"api_key":"legacy-custom-upstream-credential","headers":{"Authorization":"Bearer vendor-secret"}}}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
	_, err := (&GatewayService{}).handleErrorResponse(context.Background(), resp, c, account)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.True(t, json.Valid(rec.Body.Bytes()))
	for _, forbidden := range []string{secret, "private-provider.example", "vendor-secret"} {
		require.NotContains(t, rec.Body.String(), forbidden)
	}
	require.Contains(t, rec.Body.String(), "temperature must be")
	require.Contains(t, rec.Body.String(), "owner@provider.example")
	require.Contains(t, rec.Body.String(), "invalid_request_error")
}

func TestGatewayBadRequestDoesNotForwardProviderHTML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`<html>internal admin email and credential dump</html>`)), Header: http.Header{}}
	_, err := (&GatewayService{}).handleErrorResponse(context.Background(), resp, c, &Account{ID: 1, Type: AccountTypeAPIKey})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.True(t, json.Valid(rec.Body.Bytes()))
	require.NotContains(t, rec.Body.String(), "credential dump")
}

func TestErrorPassthroughRedactsProviderSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	rule := newNonFailoverPassthroughRule(http.StatusBadRequest, "invalid schema", http.StatusBadRequest, "")
	rule.PassthroughBody = true
	svc := &ErrorPassthroughService{}
	svc.setLocalCache([]*model.ErrorPassthroughRule{rule})
	BindErrorPassthroughService(c, svc)
	body := []byte(`{"error":{"message":"invalid schema; key sk-proj-abcdefghijklmnopqrstuv_suffix; Authorization: Bearer opaquecredential; https://upstream.example/api?api_key=secret"}}`)
	_, _, message, matched := applyErrorPassthroughRule(c, PlatformAnthropic, http.StatusBadRequest, body, http.StatusBadGateway, "upstream_error", "failed")
	require.True(t, matched)
	require.Contains(t, message, "invalid schema")
	for _, forbidden := range []string{"abcdefghijklmnopqrstuv", "suffix", "opaquecredential", "upstream.example", "=secret"} {
		require.NotContains(t, message, forbidden)
	}
}

func TestUserErrorViewRedactsHistoricalProviderDetails(t *testing.T) {
	const key = "sk-proj-abcdefghijklmnopqrstuv_123"
	src := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{Message: "Rejected " + key + " at https://provider.example/api"},
		ErrorBody:   `{"error":{"message":"invalid schema; Bearer opaque-credential; owner@provider.example"},"debug":[{"api_key":"legacy-secret","url":"https://provider.example/api"}]}`,
	}
	originalBody := src.ErrorBody
	detail := ToUserErrorRequestDetail(src)
	encoded, err := json.Marshal(detail)
	require.NoError(t, err)
	for _, forbidden := range []string{key, "opaque-credential", "legacy-secret", "https://provider.example"} {
		require.NotContains(t, string(encoded), forbidden)
	}
	require.Contains(t, detail.ErrorBody, "invalid schema")
	require.Contains(t, detail.ErrorBody, "owner@provider.example")
	require.Equal(t, originalBody, src.ErrorBody, "admin diagnostic record must not be mutated")
}

func TestClientErrorBodyPreservesValidationFields(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_parameter","param":"temperature","message":"temperature must be between 0 and 1"}}`)
	require.JSONEq(t, string(body), string(sanitizeUpstreamClientErrorBody(body, nil)))
	require.Equal(t, "", string(sanitizeUpstreamClientErrorBody(nil, nil)))
}

func TestClientErrorBodyPreservesEmailAddresses(t *testing.T) {
	body := []byte(`{"user_email":"owner@example.com","email":"user@example.com","error":{"message":"contact owner@example.com"}}`)
	require.Equal(t, string(body), string(sanitizeUpstreamClientErrorBody(body, nil)))
}

func TestGeminiMappedErrorsRedactCustomAccountCredentials(t *testing.T) {
	for _, chat := range []bool{false, true} {
		t.Run(map[bool]string{false: "messages", true: "chat"}[chat], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			account := &Account{ID: 1, Credentials: map[string]any{"api_key": "legacy-provider-credential"}}
			body := []byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"invalid schema for legacy-provider-credential at https://private.example/api; contact owner@example.com"}}`)
			rule := newNonFailoverPassthroughRule(http.StatusBadRequest, "invalid schema", http.StatusBadRequest, "")
			rule.Platforms = []string{PlatformGemini}
			rule.PassthroughBody = true
			passthrough := &ErrorPassthroughService{}
			passthrough.setLocalCache([]*model.ErrorPassthroughRule{rule})
			BindErrorPassthroughService(c, passthrough)
			svc := &GeminiMessagesCompatService{}
			var err error
			if chat {
				err = svc.writeGeminiChatCompletionsMappedError(c, account, 400, "", body)
			} else {
				err = svc.writeGeminiMappedError(c, account, 400, "", body)
			}
			require.Error(t, err)
			require.NotContains(t, rec.Body.String(), "legacy-provider-credential")
			require.NotContains(t, rec.Body.String(), "private.example")
			require.Contains(t, rec.Body.String(), "owner@example.com")
		})
	}
}

func TestAnthropicPassthroughStreamErrorRedactsProviderSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, eventPrefix := range []string{"event: error\n", ""} {
		t.Run(eventPrefix, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(eventPrefix + `data: {"type":"error","error":{"type":"invalid_request_error","message":"rejected custom-secret at https://provider.example"}}` + "\n\ndata: [DONE]\n\n")),
			}
			account := &Account{ID: 1, Credentials: map[string]any{"api_key": "custom-secret"}}
			_, err := (&GatewayService{}).handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, account, time.Now(), "claude", "claude")
			require.NoError(t, err)
			require.NotContains(t, rec.Body.String(), "custom-secret")
			require.NotContains(t, rec.Body.String(), "provider.example")
			require.Contains(t, rec.Body.String(), "invalid_request_error")
		})
	}
}

func TestOpenAIStreamErrorRedactsProviderSecrets(t *testing.T) {
	for _, eventType := range []string{"error", "response.failed"} {
		t.Run(eventType, func(t *testing.T) {
			body := []byte(`{"type":"` + eventType + `","error":{"type":"invalid_request_error","message":"Rejected sk-proj-abcdefghijklmnopqrstuv_suffix at https://provider.example"},"debug":{"Authorization":"Bearer opaquecredential"}}`)
			out, changed := sanitizeOpenAIResponseFailedEventForClient(body, eventType, false)
			require.True(t, changed)
			require.True(t, json.Valid(out))
			for _, forbidden := range []string{"abcdefghijklmnopqrstuv_suffix", "provider.example", "opaquecredential"} {
				require.NotContains(t, string(out), forbidden)
			}
			require.Contains(t, string(out), "invalid_request_error")
		})
	}
}

func TestOpenAIStreamErrorRedactsCustomAccountCredentials(t *testing.T) {
	account := &Account{Credentials: map[string]any{"api_key": "legacy-provider-credential"}}
	body := []byte(`{"type":"error","error":{"message":"invalid schema for legacy-provider-credential; contact owner@example.com"}}`)
	out, changed := sanitizeOpenAIResponseFailedEventForClient(body, "error", true, account)
	require.True(t, changed)
	require.NotContains(t, string(out), "legacy-provider-credential")
	require.Contains(t, string(out), "owner@example.com")
}
