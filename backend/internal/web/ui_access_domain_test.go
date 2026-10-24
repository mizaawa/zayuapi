//go:build embed

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newUIAccessDomainRouter(t *testing.T, provider PublicSettingsProvider) (*FrontendServer, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	files := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(`<html><head></head><body><script src="/assets/app.js"></script></body></html>`)}, "assets/app.js": &fstest.MapFile{Data: []byte("static-resource")}, "favicon.ico": &fstest.MapFile{Data: []byte("favicon")}}
	server := &FrontendServer{distFS: files, fileServer: http.FileServer(http.FS(files)), baseHTML: files["index.html"].Data, cache: NewHTMLCache(), settings: provider}
	server.cache.SetBaseHTML(server.baseHTML)
	router := gin.New()
	router.Use(server.Middleware())
	router.NoRoute(func(c *gin.Context) { c.String(http.StatusAccepted, "api-untouched") })
	return server, router
}

func requestUIAccessDomain(router *gin.Engine, target, host string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Host = host
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertUIAccessDomainNotice(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "请通过官方合规域名进行站点访问")
	require.Contains(t, response.Body.String(), `href="//example.com/"`)
	require.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "Host", response.Header().Get("Vary"))
	require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	require.Empty(t, response.Header().Get("ETag"))
	for _, forbidden := range []string{"<script", "<link", "<iframe", "window.__APP_CONFIG__", "secret-token", "/assets/", "@import", "url("} {
		require.NotContains(t, response.Body.String(), forbidden)
	}
	require.Contains(t, response.Body.String(), `class="brand-logo"`)
	require.Contains(t, response.Body.String(), `class="brand-name"`)
	require.Contains(t, response.Body.String(), `class="primary-action"`)
	require.Contains(t, response.Body.String(), `prefers-reduced-motion:reduce`)
	require.Contains(t, response.Header().Get("Content-Security-Policy"), "default-src 'none'")
	require.Contains(t, response.Header().Get("Content-Security-Policy"), "form-action 'none'")
	require.Less(t, response.Body.Len(), 12000, "standalone notice must stay small and avoid the SPA payload")
}

func TestUIAccessDomainNoticeUsesConfiguredBrandAndProductTheme(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_access_domain_enabled": true, "ui_access_domain": "example.com",
		"site_name": `杂鱼 API <Site>`, "site_logo": `/brand/logo.png?name="safe"`,
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	response := requestUIAccessDomain(router, "/login?token=secret-token", "cdn.example.com", nil)
	assertUIAccessDomainNotice(t, response)
	body := response.Body.String()
	require.Contains(t, body, `杂鱼 API &lt;Site&gt;`)
	require.Contains(t, body, `src="/brand/logo.png?name=&#34;safe&#34;"`)
	for _, token := range []string{"--background:#fffbe8", "--surface:#fffef8", "--primary:#e9b824", "--green:#477b54", "border-radius:28px", "@keyframes card-enter", "@keyframes symbol-float", "color-scheme:light"} {
		require.Contains(t, body, token)
	}
	require.NotContains(t, body, "prefers-color-scheme:dark", "the product intentionally uses its light-only Monet theme")
	require.NotContains(t, body, "#175cd3", "do not reintroduce the unrelated blue theme")
	provider.settings = map[string]any{
		"ui_access_domain_enabled": true, "ui_access_domain": "example.com",
		"site_name": `"><script>alert(1)</script>`, "site_logo": "javascript:alert(1)",
	}
	server.InvalidateCache()
	response = requestUIAccessDomain(router, "/", "cdn.example.com", nil)
	require.Contains(t, response.Body.String(), `src="/logo.svg"`)
	require.Contains(t, response.Body.String(), `&lt;script&gt;`)
	require.NotContains(t, response.Body.String(), `<script>`)
	require.NotContains(t, response.Body.String(), "javascript:")
}

