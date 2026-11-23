//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// swapMonitorHTTPClient 临时替换 monitorHTTPClient 为不带 SSRF 校验的普通 client，
// 让 httptest (127.0.0.1) 能连通。测试结束后恢复。
func swapMonitorHTTPClient(t *testing.T) {
	t.Helper()
	orig := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = orig })
}

// captureHandler 把每次收到的请求 body 和 headers 存起来，测试断言用。
type captureHandler struct {
	lastBody    map[string]any
	lastHeaders http.Header
	respondText string // 写到 Anthropic content[0].text 里（校验用）
	status      int
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	defer func() { _ = r.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	h.lastBody = parsed

	if h.status == 0 {
		h.status = 200
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)
	// 构造 Anthropic 格式的响应：content[0].text = h.respondText
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": h.respondText},
		},
	})
}

func setupFakeAnthropic(t *testing.T, handler *captureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

type openAICaptureHandler struct {
	lastBody                  map[string]any
	lastHeaders               http.Header
	lastPath                  string
	status                    int
	rawResponse               string
	responsesLeadingReasoning bool
}

func (h *openAICaptureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	h.lastPath = r.URL.Path
	defer func() { _ = r.Body.Close() }()
	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	h.lastBody = parsed

	if h.status == 0 {
		h.status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(h.status)
	if h.rawResponse != "" {
		_, _ = w.Write([]byte(h.rawResponse))
		return
	}

	answer := answerFromOpenAIRequest(parsed)
	if h.lastPath == providerOpenAIResponsesPath {
		output := []map[string]any{}
		if h.responsesLeadingReasoning {
			output = append(output, map[string]any{
				"type":    "reasoning",
				"summary": []any{},
			})
		}
		output = append(output, map[string]any{
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": answer},
			},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": output,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": answer}}},
	})
}

