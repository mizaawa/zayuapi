//go:build embed

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type uiCountryResolverFunc func(context.Context, string, bool) (string, error)

func (f uiCountryResolverFunc) Country(ctx context.Context, ip string, refresh bool) (string, error) {
	return f(ctx, ip, refresh)
}

func TestUIAccessExplicitDomainSwitch(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_access_domain": "example.com", "ui_access_domain_enabled": false,
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	allowed := requestUIAccessDomain(router, "/login", "cdn.example.com", nil)
	require.Equal(t, http.StatusOK, allowed.Code)
	require.Contains(t, allowed.Body.String(), "window.__APP_CONFIG__")
	provider.settings = map[string]any{"ui_access_domain": "example.com", "ui_access_domain_enabled": true}
	server.InvalidateCache()
	assertUIAccessDomainNotice(t, requestUIAccessDomain(router, "/login", "cdn.example.com", nil))
	provider.settings = map[string]any{"ui_access_domain": "", "ui_access_domain_enabled": true}
	server.InvalidateCache()
	require.Equal(t, http.StatusServiceUnavailable, requestUIAccessDomain(router, "/", "example.com", nil).Code)
}

func TestUICountryPolicyHostScopeBrandingAndCache(t *testing.T) {
	settings := map[string]any{
		"ui_access_domain": "example.com", "ui_access_domain_enabled": true,
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN, US",
		"site_name": "Demo <Site>", "site_logo": "/custom-logo.png",
	}
	provider := &mockSettingsProvider{settings: settings}
	server, router := newUIAccessDomainRouter(t, provider)
	calls := 0
	server.countryResolver = uiCountryResolverFunc(func(context.Context, string, bool) (string, error) {
		calls++
		return "CN", nil
	})
	assertUIAccessDomainNotice(t, requestUIAccessDomain(router, "/home", "cdn.example.com", nil))
	require.Zero(t, calls, "nonofficial UI host is rejected without any geo request")
	for _, headers := range []map[string]string{nil, {"If-None-Match": server.cache.Get().ETag}} {
		blocked := requestUIAccessDomain(router, "/login", "example.com", headers)
		require.Equal(t, http.StatusForbidden, blocked.Code)
		require.Contains(t, blocked.Body.String(), "该地区不在服务范围内")
		require.Contains(t, blocked.Body.String(), "Demo &lt;Site&gt;")
		require.Contains(t, blocked.Body.String(), `src="/custom-logo.png"`)
		require.Contains(t, blocked.Body.String(), `<button class="primary-action" type="submit">`)
		require.Contains(t, blocked.Body.String(), `<span>重试</span>`)
		require.Contains(t, blocked.Body.String(), `<form method="post" action="">`)
		require.Contains(t, blocked.Body.String(), `--background:#fffbe8`)
		require.Contains(t, blocked.Body.String(), `prefers-reduced-motion:reduce`)
		require.NotContains(t, blocked.Body.String(), "window.__APP_CONFIG__")
		require.NotContains(t, blocked.Body.String(), "<script")
		require.NotContains(t, blocked.Body.String(), "/assets/")
		require.Empty(t, blocked.Header().Get("ETag"))
		require.Equal(t, "private, no-store", blocked.Result().Header.Get("Cache-Control"))
		require.Contains(t, blocked.Result().Header.Get("Content-Security-Policy"), "form-action 'self'")
	}
	settings["ui_access_domain_enabled"] = false
	server.InvalidateCache()
	response := requestUIAccessDomain(router, "/home", "cdn.example.com", nil)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "该地区不在服务范围内")
	settings["ui_country_restriction_enabled"] = false
	server.InvalidateCache()
	require.Equal(t, http.StatusOK, requestUIAccessDomain(router, "/home", "cdn.example.com", nil).Code)
	require.Equal(t, 3, calls)
}

func TestUICountryPolicyUsesTrustedClientIPAndIgnoresForgedCountry(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN",
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	require.NoError(t, router.SetTrustedProxies([]string{"10.0.0.1"}))
	var received []string
	server.countryResolver = uiCountryResolverFunc(func(_ context.Context, ip string, _ bool) (string, error) {
		received = append(received, ip)
		return "CN", nil
	})
	for _, peer := range []string{"1.1.1.1:1234", "10.0.0.1:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/login", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "8.8.8.8")
		req.Header.Set("CF-IPCountry", "US")
		req.Header.Set("X-Country-Code", "US")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	require.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, received)
}

func TestUICountryRetryRechecksAndReturnsToOriginalPage(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN",
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	var refreshes []bool
	country := "CN"
	var lookupErr error
	server.countryResolver = uiCountryResolverFunc(func(_ context.Context, _ string, refresh bool) (string, error) {
		refreshes = append(refreshes, refresh)
		return country, lookupErr
	})
	url := "/admin/settings?tab=security&state=keep-this"
	blocked := requestUIAccessDomain(router, url, "example.com", nil)
	require.Equal(t, http.StatusForbidden, blocked.Code)
	require.NotContains(t, blocked.Body.String(), "keep-this", "form uses the current document URL without reflecting its query")
	postRetry := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, url, strings.NewReader("_ui_country_retry=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Host = "example.com"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	require.Equal(t, http.StatusForbidden, postRetry().Code)
	lookupErr = errors.New("temporarily unavailable")
	unavailable := postRetry()
	require.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
	require.Contains(t, unavailable.Body.String(), "暂时无法确认访问地区")
	require.NotContains(t, unavailable.Body.String(), "window.__APP_CONFIG__")
	country, lookupErr = "US", nil
	allowed := postRetry()
	require.Equal(t, http.StatusSeeOther, allowed.Code)
	require.Equal(t, url, allowed.Result().Header.Get("Location"))
	require.Equal(t, http.StatusOK, requestUIAccessDomain(router, url, "example.com", nil).Code)
	require.Equal(t, []bool{false, true, true, true, false}, refreshes)
	provider.settings = map[string]any{"ui_country_restriction_enabled": false, "ui_blocked_countries": "CN"}
	server.InvalidateCache()
	cleared := postRetry()
	require.Equal(t, http.StatusSeeOther, cleared.Code, "disabling the policy also restores the original GET page")
	require.Equal(t, url, cleared.Result().Header.Get("Location"))
	require.Equal(t, []bool{false, true, true, true, false}, refreshes, "disabled policy never calls geo lookup")
}

func TestUICountryLeavesAPIAssetsAndCallbacksUntouched(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN",
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	server.countryResolver = uiCountryResolverFunc(func(context.Context, string, bool) (string, error) {
		t.Fatal("API and assets must not perform a country lookup")
		return "", nil
	})
	for _, path := range []string{"/api/v1/payment/notify/stripe", "/api/v1/settings/public", "/v1/responses", "/health"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("callback-body"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, "api-untouched", rec.Body.String())
	}
	require.Equal(t, http.StatusOK, requestUIAccessDomain(router, "/assets/app.js", "cdn.example.com", nil).Code)
	require.Zero(t, provider.called)
}

func TestUICountrySafeReturnPath(t *testing.T) {
	for _, target := range []string{"//evil.example/x", "/%2fexternal.example", "/%5cevil.example"} {
		require.Equal(t, "/", safeUIReturnPath(httptest.NewRequest(http.MethodGet, target, nil)))
	}
	require.Equal(t, "/login?redirect=%2Fdashboard", safeUIReturnPath(httptest.NewRequest(http.MethodGet, "/login?redirect=%2Fdashboard", nil)))
}
