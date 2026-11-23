package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const geminiTestReceipt = `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"thought":true,"text":"internal"},{"text":"answer"}]}}],"modelVersion":"gemini-meter","usageMetadata":{"promptTokenCount":20,"cachedContentTokenCount":5,"toolUsePromptTokenCount":3,"candidatesTokenCount":6,"thoughtsTokenCount":4}}`

func TestGeminiMonitorNativeRequestAndReceipt(t *testing.T) {
	path, body, headers, err := buildDegradationRequest(degradationRequest{Model: "gemini-3-pro", Protocol: DegradationProtocolGemini, Prompt: "draw", Effort: "high", APIKey: "fixture"})
	require.NoError(t, err)
	require.Equal(t, "/v1beta/models/gemini-3-pro:streamGenerateContent?alt=sse", path)
	require.Equal(t, "fixture", headers["x-goog-api-key"])
	require.Equal(t, "draw", gjson.GetBytes(body, "contents.0.parts.0.text").String())
	require.Equal(t, "HIGH", gjson.GetBytes(body, "generationConfig.thinkingConfig.thinkingLevel").String())
	require.Equal(t, "answer", extractGeminiMonitorText([]byte(geminiTestReceipt)))
	for _, stream := range []bool{false, true} {
		s := newDegradationStreamState(time.Now())
		if stream {
			err = s.handle(DegradationProtocolGemini, "", []byte(geminiTestReceipt))
		} else {
			err = s.consumeJSON(DegradationProtocolGemini, strings.NewReader(geminiTestReceipt))
		}
		require.NoError(t, err)
		require.NoError(t, s.outcome(t.Context(), time.Minute, nil))
		require.Equal(t, "answer", s.output.String())
		require.EqualValues(t, 18, s.usage.InputTokens)
		require.EqualValues(t, 10, s.usage.OutputTokens)
		require.EqualValues(t, 5, s.usage.CacheReadTokens)
		require.EqualValues(t, 4, s.reasoningTokens)
	}
}

func TestGeminiCumulativeMeterKeepsLastValidComponents(t *testing.T) {
	var meter geminiUsageMeter
	meter.observe([]byte(geminiTestReceipt))
	for _, body := range []string{`{"usageMetadata":{"candidatesTokenCount":8}}`, `{"usageMetadata":{"thoughtsTokenCount":4}}`, `{"usageMetadata":null}`, `{"usageMetadata":{"cachedContentTokenCount":-1}}`} {
		meter.observe([]byte(body))
	}
	require.Equal(t, ClaudeUsage{InputTokens: 18, CacheReadInputTokens: 5, OutputTokens: 12}, meter.usage)
}

func TestGeminiRelayFollowsVerifiedProtocolAndSwitchesBack(t *testing.T) {
	account := &Account{ID: 1402, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	var paths []string
	nativeOnly := false
	do := func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		chat := strings.HasSuffix(r.URL.Path, "/chat/completions")
		status := 404
		body := "{}"
		if chat {
			require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
			require.Empty(t, r.Header.Get("x-goog-api-key"))
			b, _ := io.ReadAll(r.Body)
			require.Equal(t, "hi", gjson.GetBytes(b, "messages.0.content").String())
			require.False(t, gjson.GetBytes(b, "stream").Bool())
			if !nativeOnly {
				status = 200
				body = `{"model":"gemini-meter","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":4}}}`
			}
		} else if nativeOnly {
			status = 200
			body = geminiTestReceipt
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	const native = "/v1beta/models/gemini-meter:generateContent"
	const chat = "/v1/chat/completions"
	for i, want := range [][]string{{native, chat}, {chat}, {chat, native}, {native}} {
		paths = nil
		if i == 2 {
			nativeOnly = true
		}
		req, err := http.NewRequestWithContext(t.Context(), "POST", "https://relay.test"+native, strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
		require.NoError(t, err)
		r, err := geminiRelayRequest(account, "gemini-meter", req, do)
		require.NoError(t, err)
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = r.Body.Close()
		require.Equal(t, want, paths)
		u := extractGeminiUsage(b)
		require.NotNil(t, u)
		require.Equal(t, 10, u.OutputTokens)
		require.Equal(t, 5, u.CacheReadInputTokens)
	}
	for _, code := range []int{200, 400, 401, 403, 429, 500, 502, 503} {
		require.False(t, geminiProtocolMismatch(code))
	}
}

func TestGeminiOpenAIResponsesMeteredJSONAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		up := &geminiCompatHTTPUpstreamStub{}
		payload := geminiTestReceipt
		request := `{"model":"gemini-meter","input":"hi"}`
		if stream {
			payload = "data: " + payload + "\n\n"
			request = `{"model":"gemini-meter","input":"hi","stream":true}`
		}
		up.response = &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
		svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: up}
		acc := &Account{ID: 123, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		result, err := svc.ForwardAsResponses(context.Background(), c, acc, []byte(request))
		require.NoError(t, err)
		require.Equal(t, 18, result.Usage.InputTokens)
		require.Equal(t, 10, result.Usage.OutputTokens)
		require.Equal(t, 5, result.Usage.CacheReadInputTokens)
		require.Contains(t, w.Body.String(), "answer")
		if stream {
			require.Contains(t, w.Body.String(), "response.completed")
		} else {
			require.Equal(t, "response", gjson.Get(w.Body.String(), "object").String())
		}
	}
}

func TestGeminiRelayDoesNotReleaseUnmeteredContent(t *testing.T) {
	r := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"answer"},"finish_reason":"stop"}]}`))}
	response, err := normalizeGeminiRelayResponse(r, true, providerOpenAIPath)
	require.Nil(t, response)
	require.ErrorIs(t, err, errGeminiRelayReceipt)
}

func TestGeminiResponsesOnlyRelayIsRemembered(t *testing.T) {
	acc := &Account{ID: 271, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}
	const native = "/v1beta/models/gemini-only:streamGenerateContent"
	for i := 0; i < 2; i++ {
		var paths []string
		req, err := http.NewRequestWithContext(t.Context(), "POST", "https://responses-only.test"+native, strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":9}}`))
		require.NoError(t, err)
		r, err := geminiRelayRequest(acc, "gemini-only", req, func(req *http.Request) (*http.Response, error) {
			path := req.URL.Path
			paths = append(paths, path)
			status, body := 404, "{}"
			if path == providerOpenAIResponsesPath {
				b, _ := io.ReadAll(req.Body)
				require.EqualValues(t, 9, gjson.GetBytes(b, "max_output_tokens").Int())
				require.Equal(t, "hi", gjson.GetBytes(b, "input.0.content").String())
				status, body = 200, `{"object":"response","status":"completed","model":"gemini-only","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":12,"output_tokens":5,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":2}}}`
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		require.NoError(t, err)
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = r.Body.Close()
		require.Equal(t, providerOpenAIResponsesPath, geminiNegotiatedEndpoint(r))
		require.Contains(t, string(b), `"thoughtsTokenCount":2`)
		if i == 0 {
			require.Equal(t, []string{native, providerOpenAIPath, providerOpenAIResponsesPath}, paths)
		} else {
			require.Equal(t, []string{providerOpenAIResponsesPath}, paths)
		}
	}
}