func setupFakeOpenAI(t *testing.T, handler *openAICaptureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func answerFromOpenAIRequest(body map[string]any) string {
	prompt, _ := body["input"].(string)
	if prompt == "" {
		if messages, ok := body["messages"].([]any); ok && len(messages) > 0 {
			if msg, ok := messages[0].(map[string]any); ok {
				prompt, _ = msg["content"].(string)
			}
		}
	}
	return answerFromChallengePrompt(prompt)
}

var challengeQuestionRegex = regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?\nA:$`)

func answerFromChallengePrompt(prompt string) string {
	m := challengeQuestionRegex.FindStringSubmatch(prompt)
	if len(m) != 4 {
		return "0"
	}
	left, _ := strconv.Atoi(m[1])
	right, _ := strconv.Atoi(m[3])
	if m[2] == "+" {
		return strconv.Itoa(left + right)
	}
	return strconv.Itoa(left - right)
}

func TestRunCheckForModel_OffMode_PreservesDefaultBody(t *testing.T) {
	h := &captureHandler{respondText: "the answer is 42"}
	endpoint := setupFakeAnthropic(t, h)

	// 跑一次 off 模式（opts=nil），确认默认 body 行为未变
	_ = runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", nil)

	if h.lastBody["model"] != "claude-x" {
		t.Errorf("default body should contain model=claude-x, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("default body should contain messages")
	}
	if h.lastHeaders.Get("x-api-key") != "sk-fake" {
		t.Errorf("expected adapter's x-api-key header, got %q", h.lastHeaders.Get("x-api-key"))
	}
}

func TestRunCheckForModel_OpenAI_DefaultChatRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("default chat request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIPath {
		t.Fatalf("expected chat completions path %q, got %q", providerOpenAIPath, h.lastPath)
	}
	if h.lastBody["model"] != "gpt-test" {
		t.Errorf("chat body should contain model=gpt-test, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("chat body should contain messages")
	}
	if _, ok := h.lastBody["instructions"]; ok {
		t.Error("chat body must not contain top-level instructions")
	}
	if h.lastBody["stream"] != true {
		t.Errorf("chat body should set stream=true, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer sk-openai" {
		t.Errorf("expected bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestGrokMonitorConfiguration(t *testing.T) {
	if err := validateProvider(MonitorProviderGrok); err != nil {
		t.Fatalf("grok provider should be supported: %v", err)
	}
	if got := normalizeMonitorPrimaryModel(MonitorProviderGrok, ""); got != MonitorDefaultGrokModel {
		t.Fatalf("expected default Grok model %q, got %q", MonitorDefaultGrokModel, got)
	}
	if err := validateAPIMode(MonitorProviderGrok, MonitorAPIModeChatCompletions); err != nil {
		t.Fatalf("grok chat_completions mode should be valid: %v", err)
	}
	if err := validateAPIMode(MonitorProviderGrok, MonitorAPIModeResponses); err == nil {
		t.Fatal("grok responses mode should be rejected by channel monitoring")
	}
	if err := validateReplaceRequestBody(MonitorProviderGrok, MonitorAPIModeChatCompletions, map[string]any{}); err == nil {
		t.Fatal("grok replace-mode body should require messages")
	}
}

func TestRunCheckForModel_Grok_DefaultChatRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderGrok, endpoint, "xai-key", MonitorDefaultGrokModel, nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("Grok request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if res.DurationMs == nil || res.LatencyMs != nil {
		t.Fatal("buffered JSON has elapsed time, but no measured first token")
	}
	if h.lastPath != providerGrokPath {
		t.Fatalf("expected Grok chat completions path %q, got %q", providerGrokPath, h.lastPath)
	}
	if h.lastBody["model"] != MonitorDefaultGrokModel {
		t.Errorf("Grok body should contain model=%s, got %v", MonitorDefaultGrokModel, h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("Grok body should contain messages")
	}
	if h.lastBody["stream"] != true {
		t.Errorf("Grok body should set stream=true, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer xai-key" {
		t.Errorf("expected Grok bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_Grok_UpstreamFailure(t *testing.T) {
	h := &openAICaptureHandler{status: http.StatusTooManyRequests}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderGrok, endpoint, "xai-key", MonitorDefaultGrokModel, nil)

	if res.Status != MonitorStatusError {
		t.Fatalf("Grok 429 should be recorded as error, got status=%s message=%q", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "upstream HTTP 429") {
		t.Fatalf("Grok failure should preserve upstream status, got %q", res.Message)
	}
	if res.DurationMs == nil {
		t.Fatal("Grok failure should still record latency")
	}
}

func TestRunCheckForModel_Grok_RedactsXAIKeyFromUpstreamBody(t *testing.T) {
	h := &openAICaptureHandler{
		status:      http.StatusUnauthorized,
		rawResponse: `{"error":{"message":"invalid API key xai-secret"}}`,
	}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderGrok, endpoint, "request-key", MonitorDefaultGrokModel, nil)

	if res.Status != MonitorStatusError {
		t.Fatalf("Grok upstream failure should be recorded as error, got %s", res.Status)
	}
	if strings.Contains(res.Message, "xai-secret") {
		t.Fatalf("Grok error message leaked xAI key: %q", res.Message)
	}
	if !strings.Contains(res.Message, "xai-***REDACTED***") {
		t.Fatalf("Grok error message should contain redaction marker, got %q", res.Message)
	}
}

func TestRunCheckForModel_RedactsExactUnknownPrefixKeyFromUpstreamBody(t *testing.T) {
	const key = "vendor-secret-with-unknown-prefix-123456"
	h := &openAICaptureHandler{
		status:      http.StatusUnauthorized,
		rawResponse: `{"error":{"message":"credential ` + key + ` rejected"}}`,
	}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderCustom, endpoint, key, "custom-model", nil)

	if res.Status != MonitorStatusError {
		t.Fatalf("custom upstream failure should be recorded as error, got %s", res.Status)
	}
	if strings.Contains(res.Message, key) {
		t.Fatalf("custom error message leaked the exact request key: %q", res.Message)
	}
	if !strings.Contains(res.Message, "***REDACTED***") {
		t.Fatalf("custom error message should contain redaction marker, got %q", res.Message)
	}
}

func TestRunCheckForModel_RedactsCustomHeaderValueFromUpstreamBody(t *testing.T) {
	const headerSecret = "vendor-private-header-token-123456"
	h := &openAICaptureHandler{
		status:      http.StatusUnauthorized,
		rawResponse: `{"error":{"message":"credential ` + headerSecret + ` rejected"}}`,
	}
	endpoint := setupFakeOpenAI(t, h)
	opts := &CheckOptions{ExtraHeaders: map[string]string{
		"X-Custom-Token": headerSecret,
	}}

	res := runCheckForModel(context.Background(), MonitorProviderCustom, endpoint, "custom-key", "custom-model", opts)

	if res.Status != MonitorStatusError {
		t.Fatalf("custom upstream failure should be recorded as error, got %s", res.Status)
	}
	if strings.Contains(res.Message, headerSecret) {
		t.Fatalf("custom error message leaked a custom header credential: %q", res.Message)
	}
	if !strings.Contains(res.Message, "***REDACTED***") {
		t.Fatalf("custom error message should contain redaction marker, got %q", res.Message)
	}
}

func TestRunCheckForModel_RedactsCredentialBeforeErrorBodyTruncation(t *testing.T) {
	const key = "unknown-prefix-secret-1234567890"
	const truncationMarker = "...(body truncated)"
	prefix := strings.Repeat("a", monitorErrorBodySnippetMaxBytes-len(truncationMarker)-8)
	h := &openAICaptureHandler{
		status:      http.StatusUnauthorized,
		rawResponse: prefix + key + " rejected",
	}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderCustom, endpoint, key, "custom-model", nil)

	if res.Status != MonitorStatusError {
		t.Fatalf("custom upstream failure should be recorded as error, got %s", res.Status)
	}
	if strings.Contains(res.Message, key[:8]) {
		t.Fatalf("custom error message leaked a key fragment at the truncation boundary: %q", res.Message)
	}
}

func TestSanitizeErrorMessageWithSecretsRedactsEncodedCredential(t *testing.T) {
	const key = `vendor<token>&"123`
	encoded, err := json.Marshal(key)
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	escaped := string(encoded[1 : len(encoded)-1])

	got := sanitizeErrorMessageWithSecrets("credential "+escaped+" rejected", key)

	if strings.Contains(got, escaped) || strings.Contains(got, "vendor") {
		t.Fatalf("encoded credential was not redacted: %q", got)
	}
}

