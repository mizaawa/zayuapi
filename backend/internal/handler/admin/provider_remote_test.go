package admin

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderURLRejectsUnsafeOrigins(t *testing.T) {
	for _, raw := range []string{
		"http://example.com", "https://127.0.0.1", "https://localhost",
		"https://example.com/private", "https://user:pass@example.com", "https://example.com?q=1",
	} {
		_, err := normalizeProviderURL(raw)
		require.Error(t, err, raw)
	}
	base, err := normalizeProviderURL("https://www.zayuapi.com/")
	require.NoError(t, err)
	require.Equal(t, "https://www.zayuapi.com", base)
}

func TestSub2APIProviderLoginAndMetrics(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			require.Equal(t, http.MethodPost, r.Method)
			_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"test-token"}}`))
		case "/api/v1/user/profile":
			require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"data":{"balance":15.79,"total_recharged":20}}`))
		case "/api/v1/usage/dashboard/stats":
			require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"data":{"today_actual_cost":0.70,"total_actual_cost":9.21}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	metrics, err := probeProviderWithClient(context.Background(), providerRecord{SiteType: "sub2api", AuthMode: "password", BaseURL: server.URL, Account: "demo@example.com"}, "password", server.Client())
	require.NoError(t, err)
	require.Equal(t, 15.79, *metrics.Balance)
	require.Equal(t, 0.70, *metrics.TodaySpend)
	require.Equal(t, 20.0, *metrics.TotalRecharge)
	require.Equal(t, 9.21, *metrics.TotalSpend)
}

func TestNewAPIProviderUsesSessionAndQuotaPerUnit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/user/login":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "demo", Path: "/", Secure: true})
			_, _ = w.Write([]byte(`{"success":true,"data":{"username":"demo"}}`))
		case r.URL.Path == "/api/user/self":
			require.True(t, strings.Contains(r.Header.Get("Cookie"), "session=demo"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500000,"used_quota":250000}}`))
		case r.URL.Path == "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		case r.URL.Path == "/api/log/self/stat":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":100000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := server.Client()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client.Jar = jar
	metrics, err := probeProviderWithClient(context.Background(), providerRecord{SiteType: "newapi", AuthMode: "password", BaseURL: server.URL, Account: "demo"}, "password", client)
	require.NoError(t, err)
	require.Equal(t, 1.0, *metrics.Balance)
	require.Equal(t, 0.5, *metrics.TotalSpend)
	require.Equal(t, 0.2, *metrics.TodaySpend)
	require.Nil(t, metrics.TotalRecharge) // Unknown is not zero.
}
