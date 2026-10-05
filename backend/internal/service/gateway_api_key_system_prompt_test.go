package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func apiKeyPromptTestContext(path string, force bool) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("api_key", &APIKey{ID: 73, CustomSystemPromptEnabled: true, CustomSystemPromptForce: force, CustomSystemPrompt: "  Answer in Chinese.\n"})
	return c
}

func apiKeyPromptTestConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}}
}

func TestAPIKeySystemPromptDefaultPreservesRawResponses(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			c := apiKeyPromptTestContext(path, false)
			body := []byte(`{"model":"gpt-5.5","instructions":"keep agent and tool guidance","previous_response_id":"resp_previous","vendor_number":9007199254740993,"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}],"input":[{"type":"function_call","call_id":"call_one","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_one","output":{"large":9007199254740993}},{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"},{"type":"input_text","text":"Inspect it."}]}]}`)
			got, err := applyAPIKeySystemPrompt(c, body, apiKeySystemPromptResponses)
			require.NoError(t, err)
			for _, field := range []string{"model", "instructions", "previous_response_id", "vendor_number", "tools", "input.0", "input.1"} {
				require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(got, field).Raw, field)
			}
			require.Contains(t, gjson.GetBytes(got, "input.2.content.0.text").String(), "Answer in Chinese.")
			require.Equal(t, gjson.GetBytes(body, "input.2.content.0").Raw, gjson.GetBytes(got, "input.2.content.1").Raw)
			require.Equal(t, gjson.GetBytes(body, "input.2.content.1").Raw, gjson.GetBytes(got, "input.2.content.2").Raw)
			again, err := applyAPIKeySystemPrompt(c, got, apiKeySystemPromptResponses)
			require.NoError(t, err)
			require.Equal(t, got, again, "retrying the same wire body must not repeat the prompt")
		})
	}
}

func TestAPIKeySystemPromptToolOnlyContinuationPreservesOrder(t *testing.T) {
	c := apiKeyPromptTestContext("/v1/responses", false)
	body := []byte(`{"model":"gpt-5.5","previous_response_id":"resp_previous","input":[{"type":"function_call_output","call_id":"call_one","output":"first"},{"type":"function_call_output","call_id":"call_two","output":"second"}]}`)
	got, err := applyAPIKeySystemPrompt(c, body, apiKeySystemPromptResponses)
	require.NoError(t, err)
	require.Equal(t, "resp_previous", gjson.GetBytes(got, "previous_response_id").String())
	for i := 0; i < 2; i++ {
		path := fmt.Sprintf("input.%d", i)
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(got, path).Raw)
	}
	require.Equal(t, "user", gjson.GetBytes(got, "input.2.role").String())
	require.Contains(t, gjson.GetBytes(got, "input.2.content").String(), "Answer in Chinese.")

	c = apiKeyPromptTestContext("/v1/messages", false)
	body = []byte(`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool_one","name":"read","input":{"large":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_one","content":"result"},{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]}`)
	got, err = applyAPIKeySystemPrompt(c, body, apiKeySystemPromptAnthropic)
	require.NoError(t, err)
	require.Equal(t, gjson.GetBytes(body, "messages.0").Raw, gjson.GetBytes(got, "messages.0").Raw)
	require.Equal(t, gjson.GetBytes(body, "messages.1.content.0").Raw, gjson.GetBytes(got, "messages.1.content.0").Raw)
	require.Contains(t, gjson.GetBytes(got, "messages.1.content.1.text").String(), "Answer in Chinese.")
	require.Equal(t, gjson.GetBytes(body, "messages.1.content.1").Raw, gjson.GetBytes(got, "messages.1.content.2").Raw)
}

