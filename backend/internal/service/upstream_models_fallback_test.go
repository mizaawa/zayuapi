package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func upstreamModelsJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func upstreamModelsAPIKeyAccount(platform, baseURL, apiKey string) *Account {
	return &Account{
		ID:       21,
		Platform: platform,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  apiKey,
			"base_url": baseURL,
		},
	}
}

func TestFetchUpstreamSupportedModelsGeminiRelayFallsBackToOpenAIList(t *testing.T) {
	t.Parallel()

	// A sub2api relay answers the native Gemini list with 400 when the key's
	// group speaks the OpenAI protocol; its OpenAI-compatible list still works.
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusBadRequest, `{"error":{"code":400,"message":"API key group platform is not gemini","status":"INVALID_ARGUMENT"}}`),
		upstreamModelsJSONResponse(http.StatusOK, `{"object":"list","data":[{"id":"gemini-3-pro"},{"id":"gemini-2.5-flash"}]}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://relay.example", "relay-key"))
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash", "gemini-3-pro"}, models)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://relay.example/v1beta/models", upstream.requests[0].URL.String())
	require.Equal(t, "relay-key", upstream.requests[0].Header.Get("x-goog-api-key"))
	require.Equal(t, "https://relay.example/v1/models", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer relay-key", upstream.requests[1].Header.Get("Authorization"))
	require.Empty(t, upstream.requests[1].Header.Get("x-goog-api-key"))
}

func TestFetchUpstreamSupportedModelsGeminiFallbackStripsNativePath(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{"https://relay.example/v1beta", "https://relay.example/v1beta/", "https://relay.example/v1beta/openai"} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			upstreamModelsJSONResponse(http.StatusNotFound, `{"error":"not found"}`),
			upstreamModelsJSONResponse(http.StatusOK, `{"data":[{"id":"gemini-3-pro"}]}`),
		}}
		svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

		models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, baseURL, "relay-key"))
		require.NoError(t, err, baseURL)
		require.Equal(t, []string{"gemini-3-pro"}, models, baseURL)
		require.Len(t, upstream.requests, 2, baseURL)
		require.Equal(t, "https://relay.example/v1/models", upstream.requests[1].URL.String(), baseURL)
	}
}

func TestFetchUpstreamSupportedModelsFallsBackOnUnusableSuccessBodies(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"spa page":        `<!doctype html><html><body>index</body></html>`,
		"failure payload": `{"success":false,"message":"无效的令牌"}`,
	} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			upstreamModelsJSONResponse(http.StatusOK, body),
			upstreamModelsJSONResponse(http.StatusOK, `{"success":true,"object":"list","data":[{"id":"gemini-3-flash"}]}`),
		}}
		svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

		models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://newapi.example", "relay-key"))
		require.NoError(t, err, name)
		require.Equal(t, []string{"gemini-3-flash"}, models, name)
		require.Len(t, upstream.requests, 2, name)
	}
}

func TestFetchUpstreamSupportedModelsGeminiNewAPIShape(t *testing.T) {
	t.Parallel()

	// New API answers GET /v1beta/models with bare model names.
	upstream := &httpUpstreamRecorder{resp: upstreamModelsJSONResponse(http.StatusOK,
		`{"models":[{"name":"gemini-3-pro","displayName":"gemini-3-pro"},{"name":"models/gemini-2.5-flash","displayName":"gemini-2.5-flash"}],"nextPageToken":null}`)}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://newapi.example", "relay-key"))
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash", "gemini-3-pro"}, models)
	require.Len(t, upstream.requests, 1)
}

func TestFetchUpstreamSupportedModelsFollowsGeminiPages(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusOK, `{"models":[{"name":"models/gemini-2.5-flash"}],"nextPageToken":"page-2"}`),
		upstreamModelsJSONResponse(http.StatusOK, `{"models":[{"name":"models/gemini-3-pro"}]}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://generativelanguage.googleapis.com", "google-key"))
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash", "gemini-3-pro"}, models)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models", upstream.requests[0].URL.String())
	require.Equal(t, "page-2", upstream.requests[1].URL.Query().Get("pageToken"))
	require.Equal(t, "google-key", upstream.requests[1].Header.Get("x-goog-api-key"))
}