func TestSanitizeErrorMessageWithSecretsRedactsOverlappingCredentialsLongestFirst(t *testing.T) {
	const longSecret = "shared-prefix-private-suffix"

	got := sanitizeErrorMessageWithSecrets("credential "+longSecret+" rejected", "shared-prefix", longSecret)

	if strings.Contains(got, "private-suffix") || strings.Contains(got, longSecret) {
		t.Fatalf("overlapping credential left a suffix behind: %q", got)
	}
}

func TestMonitorHTTPClientsRefuseRedirects(t *testing.T) {
	client := newSSRFSafeHTTPClient(time.Second)
	if client.CheckRedirect == nil {
		t.Fatal("external monitor client must install a redirect policy")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("external monitor redirect policy returned %v", err)
	}
}

func TestRunCheckForModel_DoesNotForwardCredentialAcrossRedirect(t *testing.T) {
	const key = "redirect-secret-key"
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != key {
			t.Errorf("source credential = %q, want %q", got, key)
		}
		http.Redirect(w, r, target.URL+providerAnthropicPath, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)

	originalClient := monitorHTTPClient
	monitorHTTPClient = &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: refuseMonitorRedirect,
	}
	t.Cleanup(func() { monitorHTTPClient = originalClient })

	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, source.URL, key, "claude-test", nil)

	if res.Status != MonitorStatusError || !strings.Contains(res.Message, "upstream HTTP 307") {
		t.Fatalf("redirect should be reported as an upstream error, got status=%s message=%q", res.Status, res.Message)
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target received %d requests; credential could have been forwarded", targetRequests)
	}
}