func TestAPIKeySystemPromptForcedPreservesInstructionsAndBlocks(t *testing.T) {
	tests := []struct {
		protocol apiKeySystemPromptProtocol
		body     string
		field    string
	}{
		{apiKeySystemPromptResponses, `{"instructions":"agent tool guidance","input":"question","large":9007199254740993}`, "instructions"},
		{apiKeySystemPromptAnthropic, `{"system":"agent tool guidance","messages":[{"role":"user","content":"question"}],"large":9007199254740993}`, "system"},
		{apiKeySystemPromptAnthropic, `{"system":[{"type":"text","text":"agent tool guidance","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"question"}],"large":9007199254740993}`, "system.1.text"},
		{apiKeySystemPromptChat, `{"messages":[{"role":"system","content":"agent tool guidance"},{"role":"user","content":"question"}],"large":9007199254740993}`, "messages.1.content"},
	}
	for _, tt := range tests {
		t.Run(string(tt.protocol)+tt.field, func(t *testing.T) {
			c := apiKeyPromptTestContext("/v1/messages", true)
			got, err := applyAPIKeySystemPrompt(c, []byte(tt.body), tt.protocol)
			require.NoError(t, err)
			require.True(t, strings.HasSuffix(gjson.GetBytes(got, tt.field).String(), "  Answer in Chinese.\n"))
			require.Contains(t, string(got), "agent tool guidance")
			require.Equal(t, "9007199254740993", gjson.GetBytes(got, "large").Raw)
			if tt.field == "system.1.text" {
				require.Equal(t, gjson.Get(tt.body, "system.0").Raw, gjson.GetBytes(got, "system.0").Raw)
			}
			again, err := applyAPIKeySystemPrompt(c, got, tt.protocol)
			require.NoError(t, err)
			require.Equal(t, got, again)
		})
	}
}

func TestAPIKeySystemPromptDisabledAndUnrelatedEndpointAreTransparent(t *testing.T) {
	body := []byte(`{"input":"question","large":9007199254740993}`)
	for _, path := range []string{"/v1/responses", "/v1/chat/completions", "/v1/images/generations"} {
		c := apiKeyPromptTestContext(path, false)
		if path == "/v1/responses" {
			getAPIKeyFromContext(c).CustomSystemPromptEnabled = false
		}
		got, err := applyAPIKeySystemPrompt(c, body, apiKeySystemPromptResponses)
		require.NoError(t, err)
		require.Equal(t, body, got)
	}
}

func TestAPIKeySystemPromptDefaultStringInput(t *testing.T) {
	c := apiKeyPromptTestContext("/v1/responses", false)
	body := []byte(`{"input":"question","instructions":"agent tool guidance","vendor_number":9007199254740993}`)
	got, err := applyAPIKeySystemPrompt(c, body, apiKeySystemPromptResponses)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(gjson.GetBytes(got, "input").String(), "question"))
	require.Contains(t, gjson.GetBytes(got, "input").String(), "Answer in Chinese.")
	require.Equal(t, "agent tool guidance", gjson.GetBytes(got, "instructions").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "vendor_number").Raw)
}

func TestAPIKeySystemPromptForcedCodexPassthroughSatisfiesInstructionsCheck(t *testing.T) {
	c := apiKeyPromptTestContext("/v1/responses/compact", true)
	body := []byte(`{"model":"gpt-5.1-codex","instructions":"","input":"question"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cmp_prompt","usage":{"input_tokens":125,"output_tokens":3}}`))}}
	svc := &OpenAIGatewayService{cfg: apiKeyPromptTestConfig(), httpUpstream: upstream}
	account := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "acct"}, Extra: map[string]any{"openai_passthrough": true}}
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 125, result.Usage.InputTokens)
	require.Equal(t, "  Answer in Chinese.\n", gjson.GetBytes(upstream.lastBody, "instructions").String())
}

func TestAPIKeySystemPromptWireBuildersAfterOAuthMimicry(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			c := apiKeyPromptTestContext("/v1/responses", force)
			body := []byte(`{"model":"claude-sonnet-4-5","system":"original agent tool guidance","messages":[{"role":"user","content":"question"}]}`)
			body = rewriteSystemForNonClaudeCodeWithPromptBlocks(body, "original agent tool guidance", "", "")
			svc := &GatewayService{}
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}
			req, wire, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "token", "oauth", "claude-sonnet-4-5", true, true)
			require.NoError(t, err)
			read, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, wire, read)
			require.Contains(t, string(wire), "original agent tool guidance")
			require.Contains(t, string(wire), "Answer in Chinese.")
			if force {
				system := gjson.GetBytes(wire, "system").Array()
				require.Equal(t, "  Answer in Chinese.\n", system[len(system)-1].Get("text").String())
			} else {
				require.NotContains(t, gjson.GetBytes(wire, "system").Raw, "Answer in Chinese.")
			}
			_, retried, err := svc.buildUpstreamRequest(context.Background(), c, account, wire, "token", "oauth", "claude-sonnet-4-5", true, true)
			require.NoError(t, err)
			require.Equal(t, wire, retried)
		})
	}
}

