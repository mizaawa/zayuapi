package uiaccess

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	resolver := newTestCountryResolver()
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
			resolver := newTestCountryResolver()
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
	resolver := newTestCountryResolver()
	resolver.endpoint = server.URL + "/"
	_, err := resolver.Country(context.Background(), "8.8.8.8", false)
	require.Error(t, err)
}

func TestUICountryLookupHonorsCancellationAndFiniteCache(t *testing.T) {
	resolver := newTestCountryResolver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver.Country(ctx, "8.8.8.8", false)
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"8.8.8.8","country_code":"US"}`))
	}))
	defer server.Close()
	resolver.endpoint = server.URL + "/"
	resolver.cache["8.8.8.8"] = uiCountryEntry{result: CountryLookupResult{Country: "CN"}, expires: time.Now().Add(-time.Second)}
	code, err := resolver.Country(context.Background(), "8.8.8.8", false)
	require.NoError(t, err)
	require.Equal(t, "US", code)
}

func TestUICountryRetryOverloadCannotReusePreviousAllowedCountry(t *testing.T) {
	resolver := newTestCountryResolver()
	resolver.cache["8.8.8.8"] = uiCountryEntry{result: CountryLookupResult{Country: "US"}, expires: time.Now().Add(time.Hour)}
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

func newTestCountryResolver() *CountryResolver {
	resolver := NewCountryResolver()
	resolver.fallbackEndpoint = ""
	return resolver
}

func TestUICountryLookupFallsBackAfterPrimaryFailure(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		fallbackCalls++
		require.Equal(t, "/8.8.8.8", request.URL.Path)
		require.Empty(t, request.Header.Get("Cookie"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"ip":"8.8.8.8","country":"US"}`))
	}))
	defer fallback.Close()
	resolver := NewCountryResolver()
	resolver.endpoint, resolver.fallbackEndpoint = primary.URL+"/", fallback.URL+"/"
	result, err := resolver.Detect(context.Background(), "8.8.8.8", false)
	require.NoError(t, err)
	require.Equal(t, "US", result.Country)
	require.Equal(t, "country_is", result.Provider)
	require.Equal(t, []CountryLookupAttempt{{Provider: "geojs", ErrorCode: "provider_http_error"}, {Provider: "country_is"}}, result.Attempts)
	result, err = resolver.Detect(context.Background(), "8.8.8.8", false)
	require.NoError(t, err)
	require.True(t, result.Cached)
	require.Equal(t, 1, primaryCalls)
	require.Equal(t, 1, fallbackCalls)
}

func TestUICountryLookupAllowsHealthyResponseBeyondOldTwoSecondTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		select {
		case <-time.After(2100 * time.Millisecond):
			_, _ = w.Write([]byte(`{"ip":"8.8.8.8","country_code":"US"}`))
		case <-request.Context().Done():
		}
	}))
	defer server.Close()
	resolver := newTestCountryResolver()
	resolver.endpoint = server.URL + "/"
	result, err := resolver.Detect(context.Background(), "8.8.8.8", true)
	require.NoError(t, err, "a healthy cold provider response must not hit the previous 2s deadline")
	require.Equal(t, "US", result.Country)
}

func TestUICountryLookupTimeoutUsesFallbackWithoutReturningOldCache(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = w.Write([]byte(`{"ip":"8.8.8.8","country":"CN"}`))
	}))
	defer fallback.Close()
	resolver := NewCountryResolver()
	resolver.client.Timeout = 100 * time.Millisecond
	resolver.endpoint, resolver.fallbackEndpoint = primary.URL+"/", fallback.URL+"/"
	resolver.cache["8.8.8.8"] = uiCountryEntry{result: CountryLookupResult{IP: "8.8.8.8", Country: "US"}, expires: time.Now().Add(time.Hour)}
	result, err := resolver.Detect(context.Background(), "8.8.8.8", true)
	require.NoError(t, err)
	require.Equal(t, "CN", result.Country)
	require.Equal(t, "country_is", result.Provider)
	require.Equal(t, "timeout", result.Attempts[0].ErrorCode)
}

func TestUICountryLookupFallbackValidatesResponseAndReportsFailure(t *testing.T) {
	for _, body := range []string{
		`{"ip":"1.1.1.1","country":"US"}`,
		`{"ip":"8.8.8.8","country":"ZZ"}`,
		`{"ip":"8.8.8.8","country":null}`,
		`{"ip":"8.8.8.8","country_code":"US"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, ".json") {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			resolver := NewCountryResolver()
			resolver.endpoint, resolver.fallbackEndpoint = server.URL+"/", server.URL+"/"
			result, err := resolver.Detect(context.Background(), "8.8.8.8", true)
			require.Error(t, err)
			require.Empty(t, result.Country, "an unavailable provider must not silently allow a visitor")
			require.Equal(t, "providers_unavailable", result.ErrorCode)
			require.Len(t, result.Attempts, 2)
			require.Empty(t, resolver.cache)
		})
	}
}

func TestUICountryLookupReportsPrivateIPWithoutNetwork(t *testing.T) {
	resolver := NewCountryResolver()
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.18.0.1", "fc00::1"} {
		result, err := resolver.Detect(context.Background(), ip, true)
		require.Error(t, err)
		require.Equal(t, "non_public_ip", result.ErrorCode)
		require.Equal(t, "non_public_ip", CountryLookupErrorCode(err))
		require.Empty(t, result.Attempts)
	}
}

// Opt-in live verification uses public recursive DNS addresses, never a user IP.
// CI remains deterministic; release verification records the actual responses.
func TestUICountryLookupLivePublicResolverIPs(t *testing.T) {
	if os.Getenv("UI_COUNTRY_LIVE_TEST") != "1" {
		t.Skip("set UI_COUNTRY_LIVE_TEST=1 for real provider verification")
	}
	for _, provider := range []string{"geojs", "country_is"} {
		for ip, want := range map[string]string{"8.8.8.8": "US", "223.5.5.5": "CN"} {
			t.Run(provider+"/"+ip, func(t *testing.T) {
				resolver := NewCountryResolver()
				if provider == "geojs" {
					resolver.fallbackEndpoint = ""
				} else {
					resolver.endpoint = ""
				}
				started := time.Now()
				result, err := resolver.Detect(context.Background(), ip, true)
				t.Logf("LIVE provider=%s ip=%s country=%s error_code=%s attempts=%+v elapsed=%s", provider, ip, result.Country, result.ErrorCode, result.Attempts, time.Since(started))
				require.NoError(t, err)
				require.Equal(t, want, result.Country)
				require.Equal(t, provider, result.Provider)
			})
		}
	}
}