func TestFetchUpstreamSupportedModelsStopsRepeatingCursor(t *testing.T) {
	t.Parallel()

	page := `{"models":[{"name":"models/gemini-3-pro"}],"nextPageToken":"same"}`
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusOK, page),
		upstreamModelsJSONResponse(http.StatusOK, page),
		upstreamModelsJSONResponse(http.StatusOK, page),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://generativelanguage.googleapis.com", "google-key"))
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-3-pro"}, models)
	require.Len(t, upstream.requests, 2)
}

func TestFetchUpstreamSupportedModelsFollowsAnthropicPages(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusOK, `{"data":[{"id":"claude-opus-4-6"}],"has_more":true,"first_id":"claude-opus-4-6","last_id":"claude-opus-4-6"}`),
		upstreamModelsJSONResponse(http.StatusOK, `{"data":[{"id":"claude-sonnet-4-6"}],"has_more":false,"last_id":"claude-sonnet-4-6"}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformAnthropic, "https://api.anthropic.com", "sk-ant"))
	require.NoError(t, err)
	require.Equal(t, []string{"claude-opus-4-6", "claude-sonnet-4-6"}, models)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://api.anthropic.com/v1/models", upstream.requests[0].URL.String())
	require.Equal(t, "claude-opus-4-6", upstream.requests[1].URL.Query().Get("after_id"))
	require.Equal(t, "sk-ant", upstream.requests[1].Header.Get("x-api-key"))
}

func TestFetchUpstreamSupportedModelsAnthropicRelayFallsBackToBearerList(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusUnauthorized, `{"error":{"message":"Invalid token"}}`),
		upstreamModelsJSONResponse(http.StatusOK, `{"success":true,"object":"list","data":[{"id":"claude-sonnet-4-6"}]}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	models, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformAnthropic, "https://relay.example", "relay-key"))
	require.NoError(t, err)
	require.Equal(t, []string{"claude-sonnet-4-6"}, models)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "relay-key", upstream.requests[0].Header.Get("x-api-key"))
	require.Equal(t, "https://relay.example/v1/models", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer relay-key", upstream.requests[1].Header.Get("Authorization"))
	require.Empty(t, upstream.requests[1].Header.Get("x-api-key"))
}

func TestFetchUpstreamSupportedModelsReportsNativeFailureWhenFallbackFails(t *testing.T) {
	t.Parallel()

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		upstreamModelsJSONResponse(http.StatusForbidden, `{"error":"SECRET_TOKEN"}`),
		upstreamModelsJSONResponse(http.StatusNotFound, `{"error":"SECRET_TOKEN"}`),
	}}
	svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

	_, err := svc.FetchUpstreamSupportedModels(context.Background(), upstreamModelsAPIKeyAccount(PlatformGemini, "https://relay.example", "relay-key"))
	require.Error(t, err)
	var syncErr *UpstreamModelSyncError
	require.True(t, errors.As(err, &syncErr))
	require.Equal(t, UpstreamModelSyncErrorUpstream, syncErr.Kind)
	require.Contains(t, syncErr.SafeMessage(), "HTTP 403")
	require.NotContains(t, syncErr.SafeMessage(), "SECRET_TOKEN")
	require.Len(t, upstream.requests, 2)
}

func TestFetchUpstreamSupportedModelsSkipsFallbackForOfficialAndOpenAI(t *testing.T) {
	t.Parallel()

	for _, account := range []*Account{
		upstreamModelsAPIKeyAccount(PlatformGemini, "https://generativelanguage.googleapis.com", "google-key"),
		upstreamModelsAPIKeyAccount(PlatformAnthropic, "https://api.anthropic.com", "sk-ant"),
		upstreamModelsAPIKeyAccount(PlatformOpenAI, "https://relay.example/v1", "sk-openai"),
	} {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			upstreamModelsJSONResponse(http.StatusUnauthorized, `{"error":"bad key"}`),
			upstreamModelsJSONResponse(http.StatusOK, `{"data":[{"id":"unexpected"}]}`),
		}}
		svc := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}

		_, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
		require.Error(t, err, account.Platform)
		require.Len(t, upstream.requests, 1, account.Platform)
	}
}

func TestGeminiCompatibleModelsBaseURL(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://relay.example", geminiCompatibleModelsBaseURL(" https://relay.example/ "))
	require.Equal(t, "https://relay.example", geminiCompatibleModelsBaseURL("https://relay.example/v1beta"))
	require.Equal(t, "https://relay.example", geminiCompatibleModelsBaseURL("https://relay.example/V1BETA/models"))
	require.Equal(t, "https://relay.example/v1", geminiCompatibleModelsBaseURL("https://relay.example/v1"))
}