func TestAPIKeySystemPromptOpenAIForwardWireAndUsage(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, passthrough := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				for _, force := range []bool{false, true} {
					name := fmt.Sprintf("%s/pass=%v/compact=%v/force=%v", accountType, passthrough, compact, force)
					t.Run(name, func(t *testing.T) {
						path := "/v1/responses"
						if compact {
							path += "/compact"
						}
						c := apiKeyPromptTestContext(path, force)
						body := []byte(`{"model":"gpt-5.5","stream":false,"instructions":"agent tool guidance","input":[{"role":"user","content":"question"}]}`)
						response := `{"id":"resp_prompt","object":"response","status":"completed","model":"gpt-5.5","output":[],"usage":{"input_tokens":125,"output_tokens":3,"input_tokens_details":{"cached_tokens":17}}}`
						contentType := "application/json"
						if accountType == AccountTypeOAuth && !compact {
							response = "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\ndata: [DONE]\n\n"
							contentType = "text/event-stream"
						}
						upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}}
						svc := &OpenAIGatewayService{cfg: apiKeyPromptTestConfig(), httpUpstream: upstream}
						account := &Account{ID: 19, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1,
							Credentials: map[string]any{"api_key": "key", "base_url": "http://upstream.test", "access_token": "token", "chatgpt_account_id": "acct"},
							Extra:       map[string]any{"openai_passthrough": passthrough, "openai_responses_mode": "responses"}}
						result, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Equal(t, 125, result.Usage.InputTokens)
						require.Equal(t, 3, result.Usage.OutputTokens)
						require.Equal(t, 17, result.Usage.CacheReadInputTokens)
						expectedPath := path
						if accountType == AccountTypeOAuth {
							expectedPath = "/backend-api/codex" + strings.TrimPrefix(path, "/v1")
						}
						require.Equal(t, expectedPath, upstream.lastReq.URL.Path)
						require.Contains(t, string(upstream.lastBody), "Answer in Chinese.")
						if force {
							require.Equal(t, "agent tool guidance\n\n  Answer in Chinese.\n", gjson.GetBytes(upstream.lastBody, "instructions").String())
							require.Equal(t, "question", gjson.GetBytes(upstream.lastBody, "input.0.content").String())
						} else {
							require.Equal(t, "agent tool guidance", gjson.GetBytes(upstream.lastBody, "instructions").String())
							require.True(t, strings.HasSuffix(gjson.GetBytes(upstream.lastBody, "input.0.content").String(), "question"))
						}
					})
				}
			}
		}
	}
}

func TestAPIKeySystemPromptMessagesBridgeUsesFinalResponsesInstructions(t *testing.T) {
	c := apiKeyPromptTestContext("/v1/messages", true)
	body := []byte(`{"model":"gpt-5.5","max_tokens":16,"system":"agent tool guidance","messages":[{"role":"user","content":"question"}],"stream":false}`)
	response := `data: {"type":"response.completed","response":{"id":"resp_prompt","object":"response","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":125,"output_tokens":3}}}` + "\n\ndata: [DONE]\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}}
	cfg := apiKeyPromptTestConfig()
	cfg.Gateway.ForcedCodexInstructionsTemplate = "server template\n\n{{ .ExistingInstructions }}"
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "acct"}}
	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "gpt-5.5")
	require.NoError(t, err)
	require.Equal(t, "server template\n\nagent tool guidance\n\n  Answer in Chinese.\n", gjson.GetBytes(upstream.lastBody, "instructions").String())
	require.Equal(t, 125, result.Usage.InputTokens)
}

func TestAPIKeySystemPromptResponsesBridgeRetainsAgentSystem(t *testing.T) {
	for _, force := range []bool{false, true} {
		c := apiKeyPromptTestContext("/v1/responses", force)
		var input apicompat.ResponsesRequest
		require.NoError(t, json.Unmarshal([]byte(`{"model":"claude-sonnet-4-5","instructions":"agent instructions","input":[{"role":"developer","content":"agent tool guidance"},{"role":"user","content":"question"}]}`), &input))
		converted, err := apicompat.ResponsesToAnthropicRequest(&input)
		require.NoError(t, err)
		body, err := json.Marshal(converted)
		require.NoError(t, err)
		account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}
		svc := &GatewayService{cfg: apiKeyPromptTestConfig()}
		req, wire, err := svc.buildUpstreamRequest(context.Background(), c, account, body, "key", "apikey", input.Model, false, false)
		require.NoError(t, err)
		actual, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, wire, actual)
		require.Contains(t, gjson.GetBytes(actual, "system").String(), "agent instructions\n\nagent tool guidance")
		require.Contains(t, string(actual), "Answer in Chinese.")
		require.True(t, bytes.Contains(actual, []byte("question")))
	}
}