func TestCustomMonitorConfiguration(t *testing.T) {
	if err := validateProvider(MonitorProviderCustom); err != nil {
		t.Fatalf("custom provider should be supported: %v", err)
	}
	if err := validateAPIMode(MonitorProviderCustom, MonitorAPIModeChatCompletions); err != nil {
		t.Fatalf("custom chat_completions mode should be valid: %v", err)
	}
	if err := validateAPIMode(MonitorProviderCustom, MonitorAPIModeResponses); err != nil {
		t.Fatalf("custom responses mode should be valid: %v", err)
	}
	if err := validateReplaceRequestBody(MonitorProviderCustom, MonitorAPIModeChatCompletions, map[string]any{}); err == nil {
		t.Fatal("custom replace-mode body should require messages")
	}
}

func TestRunCheckForModel_Custom_DefaultChatRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderCustom, endpoint, "custom-key", "custom-model", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("custom request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerCustomPath {
		t.Fatalf("expected custom chat completions path %q, got %q", providerCustomPath, h.lastPath)
	}
	if h.lastBody["model"] != "custom-model" {
		t.Errorf("custom body should contain model=custom-model, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["messages"]; !ok {
		t.Error("custom body should contain messages")
	}
	if h.lastBody["stream"] != true {
		t.Errorf("custom body should set stream=true, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer custom-key" {
		t.Errorf("expected custom bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_Custom_ResponsesRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderCustom, endpoint, "custom-key", "custom-model", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("custom responses request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIResponsesPath {
		t.Fatalf("custom responses path = %q, want %q", h.lastPath, providerOpenAIResponsesPath)
	}
	if h.lastBody["model"] != "custom-model" {
		t.Errorf("custom responses body should contain model=custom-model, got %v", h.lastBody["model"])
	}
	if _, ok := h.lastBody["input"]; !ok {
		t.Error("custom responses body should contain input")
	}
	if h.lastHeaders.Get("Authorization") != "Bearer custom-key" {
		t.Errorf("expected custom bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_CustomManagedGatewaySignsLocalChatRequest(t *testing.T) {
	attestor, err := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	if err != nil {
		t.Fatalf("create monitor attestor: %v", err)
	}

	var gotPath string
	var gotAuthorization string
	var signatureValid bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		signatureValid = attestor.ValidateRequest(r, "custom-key")

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": answerFromOpenAIRequest(body)}}},
		})
	}))
	t.Cleanup(server.Close)

	originalClient := monitorInternalHTTPClient
	monitorInternalHTTPClient = server.Client()
	t.Cleanup(func() { monitorInternalHTTPClient = originalClient })

	result := runCheckForModel(context.Background(), MonitorProviderCustom, server.URL, "custom-key", "custom-model", &CheckOptions{
		ManagedGateway:  true,
		ManagedAttestor: attestor,
	})

	if result.Status != MonitorStatusOperational {
		t.Fatalf("managed custom request should pass challenge, got status=%s message=%q", result.Status, result.Message)
	}
	if gotPath != providerCustomPath {
		t.Fatalf("managed custom request path = %q, want %q", gotPath, providerCustomPath)
	}
	if gotAuthorization != "Bearer custom-key" {
		t.Fatalf("managed custom authorization = %q", gotAuthorization)
	}
	if !signatureValid {
		t.Fatal("managed custom request did not carry a valid monitor attestation")
	}
}

func TestRunCheckForModel_OpenAIResponses_DefaultRequest(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("default responses request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIResponsesPath {
		t.Fatalf("expected responses path %q, got %q", providerOpenAIResponsesPath, h.lastPath)
	}
	if h.lastBody["model"] != "gpt-test" {
		t.Errorf("responses body should contain model=gpt-test, got %v", h.lastBody["model"])
	}
	instructions, _ := h.lastBody["instructions"].(string)
	if strings.TrimSpace(instructions) == "" {
		t.Error("responses body should contain non-empty instructions")
	}
	input, _ := h.lastBody["input"].(string)
	if strings.TrimSpace(input) == "" {
		t.Error("responses body should contain non-empty input")
	}
	if _, ok := h.lastBody["messages"]; ok {
		t.Error("responses body must not contain chat messages")
	}
	if h.lastBody["stream"] != true {
		t.Errorf("responses body should set stream=true, got %v", h.lastBody["stream"])
	}
	if h.lastHeaders.Get("Authorization") != "Bearer sk-openai" {
		t.Errorf("expected bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
}

func TestRunCheckForModel_OpenAIResponses_SkipsLeadingReasoningItem(t *testing.T) {
	h := &openAICaptureHandler{responsesLeadingReasoning: true}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-5.5", &CheckOptions{
		APIMode: MonitorAPIModeResponses,
	})

	if res.Status != MonitorStatusOperational {
		t.Fatalf("responses request should find text after leading reasoning item, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != providerOpenAIResponsesPath {
		t.Fatalf("expected responses path %q, got %q", providerOpenAIResponsesPath, h.lastPath)
	}
}

func TestRunCheckForModel_OpenAIResponsesReplaceMissingInstructionsFailsLocally(t *testing.T) {
	h := &openAICaptureHandler{}
	endpoint := setupFakeOpenAI(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderOpenAI, endpoint, "sk-openai", "gpt-test", &CheckOptions{
		APIMode:          MonitorAPIModeResponses,
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride: map[string]any{
			"model": "gpt-test",
			"input": "hello",
		},
	})

	if res.Status != MonitorStatusError {
		t.Fatalf("invalid responses replace body should fail locally as error, got status=%s", res.Status)
	}
	if !strings.Contains(res.Message, "instructions and input are required") {
		t.Errorf("expected local validation message about instructions/input, got %q", res.Message)
	}
	if h.lastPath != "" {
		t.Errorf("invalid replace body should fail before HTTP request, got path %q", h.lastPath)
	}
}

func TestRunCheckForModel_MergeMode_UserFieldsWinButDenyListProtects(t *testing.T) {
	h := &captureHandler{respondText: "the answer is 42"}
	endpoint := setupFakeAnthropic(t, h)

	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]any{
			"system":     "You are Claude Code...",
			"max_tokens": float64(999),   // 应该覆盖默认 50
			"model":      "hacked-model", // 应该被黑名单挡住，保留原 model
			"messages":   []any{},        // 同上，被挡
		},
		ExtraHeaders: map[string]string{
			"User-Agent":     "claude-cli/1.0",
			"Content-Length": "999", // 黑名单
			"AUTHORIZATION":  "Bearer attacker-controlled",
			"X-API-KEY":      "attacker-controlled",
			"x-goog-api-key": "attacker-controlled",
			"x-custom":       "ok",
		},
	}
	_ = runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	if h.lastBody["system"] != "You are Claude Code..." {
		t.Errorf("merge mode should inject system, got %v", h.lastBody["system"])
	}
	// max_tokens 覆盖生效
	if mt, ok := h.lastBody["max_tokens"].(float64); !ok || mt != 999 {
		t.Errorf("merge mode should override max_tokens to 999, got %v", h.lastBody["max_tokens"])
	}
	// model 在黑名单 — 应该保留默认值
	if h.lastBody["model"] != "claude-x" {
		t.Errorf("model should be protected by deny list, got %v", h.lastBody["model"])
	}
	// messages 在黑名单 — 应该保留默认值（非空）
	msgs, _ := h.lastBody["messages"].([]any)
	if len(msgs) == 0 {
		t.Error("messages should be protected by deny list (kept default, non-empty)")
	}
	// header 合并
	if h.lastHeaders.Get("User-Agent") != "claude-cli/1.0" {
		t.Errorf("extra User-Agent should override, got %q", h.lastHeaders.Get("User-Agent"))
	}
	if h.lastHeaders.Get("x-custom") != "ok" {
		t.Errorf("extra custom header should be present, got %q", h.lastHeaders.Get("x-custom"))
	}
	if h.lastHeaders.Get("x-api-key") != "sk-fake" {
		t.Errorf("managed x-api-key must not be overridden, got %q", h.lastHeaders.Get("x-api-key"))
	}
	if got := h.lastHeaders.Get("Authorization"); got != "" {
		t.Errorf("unexpected authorization override reached upstream: %q", got)
	}
	if got := h.lastHeaders.Get("x-goog-api-key"); got != "" {
		t.Errorf("unexpected Google auth override reached upstream: %q", got)
	}
	// Content-Length 黑名单：会被 net/http 自动重算，但不应由用户的 "999" 决定。
	// 我们无法直接断言丢弃（http.Client 总会填上），只断言请求成功即可。
}

