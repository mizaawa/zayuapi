package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const accountTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg=="

func TestAccountTestService_OpenAIImageOAuthHandlesOutputItemDoneFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000006,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{
		ID:       53,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	err := svc.testOpenAIImageOAuth(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Contains(t, rec.Body.String(), "Calling Codex /responses image tool")
	require.Contains(t, rec.Body.String(), "data:image/png;base64,aGVsbG8=")
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_OpenAIImageAPIKeyUsesConfiguredV1BaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"` + accountTestPNG + `","revised_prompt":"draw a cat"}]}`)),
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:       54,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-image-2", "draw a cat")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Contains(t, rec.Body.String(), "data:image/png;base64,"+accountTestPNG)
	require.Contains(t, rec.Body.String(), "\"success\":true")
}

func TestAccountTestService_CustomImageRouting(t *testing.T) {
	for _, tc := range []struct {
		name, requested, mapped, mode, protocol string
	}{
		{name: "gpt", requested: "gpt-image-2"},
		{name: "compatible", requested: "banana-2"},
		{name: "grok", requested: "grok-imagine-image"},
		{name: "gemini", requested: "gemini-3.1-flash-image"},
		{name: "dall-e", requested: "dall-e-3"},
		{name: "mapped capability", requested: "custom-image", mapped: "banana-pro"},
		{name: "opaque upstream", requested: "gpt-image-2", mapped: "provider-model-123"},
		{name: "explicit image mode", requested: "svg", mapped: "provider-art", mode: "image"},
		{name: "8yes image", requested: "gpt-image-2", protocol: "8yes"},
		{name: "sora image", requested: "gpt-image-2", protocol: "sora"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"url":"https://cdn.example/image.png"}]}`)),
			}}
			account := &Account{ID: 1, Platform: PlatformCustom, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"base_url": "https://provider.example/v1", "api_key": "private-key", "media_protocol": tc.protocol},
			}
			if tc.mapped != "" {
				account.Credentials["model_mapping"] = map[string]any{tc.requested: tc.mapped}
			}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}}
			require.NoError(t, svc.testOpenAIAccountConnection(c, account, tc.requested, "draw", tc.mode))
			require.Len(t, upstream.requests, 1, "a paid creation must never be retried on another endpoint")
			require.Equal(t, "/v1/images/generations", upstream.lastReq.URL.Path)
			expectedModel := tc.requested
			if tc.mapped != "" {
				expectedModel = tc.mapped
			}
			require.Equal(t, expectedModel, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "draw", gjson.GetBytes(upstream.lastBody, "prompt").String())
			require.Equal(t, int64(1), gjson.GetBytes(upstream.lastBody, "n").Int())
			require.False(t, gjson.GetBytes(upstream.lastBody, "response_format").Exists())
			require.Contains(t, rec.Body.String(), `"image_url":"https://cdn.example/image.png"`)
			require.Contains(t, rec.Body.String(), `"success":true`)
		})
	}
}

func TestAccountTestService_ImageRejectsUnsupportedProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	upstream := &httpUpstreamRecorder{}
	svc := &AccountTestService{httpUpstream: upstream}
	account := &Account{Platform: PlatformCustom, Type: AccountTypeAPIKey, Credentials: map[string]any{"media_protocol": "968"}}
	require.ErrorContains(t, svc.testOpenAIAccountConnection(c, account, "gpt-image-2", "draw", ""), "Unsupported media protocol")
	require.Empty(t, upstream.requests)
}

func TestAccountTestService_ImageResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
		status                int
		limit                 int64
	}{
		{name: "empty item", body: `{"data":[{}]}`, wantError: "No images"},
		{name: "html payload", body: `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("<html>secret</html>")) + `"}]}`, wantError: "Unsupported image content"},
		{name: "invalid base64", body: `{"data":[{"b64_json":"bad%data"}]}`, wantError: "Invalid base64"},
		{name: "local URL", body: `{"data":[{"url":"http://127.0.0.1/private"}]}`, wantError: "Unsafe image URL"},
		{name: "script URL", body: `{"data":[{"url":"javascript:alert(1)"}]}`, wantError: "Unsafe image URL"},
		{name: "credential URL", body: `{"data":[{"url":"https://user:pass@cdn.example/image.png"}]}`, wantError: "Unsafe image URL"},
		{name: "too large", body: strings.Repeat("x", 65), limit: 64, wantError: "upstream response body too large"},
		{name: "upstream error", body: `{"error":{"message":"private-key denied","api_key":"private-key"}}`, status: http.StatusBadGateway, wantError: "Images API returned 502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
			status := tc.status
			if status == 0 {
				status = http.StatusOK
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{Gateway: config.GatewayConfig{UpstreamResponseReadMaxBytes: tc.limit}}}
			account := &Account{ID: 1, Platform: PlatformCustom, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "private-key"}}
			err := svc.testOpenAIImageAPIKey(c, c.Request.Context(), account, "gpt-image-2", "draw")
			require.ErrorContains(t, err, tc.wantError)
			require.NotContains(t, rec.Body.String(), `"success":true`)
			require.NotContains(t, rec.Body.String(), "private-key")
			require.Len(t, upstream.requests, 1)
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: ") {
					require.True(t, json.Valid([]byte(strings.TrimPrefix(line, "data: "))))
				}
			}
		})
	}
}