func TestAPIKeySystemPromptAnthropicPassthroughWireBody(t *testing.T) {
	for _, force := range []bool{false, true} {
		c := apiKeyPromptTestContext("/v1/messages", force)
		body := []byte(`{"model":"claude-sonnet-4-5","system":[{"type":"text","text":"agent tool guidance","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"question"}],"vendor_number":9007199254740993}`)
		svc := &GatewayService{cfg: apiKeyPromptTestConfig()}
		account := &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
		req, wire, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "key")
		require.NoError(t, err)
		actual, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, wire, actual)
		require.Equal(t, gjson.GetBytes(body, "system.0").Raw, gjson.GetBytes(actual, "system.0").Raw)
		require.Equal(t, "9007199254740993", gjson.GetBytes(actual, "vendor_number").Raw)
		require.Contains(t, string(actual), "Answer in Chinese.")
		if force {
			require.Equal(t, "question", gjson.GetBytes(actual, "messages.0.content").String())
		}
	}
}

func TestAPIKeySystemPromptChatFallbackWireBody(t *testing.T) {
	for _, force := range []bool{false, true} {
		c := apiKeyPromptTestContext("/v1/responses", force)
		upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
		body := []byte(`{"model":"chat-only","messages":[{"role":"system","content":"agent tool guidance"},{"role":"assistant","tool_calls":[{"id":"call_one","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_one","content":"result"}],"vendor_number":9007199254740993}`)
		resp, err := svc.sendCCUpstreamRequest(context.Background(), c, account, "http://upstream.test/v1/chat/completions", body, false, "key", "", "")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, "9007199254740993", gjson.GetBytes(upstream.lastBody, "vendor_number").Raw)
		require.Contains(t, string(upstream.lastBody), "agent tool guidance")
		require.Contains(t, string(upstream.lastBody), "Answer in Chinese.")
		require.Contains(t, string(upstream.lastBody), `"tool_call_id":"call_one"`)
		require.Contains(t, string(upstream.lastBody), `"tool_calls"`)
	}
}

func TestAPIKeySystemPromptWebSocketPassthroughEveryTurn(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			controlCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
			account := passthroughLifecycleAccount()
			serverErr := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverErr <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, first, err := conn.Read(controlCtx)
				if err != nil {
					serverErr <- err
					return
				}
				c := apiKeyPromptTestContext("/v1/responses", force)
				c.Request = r.Clone(controlCtx)
				serverErr <- svc.ProxyResponsesWebSocketFromClient(controlCtx, c, conn, account, "key", first, nil)
			}))
			defer server.Close()
			client, _, err := coderws.Dial(controlCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
			require.NoError(t, err)
			defer func() { _ = client.CloseNow() }()
			first := []byte(`{"type":"response.create","model":"gpt-5.5","instructions":"agent tool guidance","input":[{"role":"user","content":"question"}]}`)
			require.NoError(t, client.Write(controlCtx, coderws.MessageText, first))
			wire := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
			require.Contains(t, string(wire), "Answer in Chinese.")
			require.Equal(t, 1, strings.Count(string(wire), "Answer in Chinese."))
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.5","status":"completed","output":[],"usage":{"input_tokens":125,"output_tokens":3}}}`)
			_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
			require.NoError(t, err)
			next := []byte(`{"type":"response.create","instructions":"agent tool guidance","previous_response_id":"resp_first","input":[{"type":"function_call_output","call_id":"call_one","output":{"large":9007199254740993}}]}`)
			require.NoError(t, client.Write(controlCtx, coderws.MessageText, next))
			wire = requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
			require.Equal(t, "resp_first", gjson.GetBytes(wire, "previous_response_id").String())
			require.Equal(t, gjson.GetBytes(next, "input.0").Raw, gjson.GetBytes(wire, "input.0").Raw)
			require.Equal(t, 1, strings.Count(string(wire), "Answer in Chinese."))
			if force {
				require.Equal(t, "agent tool guidance\n\n  Answer in Chinese.\n", gjson.GetBytes(wire, "instructions").String())
				require.Len(t, gjson.GetBytes(wire, "input").Array(), 1)
			} else {
				require.Equal(t, "user", gjson.GetBytes(wire, "input.1.role").String())
			}
			cancel()
			_ = client.CloseNow()
			select {
			case <-serverErr:
			case <-time.After(3 * time.Second):
				t.Fatal("websocket forwarding did not stop")
			}
		})
	}
}