func TestRunCheckForModel_ReplaceMode_FullBodyUsedAndChallengeSkipped(t *testing.T) {
	// replace 模式下我们的 body 完全自定义，challenge 数学题不会出现在请求里，
	// 上游也不会回正确答案 — 但只要 2xx + 响应文本非空，就算 operational
	h := &captureHandler{respondText: "any non-empty text"}
	endpoint := setupFakeAnthropic(t, h)

	userBody := map[string]any{
		"model":      "user-forced-model",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens": float64(10),
		"system":     "You are someone else",
	}
	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride:     userBody,
	}
	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	// 请求 body = 用户提供的原样
	if h.lastBody["model"] != "user-forced-model" {
		t.Errorf("replace mode should use user's model, got %v", h.lastBody["model"])
	}
	if h.lastBody["system"] != "You are someone else" {
		t.Errorf("replace mode should use user's system, got %v", h.lastBody["system"])
	}
	// challenge 虽然没命中，但由于 replace 模式跳过 challenge 校验 + 响应非空 → operational
	if res.Status != MonitorStatusOperational {
		t.Errorf("replace mode with 2xx + non-empty text should be operational, got status=%s message=%q",
			res.Status, res.Message)
	}
}

func TestRunCheckForModel_ReplaceMode_EmptyResponseIsFailed(t *testing.T) {
	h := &captureHandler{respondText: ""} // 上游 200 但 content[0].text 为空
	endpoint := setupFakeAnthropic(t, h)

	opts := &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride:     map[string]any{"model": "x", "messages": []any{}},
	}
	res := runCheckForModel(context.Background(), MonitorProviderAnthropic, endpoint, "sk-fake", "claude-x", opts)

	if res.Status != MonitorStatusFailed {
		t.Errorf("replace mode with empty text should be failed, got status=%s", res.Status)
	}
	if !strings.Contains(res.Message, "replace-mode") {
		t.Errorf("failure message should hint replace-mode, got %q", res.Message)
	}
}

func TestExtractAnthropicMonitorText(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "text block after thinking",
			body: `{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"2"}]}`,
			want: "2",
		},
		{
			name: "single text block",
			body: `{"content":[{"type":"text","text":"2"}]}`,
			want: "2",
		},
		{
			name: "thinking only",
			body: `{"content":[{"type":"thinking","thinking":""}]}`,
			want: "",
		},
		{
			name: "multiple text blocks",
			body: `{"content":[{"type":"text","text":"answer"},{"type":"tool_use","name":"x"},{"type":"text","text":"2"}]}`,
			want: "answer\n2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAnthropicMonitorText([]byte(tt.body))
			if got != tt.want {
				t.Fatalf("extractAnthropicMonitorText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateChallenge_AnthropicTextAfterThinking(t *testing.T) {
	body := []byte(`{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"答案是 2"}]}`)
	respText := extractAnthropicMonitorText(body)

	if !validateChallenge(respText, "2") {
		t.Fatalf("validateChallenge(%q, %q) = false, want true", respText, "2")
	}
}
