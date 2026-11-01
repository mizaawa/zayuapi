//go:build embed

package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// assertUIAccessUncacheable checks every opt-out that a browser, CDN or
// reverse proxy may rely on for a visitor-dependent UI response.
func assertUIAccessUncacheable(t *testing.T, header http.Header) {
	t.Helper()
	cacheControl := header.Get("Cache-Control")
	for _, directive := range []string{"private", "no-store", "no-cache", "max-age=0"} {
		require.Contains(t, cacheControl, directive)
	}
	require.Equal(t, "no-cache", header.Get("Pragma"))
	require.Equal(t, "0", header.Get("Expires"))
	require.Equal(t, "no-store", header.Get("CDN-Cache-Control"))
	require.Equal(t, "no-store", header.Get("Cloudflare-CDN-Cache-Control"))
	require.Equal(t, "no-store", header.Get("Surrogate-Control"))
	require.Equal(t, "0", header.Get("X-Accel-Expires"))
	require.Equal(t, "*", header.Get("Vary"))
	require.Equal(t, []string{uiAccessNoCacheCookie}, header.Values("Set-Cookie"))
}

func TestPreventUIAccessCachingAddsItsCookieOnceAndKeepsOthers(t *testing.T) {
	header := http.Header{}
	header.Add("Set-Cookie", "session=keep; Path=/")
	preventUIAccessCaching(header)
	preventUIAccessCaching(header)
	require.Equal(t, []string{"session=keep; Path=/", uiAccessNoCacheCookie}, header.Values("Set-Cookie"))
	require.Contains(t, uiAccessNoCacheCookie, "Max-Age=0", "the cookie must never be stored by the browser")
}

func TestUIRetryReturnPathAddsAFreshCacheBuster(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/admin/settings?tab=security&_ui_access=old&state=a%20b&_ui_access", nil)
	first := uiRetryReturnPath(request)
	require.Regexp(t, `^/admin/settings\?tab=security&state=a%20b&_ui_access=[a-z2-7]{26}$`, first)
	require.NotEqual(t, first, uiRetryReturnPath(request), "each retry gets its own URL")
	require.Regexp(t, `^/login\?_ui_access=[a-z2-7]{26}$`, uiRetryReturnPath(httptest.NewRequest(http.MethodPost, "/login", nil)))
	require.Regexp(t, `^/\?_ui_access=[a-z2-7]{26}$`, uiRetryReturnPath(httptest.NewRequest(http.MethodPost, "//evil.example/x?a=1", nil)))
}

func TestUICountryResponsesStayOutOfSharedCaches(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN",
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	country := "CN"
	server.countryResolver = uiCountryResolverFunc(func(context.Context, string, bool) (string, error) {
		return country, nil
	})
	retry := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/dashboard?_ui_access=stale", strings.NewReader("_ui_country_retry=1"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Host = "example.com"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	blocked := requestUIAccessDomain(router, "/dashboard", "example.com", nil)
	require.Equal(t, http.StatusForbidden, blocked.Code)
	assertUIAccessUncacheable(t, blocked.Header())
	stillBlocked := retry()
	require.Equal(t, http.StatusForbidden, stillBlocked.Code)
	assertUIAccessUncacheable(t, stillBlocked.Header())

	country = "US"
	allowed := retry()
	require.Equal(t, http.StatusSeeOther, allowed.Code)
	assertUIAccessUncacheable(t, allowed.Header())
	location := allowed.Header().Get("Location")
	require.Regexp(t, `^/dashboard\?_ui_access=[a-z2-7]{26}$`, location, "the stale buster is replaced, never repeated")

	page := requestUIAccessDomain(router, location, "example.com", nil)
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "window.__APP_CONFIG__")
	assertUIAccessUncacheable(t, page.Header())

	// The SPA probes later navigations with HEAD; the answer must not be stored either.
	probe := httptest.NewRequest(http.MethodHead, "/keys?_ui_access=probe", nil)
	probe.Host = "example.com"
	probed := httptest.NewRecorder()
	router.ServeHTTP(probed, probe)
	require.Equal(t, http.StatusOK, probed.Code)
	assertUIAccessUncacheable(t, probed.Header())
}

func TestUnrestrictedUIKeepsRevalidatingCache(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]any{"site_name": "Demo"}}
	_, router := newUIAccessDomainRouter(t, provider)
	response := requestUIAccessDomain(router, "/dashboard", "example.com", nil)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
	require.NotEmpty(t, response.Header().Get("ETag"))
	require.Empty(t, response.Header().Values("Set-Cookie"))
	require.Empty(t, response.Header().Get("Vary"))
}
