package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Serve a dashboard on a named HTTPS origin without DNS or real credentials.
type sub2APIFixture func(http.ResponseWriter, *http.Request)

func (f sub2APIFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	f(w, r)
	return w.Result(), nil
}
func useSub2APIFixture(t *testing.T, f sub2APIFixture) {
	t.Helper()
	previous := providerSessionHTTPClient
	providerSessionHTTPClient = func() (*http.Client, error) {
		jar, err := cookiejar.New(nil)
		return &http.Client{Transport: f, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, err
	}
	t.Cleanup(func() { providerSessionHTTPClient = previous })
}

func TestSub2APICompatibilityPreviewSaveAndImport(t *testing.T) {
	const host = "https://panel.example"
	for _, tc := range []struct{ name, prefix, published, configured, want string }{
		{"legacy origin", "", "", "", host},
		{"mounted dashboard and advertised gateway", "/tenant", host + "/tenant/relay/v1/", "", host + "/tenant/relay"},
		{"external advertisement requires opt-in", "/tenant", "https://models.example/api/v1", "", host + "/tenant"},
		{"explicit separate gateway", "/tenant", "", "https://models.example/proxy/v1beta/", "https://models.example/proxy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var exchanges, creates, settings int
			useSub2APIFixture(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "panel.example", r.URL.Host, "dashboard credentials may not leave the configured dashboard")
				base := tc.prefix + "/api/v1/"
				if !assert.True(t, strings.HasPrefix(r.URL.Path, base), r.URL.Path) {
					http.NotFound(w, r)
					return
				}
				p := strings.TrimPrefix(r.URL.Path, base)
				if p != "auth/refresh" && p != "settings/public" {
					assert.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
				}
				switch p {
				case "auth/refresh":
					exchanges++
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Empty(t, r.Header.Get("Authorization"))
					var body map[string]string
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, map[string]string{"refresh_token": "rt_test_input"}, body)
					if exchanges != 1 {
						writeProviderJSON(w, 401, `{"code":401,"reason":"REFRESH_TOKEN_INVALID"}`)
						return
					}
					http.SetCookie(w, &http.Cookie{Name: "panel", Value: "test-cookie", Path: "/"})
					writeProviderJSON(w, 200, `{"code":0,"data":{"access_token":"test-access","refresh_token":"rt_test_next","token_type":"Bearer"}}`)
				case "user/profile":
					writeProviderJSON(w, 200, `{"code":0,"data":{"balance":8}}`)
				case "usage/dashboard/stats":
					writeProviderJSON(w, 200, `{"code":0,"data":{"today_actual_cost":1.5}}`)
				case "settings/public":
					settings++
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.Empty(t, r.Header.Get("Cookie"))
					if tc.published == "" {
						http.NotFound(w, r)
						return
					}
					writeProviderJSON(w, 200, fmt.Sprintf(`{"code":0,"data":{"api_base_url":%q}}`, tc.published))
				case "groups/rates":
					writeProviderJSON(w, 200, `{"code":0,"data":{"7":0.5}}`)
				case "groups/available":
					writeProviderJSON(w, 200, `{"code":0,"data":[{"id":7,"name":"Relay","platform":"openai","status":"active","rate_multiplier":1}]}`)
				case "keys":
					if r.Method == http.MethodPost {
						creates++
						assert.NotEmpty(t, r.Header.Get("Idempotency-Key"))
						writeProviderJSON(w, 200, `{"code":0,"data":{"id":11}}`)
						return
					}
					writeProviderJSON(w, 200, `{"code":0,"data":{"total":1,"items":[{"id":11,"name":"test","key":"sk-test-model-only","group_id":7,"status":"active","group":{"name":"Relay","platform":"openai"}}]}}`)
				default:
					t.Errorf("unexpected dashboard request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			h := &AccountHandler{}
			h.SetProviderDependencies(service.NewSettingService(&providerTestSettingsRepo{}, &config.Config{}), providerTestEncryptor{})
			input := providerInput{Name: "dashboard", SiteType: "sub2api", BaseURL: host + tc.prefix, AuthMode: "token", RefreshToken: "rt_test_input"}
			if tc.prefix != "" {
				input.BaseURL += "/api/v1/"
			}
			if tc.configured != "" {
				input.GatewayURL = &tc.configured
			}
			preview := providerTestRequest(t, h.ProbeProvider, http.MethodPost, "/providers/probe", input, false)
			require.Equal(t, 409, preview.Code)
			require.Equal(t, "PROVIDER_REFRESH_NEEDS_SAVE", providerErrorReason(t, preview))
			require.Zero(t, exchanges)
			saved := providerTestRequest(t, h.CreateProvider, http.MethodPost, "/providers", input, false)
			require.Equal(t, 201, saved.Code, saved.Body.String())
			v := decodeProviderData[providerView](t, saved)
			require.Equal(t, "connected", v.Status)
			require.Equal(t, 8.0, *v.Metrics.Balance)
			require.NotContains(t, saved.Body.String(), "rt_test")
			require.NotContains(t, saved.Body.String(), "test-access")
			if tc.configured != "" {
				require.Zero(t, settings)
			} else {
				require.Equal(t, 1, settings)
			}
			prefix := "/providers/" + v.ID
			list := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", prefix+"/keys", "")
			require.Equal(t, 200, list.Code, list.Body.String())
			require.NotContains(t, list.Body.String(), "sk-test")
			require.Equal(t, tc.want, decodeProviderData[struct{ Endpoint string }](t, list).Endpoint)
			created := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", prefix+"/keys", `{"group_id":"7"}`)
			require.Equal(t, 200, created.Code, created.Body.String())
			require.Equal(t, 1, creates)
			for _, pt := range []string{"anthropic", "openai", "gemini", "antigravity", "grok", "custom"} {
				req := CreateAccountRequest{Platform: pt, Type: service.AccountTypeAPIKey, ProviderID: v.ID, ProviderKeyID: "11"}
				require.NoError(t, h.applyProviderKeyBinding(context.Background(), &req))
				want := tc.want
				if pt == "custom" || pt == "grok" {
					want += "/v1"
				}
				require.Equal(t, want, req.Credentials["base_url"], pt)
				require.Equal(t, "sk-test-model-only", req.Credentials["api_key"])
			}
			require.Equal(t, 1, exchanges)
		})
	}
}

func TestSub2APIConcurrentImportKeepsRotationAfterDashboardFailure(t *testing.T) {
	var exchanges atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	useSub2APIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			if exchanges.Add(1) != 1 {
				writeProviderJSON(w, 401, `{"code":401,"reason":"REFRESH_TOKEN_INVALID"}`)
				return
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
			writeProviderJSON(w, 200, `{"code":0,"data":{"access_token":"fresh-access","refresh_token":"rt_test_new"}}`)
		case "/api/v1/user/profile":
			writeProviderJSON(w, 200, `{"code":0,"data":{"balance":5}}`)
		case "/api/v1/usage/dashboard/stats":
			writeProviderJSON(w, 503, `{"code":503}`)
		case "/api/v1/keys":
			assert.Equal(t, "Bearer fresh-access", r.Header.Get("Authorization"))
			writeProviderJSON(w, 200, `{"code":0,"data":{"items":[],"total":0}}`)
		case "/api/v1/groups/rates":
			writeProviderJSON(w, 200, `{"code":0,"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	})
	// Prior versions could store a raw rt_ in the access-token slot.
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", SiteType: "sub2api", BaseURL: "https://panel.example", AuthMode: "token"}, "rt_test_old")
	stale, err := h.providerRecordByID(ctx, "relay")
	require.NoError(t, err)
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		h.refreshProviderBalances(ctx, &service.SystemSettings{}, nil, h.probeRecordContext)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("worker never exchanged RT")
	}
	imported := make(chan error, 1)
	go func() {
		imported <- h.withProviderSession(ctx, stale, func(s *providerSession) error { _, e := s.listKeys(ctx); return e })
	}()
	close(release)
	select {
	case err = <-imported:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("import blocked")
	}
	select {
	case <-worker:
	case <-ctx.Done():
		t.Fatal("worker blocked")
	}
	saved, err := h.providerRecordByID(ctx, "relay")
	require.NoError(t, err)
	require.Equal(t, "disconnected", saved.Status)
	require.NotEqual(t, "auth", saved.ErrorKind)
	raw, err := h.providerEncryptor.Decrypt(saved.SecretEncrypted)
	require.NoError(t, err)
	token, err := decodeProviderToken(raw)
	require.NoError(t, err)
	require.Equal(t, "rt_test_new", token.RefreshToken)
	require.Equal(t, int32(1), exchanges.Load())
}

func TestSub2APITerminalRTFailuresAreSpecific(t *testing.T) {
	for _, tc := range []struct {
		reason string
		want   error
	}{{"REFRESH_TOKEN_INVALID", errProviderRefreshInvalid}, {"REFRESH_TOKEN_REUSED", errProviderRefreshInvalid}, {"REFRESH_TOKEN_EXPIRED", errProviderRefreshExpired}, {"SESSION_BINDING_MISMATCH", errProviderSessionBound}, {"TOKEN_REVOKED", errProviderSessionRevoked}} {
		t.Run(tc.reason, func(t *testing.T) {
			calls := 0
			f := sub2APIFixture(func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeProviderJSON(w, 401, fmt.Sprintf(`{"code":401,"reason":%q,"message":"private-test-value"}`, tc.reason))
			})
			s := providerSession{record: providerRecord{SiteType: "sub2api", AuthMode: "token"}, secret: "rt_mock", remote: providerRemote{client: &http.Client{Transport: f}, base: "https://panel.example"}}
			err := s.authenticate(context.Background())
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, "auth", providerFailureKind(err))
			require.NotContains(t, providerPublicError(err), "private-test-value")
			require.Equal(t, 1, calls)
		})
	}
}

func TestSub2APIGatewayMountValidation(t *testing.T) {
	for _, raw := range []string{"http://gateway.example", "https://user@gateway.example", "https://gateway.example/a/../b", "https://gateway.example/a%2Fb", "https://gateway.example/a//b", "https://gateway.example/a?q=x", "https://gateway.example/#token", "https://127.0.0.1:443/mount"} {
		_, err := normalizeGatewayURL(raw)
		require.Error(t, err, raw)
	}
	for _, bad := range []string{"https://panel.example/tenant-other", "https://panel.example.evil.test/tenant", "https://other.example/tenant"} {
		r := providerRecord{SiteType: "sub2api", BaseURL: "https://panel.example/tenant", AdvertisedGatewayURL: bad}
		require.Equal(t, r.BaseURL, r.modelGateway())
	}
	_, err := normalizeDashboardURL("https://panel.example/mount", "newapi")
	require.Error(t, err)
}
