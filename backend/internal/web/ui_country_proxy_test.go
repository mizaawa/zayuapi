//go:build embed

package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type countryProxyTestTransport func(*http.Request) (*http.Response, error)

func (f countryProxyTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUICountryPolicyUsesConfiguredProxySnapshotEndToEnd(t *testing.T) {
	var queriedIPs []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ip := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/ip/geo/"), ".json")
		queriedIPs = append(queriedIPs, ip)
		country := "US"
		if ip == "223.5.5.5" {
			country = "CN"
		}
		_, _ = fmt.Fprintf(w, `{"ip":%q,"country_code":%q}`, ip, country)
	}))
	defer provider.Close()
	providerURL, err := url.Parse(provider.URL)
	require.NoError(t, err)
	originalTransport := http.DefaultTransport
	http.DefaultTransport = countryProxyTestTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "get.geojs.io", request.URL.Host, "test must never contact an external provider")
		cloned := request.Clone(request.Context())
		cloned.URL.Scheme, cloned.URL.Host = providerURL.Scheme, providerURL.Host
		return originalTransport.RoundTrip(cloned)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	for _, test := range []struct {
		name          string
		compat        bool
		snapshot      bool
		peer          string
		trusted       []string
		customHeaders []string
		headers       map[string]string
		wantIP        string
		wantStatus    int
	}{
		{"private_proxy_compat_on", true, true, "172.18.0.1:3456", nil, nil, map[string]string{"X-Forwarded-For": "8.8.8.8"}, "8.8.8.8", http.StatusForbidden},
		{"custom_header_compat_on", true, true, "172.18.0.1:3456", nil, []string{"X-Visitor-IP"}, map[string]string{"X-Visitor-IP": "223.5.5.5", "CF-Connecting-IP": "8.8.8.8"}, "223.5.5.5", http.StatusOK},
		{"private_proxy_compat_off", false, true, "172.18.0.1:3456", nil, nil, map[string]string{"X-Forwarded-For": "8.8.8.8"}, "", http.StatusServiceUnavailable},
		{"strict_trusted_proxy", false, true, "172.18.0.1:3456", []string{"172.18.0.1"}, []string{"X-Visitor-IP"}, map[string]string{"X-Forwarded-For": "223.5.5.5", "X-Visitor-IP": "8.8.8.8"}, "223.5.5.5", http.StatusOK},
		{"strict_direct_peer_rejects_spoof", false, true, "1.1.1.1:3456", nil, nil, map[string]string{"X-Forwarded-For": "223.5.5.5", "CF-IPCountry": "CN"}, "1.1.1.1", http.StatusForbidden},
		{"missing_snapshot_stays_strict", true, false, "1.1.1.1:3456", nil, nil, map[string]string{"X-Forwarded-For": "223.5.5.5"}, "1.1.1.1", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			queriedIPs = nil
			settings := &mockSettingsProvider{settings: map[string]any{"ui_country_restriction_enabled": true, "ui_blocked_countries": "US"}}
			frontend, _ := newUIAccessDomainRouter(t, settings)
			frontend.countryResolver = uiaccess.NewCountryResolver()
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(test.trusted))
			if test.snapshot {
				cfg := &config.Config{}
				cfg.SetForwardedClientIPSettings(test.compat, test.customHeaders)
				router.Use(middleware.SessionBindingContext(cfg))
			}
			router.Use(frontend.Middleware())
			router.NoRoute(func(c *gin.Context) { c.String(http.StatusAccepted, "callback-and-stream-untouched") })
			request := httptest.NewRequest(http.MethodGet, "/admin/settings?tab=security", nil)
			request.RemoteAddr = test.peer
			for name, value := range test.headers {
				request.Header.Set(name, value)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code)
			if test.wantIP == "" {
				require.Empty(t, queriedIPs)
				require.Equal(t, "non_public_ip", recorder.Header().Get("X-UI-Access-Reason"))
			} else {
				require.Equal(t, []string{test.wantIP}, queriedIPs)
			}
			before := len(queriedIPs)
			for _, path := range []string{"/api/v1/payment/notify/stripe", "/v1/responses", "/api/v1/settings/public"} {
				apiRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader("untouched-payload"))
				apiRequest.RemoteAddr = test.peer
				apiRecorder := httptest.NewRecorder()
				router.ServeHTTP(apiRecorder, apiRequest)
				require.Equal(t, http.StatusAccepted, apiRecorder.Code)
				require.Equal(t, "callback-and-stream-untouched", apiRecorder.Body.String())
			}
			require.Len(t, queriedIPs, before, "API paths must never invoke geo lookup")
		})
	}
}