func TestUIAccessDomainBlocksBeforeSPAAndConditionalCache(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]string{"ui_access_domain": "example.com"}}
	_, router := newUIAccessDomainRouter(t, provider)
	for _, target := range []string{"http://cdn.example.com/", "https://cdn.example.com/index.html", "http://cdn.example.com/admin/settings?token=secret-token"} {
		assertUIAccessDomainNotice(t, requestUIAccessDomain(router, target, "cdn.example.com", nil))
	}
	require.Equal(t, 1, provider.called, "denied UI requests can reuse the cached policy without a settings read")
	for _, host := range []string{"example.com", "EXAMPLE.COM.:8443"} {
		allowed := requestUIAccessDomain(router, "https://example.com/dashboard", host, nil)
		require.Equal(t, http.StatusOK, allowed.Code)
		require.Contains(t, allowed.Body.String(), "window.__APP_CONFIG__")
		require.Contains(t, allowed.Header().Get("Cache-Control"), "no-store")
		assertUIAccessDomainNotice(t, requestUIAccessDomain(router, "/dashboard?token=secret-token", "cdn.example.com", map[string]string{"If-None-Match": allowed.Header().Get("ETag"), "X-Forwarded-Host": "example.com", "Forwarded": "host=example.com;proto=https"}))
	}
}

func TestUIAccessDomainLeavesAPIStaticAndStreamingUntouched(t *testing.T) {
	provider := &mockSettingsProvider{err: errors.New("settings unavailable")}
	_, router := newUIAccessDomainRouter(t, provider)
	for _, path := range []string{"/api/v1/settings/public", "/api/v1/payment/notify/stripe", "/api/v1/payment/oauth/wechat/callback?state=secret-token", "/health", "/v1/responses", "/openai/v1/chat/completions", "/v1beta/models", "/backend-api/codex/responses", "/setup/status"} {
		response := requestUIAccessDomain(router, path, "cdn.example.com", nil)
		require.Equal(t, http.StatusAccepted, response.Code, path)
		require.Equal(t, "api-untouched", response.Body.String(), path)
	}
	for _, path := range []string{"/assets/app.js", "/favicon.ico"} {
		response := requestUIAccessDomain(router, path, "cdn.example.com", nil)
		require.Equal(t, http.StatusOK, response.Code, path)
		require.NotContains(t, response.Body.String(), "官方合规域名")
	}
	router.GET("/v1/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.WriteString("data: first\n\n")
		c.Writer.Flush()
		_, _ = c.Writer.WriteString("data: done\n\n")
	})
	stream := requestUIAccessDomain(router, "/v1/stream", "cdn.example.com", nil)
	require.Equal(t, http.StatusOK, stream.Code)
	require.True(t, stream.Flushed)
	require.Equal(t, "data: first\n\ndata: done\n\n", stream.Body.String())
	require.Equal(t, "text/event-stream", stream.Header().Get("Content-Type"))
	require.Zero(t, provider.called)
}

func TestUIAccessDomainCacheInvalidationAndFailure(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]string{"ui_access_domain": ""}}
	server, router := newUIAccessDomainRouter(t, provider)
	require.Equal(t, http.StatusOK, requestUIAccessDomain(router, "/", "cdn.example.com", nil).Code)
	provider.settings = map[string]string{"ui_access_domain": "example.com"}
	server.InvalidateCache()
	assertUIAccessDomainNotice(t, requestUIAccessDomain(router, "/", "cdn.example.com", nil))
	provider.settings = map[string]string{"ui_access_domain": ""}
	server.InvalidateCache()
	require.Equal(t, http.StatusOK, requestUIAccessDomain(router, "/", "cdn.example.com", nil).Code)
	for _, settings := range []any{map[string]string{"ui_access_domain": `evil"><script>.com`}, make(chan int), []string{"invalid-shape"}} {
		provider.settings = settings
		server.InvalidateCache()
		response := requestUIAccessDomain(router, "/?token=secret-token", "cdn.example.com", nil)
		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.NotContains(t, response.Body.String(), "<script")
		require.NotContains(t, response.Body.String(), "secret-token")
		require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	}
}

type uiAccessDomainProviderFunc func(context.Context) (any, error)

func (f uiAccessDomainProviderFunc) GetPublicSettingsForInjection(ctx context.Context) (any, error) {
	return f(ctx)
}

func TestUIAccessDomainRejectsStaleCacheFill(t *testing.T) {
	var server *FrontendServer
	provider := uiAccessDomainProviderFunc(func(context.Context) (any, error) {
		server.InvalidateCache()
		return map[string]string{"ui_access_domain": ""}, nil
	})
	var router *gin.Engine
	server, router = newUIAccessDomainRouter(t, provider)
	response := requestUIAccessDomain(router, "/", "cdn.example.com", nil)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Nil(t, server.cache.Get())
	require.False(t, strings.Contains(response.Body.String(), "<script"))
}

