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
	type forwardCase struct {
		name        string
		accountType string
		ceiling     string
		effort      string
		want        string
		model       string
		codex       bool
		passthrough bool
	}
	tests := []forwardCase{
		{name: "api_key_max", accountType: AccountTypeAPIKey, want: "max"},
		{name: "oauth_max", accountType: AccountTypeOAuth, want: "max"},
		{name: "legacy_group_cap_is_ignored", accountType: AccountTypeAPIKey, ceiling: "xhigh", want: "max"},
		{name: "api_key_future_effort", accountType: AccountTypeAPIKey, effort: "future-level", ceiling: "low", want: "future-level"},
		{name: "oauth_future_effort", accountType: AccountTypeOAuth, effort: "future-level", ceiling: "low", want: "future-level"},
	}
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, passthrough := range []bool{false, true} {
			mode := "normal"
			if passthrough {
				mode = "passthrough"
			}
			for _, effort := range []string{"max", "xhigh", "ultra"} {
				tests = append(tests, forwardCase{
					name:        "codex_gpt6_sol_" + accountType + "_" + mode + "_" + effort,
					accountType: accountType, effort: effort, want: effort,
					model: "gpt-6-sol", codex: true, passthrough: passthrough,
				})
			}
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentType := "application/json"
			responseBody := `{"usage":{"input_tokens":1,"output_tokens":2}}`
			if tt.codex {
				contentType = "text/event-stream"
				responseBody = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-effort\",\"model\":\"gpt-6-sol\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n" +
					"data: [DONE]\n\n"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{contentType}},
				Body:       io.NopCloser(strings.NewReader(responseBody)),
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
				Extra: map[string]any{"use_responses_api": true, "openai_passthrough": tt.passthrough},
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
			if tt.codex {
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
				c.Request.Header.Set("originator", "codex_cli_rs")
			}
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			effort := tt.effort
			if effort == "" {
				effort = "max"
			}
			model := tt.model
			if model == "" {
				model = "gpt-6-astra"
			}
			body, err := json.Marshal(map[string]any{"model": model, "stream": tt.codex, "instructions": "test", "input": "hello", "reasoning": map[string]any{"effort": effort}})
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
