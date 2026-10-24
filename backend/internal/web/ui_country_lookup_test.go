//go:build embed

package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUICountryLookupValidationCacheAndExplicitRefresh(t *testing.T) {
	calls := 0
	country := "CN"
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/8.8.8.8.json", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Empty(t, r.Header.Get("Cookie"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "Sub2API-UI-Access-Control", r.Header.Get("User-Agent"))
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"ip":"8.8.8.8","country_code":%q}`, country)
	}))
	defer server.Close()
	resolver := newUICountryResolver()
	resolver.endpoint = server.URL + "/"
	ctx := context.Background()
	code, err := resolver.Country(ctx, "8.8.8.8", false)
	require.NoError(t, err)
	require.Equal(t, "CN", code)
	country = "US"
	code, err = resolver.Country(ctx, "::ffff:8.8.8.8", false)
	require.NoError(t, err)
	require.Equal(t, "CN", code)
	require.Equal(t, 1, calls)
	code, err = resolver.Country(ctx, "8.8.8.8", true)
	require.NoError(t, err)
	require.Equal(t, "US", code)
	require.Equal(t, 2, calls)
	status = http.StatusServiceUnavailable
	_, err = resolver.Country(ctx, "8.8.8.8", true)
	require.Error(t, err)
	_, err = resolver.Country(ctx, "8.8.8.8", false)
	require.Error(t, err, "failed retry invalidates the earlier allowed cache entry")
	require.Equal(t, 4, calls)
	for _, input := range []string{"127.0.0.1", "10.0.0.1", "::1", "fc00::1", "169.254.169.254", "8.8.8.8/../../x", "host.example", "224.0.0.1"} {
		_, err = resolver.Country(ctx, input, true)
		require.Error(t, err, input)
	}
	require.Equal(t, 4, calls, "nonpublic or malformed addresses must not leave the process")
}

func TestUICountryLookupRejectsMalformedMismatchedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{
		`{"ip":"1.1.1.1","country_code":"CN"}`,
		`{"ip":"8.8.8.8","country_code":"XX"}`,
		`{"ip":"8.8.8.8","country_code":"XK"}`,
		`{"country_code":"CN"}`,
		`not-json`, strings.Repeat("x", 16*1024+1),
	} {
		t.Run(fmt.Sprintf("body_%d_%s", len(body), body[:min(18, len(body))]), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			resolver := newUICountryResolver()
			resolver.endpoint = server.URL + "/"
			_, err := resolver.Country(context.Background(), "8.8.8.8", false)
			require.Error(t, err)
		})
	}
}

func TestUICountryLookupDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret" {
			t.Error("country provider redirect must never be followed")
		}
		http.Redirect(w, r, "/secret", http.StatusFound)
	}))
	defer server.Close()
	resolver := newUICountryResolver()
	resolver.endpoint = server.URL + "/"
	_, err := resolver.Country(context.Background(), "8.8.8.8", false)
	require.Error(t, err)
}

func TestUICountryLookupHonorsCancellationAndFiniteCache(t *testing.T) {
	resolver := newUICountryResolver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver.Country(ctx, "8.8.8.8", false)
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"8.8.8.8","country_code":"US"}`))
	}))
	defer server.Close()
	resolver.endpoint = server.URL + "/"
	resolver.cache["8.8.8.8"] = uiCountryEntry{country: "CN", expires: time.Now().Add(-time.Second)}
	code, err := resolver.Country(context.Background(), "8.8.8.8", false)
	require.NoError(t, err)
	require.Equal(t, "US", code)
}

func TestUICountryRetryOverloadCannotReusePreviousAllowedCountry(t *testing.T) {
	resolver := newUICountryResolver()
	resolver.cache["8.8.8.8"] = uiCountryEntry{country: "US", expires: time.Now().Add(time.Hour)}
	for range cap(resolver.capacity) {
		resolver.capacity <- struct{}{}
	}
	_, err := resolver.Country(context.Background(), "8.8.8.8", true)
	require.Error(t, err)
	_, exists := resolver.cache["8.8.8.8"]
	require.False(t, exists)
	_, err = resolver.Country(context.Background(), "8.8.8.8", false)
	require.Error(t, err, "overload cannot expose an old allowed country")
}