func expireUIAccessDomainCache(cache *HTMLCache) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.cachedAt = time.Now().Add(-htmlCacheTTL - time.Nanosecond)
}

func TestUIAccessDomainSharedSettingsRefreshAcrossInstances(t *testing.T) {
	// The provider models a shared database. Only the instance handling the
	// setting write receives the local invalidation callback.
	provider := &mockSettingsProvider{settings: map[string]string{}}
	serverA, routerA := newUIAccessDomainRouter(t, provider)
	serverB, routerB := newUIAccessDomainRouter(t, provider)
	for _, router := range []*gin.Engine{routerA, routerB} {
		require.Equal(t, http.StatusOK, requestUIAccessDomain(router, "/", "cdn.example.com", nil).Code)
	}
	for _, change := range []struct {
		name, domain, probeHost, allowedHost string
		beforeExpiry, afterExpiry            int
	}{
		{"enable_from_missing", "example.com", "cdn.example.com", "example.com", http.StatusOK, http.StatusForbidden},
		{"change_official_domain", "portal.example.com", "example.com", "portal.example.com", http.StatusOK, http.StatusForbidden},
		{"disable_restriction", "", "cdn.example.com", "cdn.example.com", http.StatusForbidden, http.StatusOK},
	} {
		t.Run(change.name, func(t *testing.T) {
			provider.settings = map[string]string{"ui_access_domain": change.domain}
			serverA.InvalidateCache()
			require.Equal(t, change.afterExpiry, requestUIAccessDomain(routerA, "/dashboard", change.probeHost, nil).Code)
			require.Equal(t, http.StatusOK, requestUIAccessDomain(routerA, "/dashboard", change.allowedHost, nil).Code)
			// Requests within the finite TTL may still observe the prior snapshot.
			require.Equal(t, change.beforeExpiry, requestUIAccessDomain(routerB, "/dashboard", change.probeHost, nil).Code)
			expireUIAccessDomainCache(serverB.cache)
			refreshed := requestUIAccessDomain(routerB, "/dashboard", change.probeHost, nil)
			require.Equal(t, change.afterExpiry, refreshed.Code)
			if change.domain != "" {
				require.Contains(t, refreshed.Body.String(), `href="//`+change.domain+`/"`)
				require.NotContains(t, refreshed.Body.String(), "<script")
			}
			require.Equal(t, http.StatusOK, requestUIAccessDomain(routerB, "/dashboard", change.allowedHost, nil).Code)
		})
	}
	require.Equal(t, 8, provider.called, "each instance reads once initially and once for each refreshed setting")
	// An expired unrestricted snapshot cannot become a permanent fail-open
	// fallback if the remote settings read subsequently becomes unavailable.
	provider.err = errors.New("shared settings unavailable")
	expireUIAccessDomainCache(serverB.cache)
	failed := requestUIAccessDomain(routerB, "/dashboard", "cdn.example.com", nil)
	require.Equal(t, http.StatusServiceUnavailable, failed.Code)
	require.NotContains(t, failed.Body.String(), "<script")
	require.Contains(t, failed.Header().Get("Cache-Control"), "no-store")
}

func TestUIAccessDomainCacheTTLDoesNotSlideOrOverrideInvalidation(t *testing.T) {
	cache := NewHTMLCache()
	generation := cache.Generation()
	require.True(t, cache.SetIfGeneration([]byte("html"), []byte(`{"ui_access_domain":""}`), generation))
	cachedAt := cache.cachedAt
	require.NotNil(t, cache.Get())
	require.Equal(t, cachedAt, cache.cachedAt, "cache reads must not extend the refresh deadline")
	expireUIAccessDomainCache(cache)
	require.Nil(t, cache.Get(), "the disabled policy must expire too")
	require.Equal(t, generation, cache.Generation())
	cache.Invalidate()
	require.False(t, cache.SetIfGeneration([]byte("stale"), []byte(`{}`), generation))
	require.Nil(t, cache.Get())
	require.True(t, cache.SetIfGeneration([]byte("fresh"), []byte(`{}`), cache.Generation()))
	require.NotNil(t, cache.Get())
}
