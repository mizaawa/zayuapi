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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIReasoningEffortUsagePreservesExplicitMax(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-5.6-sol", "deepseek-v4-pro", "custom-model", ""} {
		t.Run(model, func(t *testing.T) {
			for _, body := range []string{`{"reasoning":{"effort":"max"}}`, `{"reasoning_effort":" MAX "}`} {
				var reqBody map[string]any
				require.NoError(t, json.Unmarshal([]byte(body), &reqBody))
				for _, got := range []*string{
					extractOpenAIReasoningEffortFromBody([]byte(body), model),
					extractOpenAIReasoningEffort(reqBody, model),
				} {
					require.NotNil(t, got)
					require.Equal(t, "max", *got)
				}
			}
		})
	}
}

func TestOpenAIGatewayForwardReasoningEffortUsageMatchesWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name        string
		accountType string
		ceiling     string
		effort      string
		want        string
	}{
		{name: "api_key_max", accountType: AccountTypeAPIKey, want: "max"},
		{name: "oauth_max", accountType: AccountTypeOAuth, want: "max"},
		{name: "legacy_group_cap_is_ignored", accountType: AccountTypeAPIKey, ceiling: "xhigh", want: "max"},
		{name: "api_key_future_effort", accountType: AccountTypeAPIKey, effort: "future-level", ceiling: "low", want: "future-level"},
		{name: "oauth_future_effort", accountType: AccountTypeOAuth, effort: "future-level", ceiling: "low", want: "future-level"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":1,"output_tokens":2}}`)),
			}}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{
				ID: 12, Name: "effort-usage-test", Platform: PlatformOpenAI, Type: tt.accountType,
				Concurrency: 1, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{
					"api_key": "sk-test", "base_url": "https://example.com",
					"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc",
				},
				Extra: map[string]any{"use_responses_api": true},
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			effort := tt.effort
			if effort == "" {
				effort = "max"
			}
			body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": false, "instructions": "test", "input": "hello", "reasoning": map[string]any{"effort": effort}})
			require.NoError(t, err)
			body, _ = ApplyOpenAIReasoningEffortPolicy(body, tt.ceiling, nil)

			result, err := svc.Forward(context.Background(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.want, gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
			require.NotNil(t, result.ReasoningEffort)
			require.Equal(t, tt.want, *result.ReasoningEffort)
		})
	}
}
