package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// This repository isolates onboarding tests from production data while using
// the actual AdminService for validation, normalization and persistence calls.
type onboardingAccountRepo struct {
	service.AdminAccountRepository
	accounts map[int64]*service.Account
	writes   int
}

func newOnboardingRepo() *onboardingAccountRepo {
	return &onboardingAccountRepo{accounts: make(map[int64]*service.Account)}
}

func (r *onboardingAccountRepo) Create(_ context.Context, a *service.Account) error {
	a.ID = int64(len(r.accounts) + 1)
	r.accounts[a.ID] = a
	r.writes++
	return nil
}
func (r *onboardingAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	a, ok := r.accounts[id]
	if !ok {
		return nil, service.ErrAccountNotFound
	}
	copy := *a
	return &copy, nil
}
func (r *onboardingAccountRepo) Update(_ context.Context, a *service.Account) error {
	r.accounts[a.ID] = a
	r.writes++
	return nil
}

func (r *onboardingAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	accounts := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		a, err := r.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r *onboardingAccountRepo) BulkUpdate(context.Context, []int64, service.AccountBulkUpdate) (int64, error) {
	r.writes++
	return 1, nil
}
func (r *onboardingAccountRepo) UpdateWithAccountBillingSettings(ctx context.Context, a *service.Account, _ *bool, _ *bool, _ *float64) error {
	return r.Update(ctx, a)
}
func (r *onboardingAccountRepo) CreateWithAccountGroups(ctx context.Context, a *service.Account, _ []service.AccountGroup) error {
	return r.Create(ctx, a)
}
func (r *onboardingAccountRepo) ListShadowsByParent(context.Context, int64) ([]*service.Account, error) {
	return nil, nil
}
func (r *onboardingAccountRepo) SetError(context.Context, int64, string) error            { return nil }
func (r *onboardingAccountRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }
func (r *onboardingAccountRepo) ClearRateLimit(context.Context, int64) error              { return nil }
func (r *onboardingAccountRepo) ClearTempUnschedulable(context.Context, int64) error      { return nil }

type onboardingProxyRepo struct{ service.ProxyRepository }

func (onboardingProxyRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]service.Proxy, *pagination.PaginationResult, error) {
	return nil, &pagination.PaginationResult{}, nil
}

type onboardingGroupRepo struct{ service.AdminGroupRepository }

func (onboardingGroupRepo) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}

func newOnboardingAdminService(repo *onboardingAccountRepo) service.AdminService {
	return service.NewAdminService(nil, onboardingGroupRepo{}, repo, onboardingProxyRepo{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func newOnboardingHandler(repo *onboardingAccountRepo, upstream service.HTTPUpstream) (*AccountHandler, *service.AccountTestService) {
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	probe := service.NewAccountTestService(repo, nil, nil, nil, nil, upstream, cfg, nil)
	h := NewAccountHandler(newOnboardingAdminService(repo), nil, nil, nil, nil, nil, nil, nil, probe, nil, nil, nil, nil)
	return h, probe
}

func onboardingRequest(t *testing.T, handle gin.HandlerFunc, value any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal("cannot serialize onboarding test input")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(logger.IntoContext(c.Request.Context(), zap.NewNop()))
	handle(c)
	return w
}

func importAPIKeyAccount(t *testing.T, h *AccountHandler, repo *onboardingAccountRepo, platform, baseURL, key, model string) *service.Account {
	t.Helper()
	w := onboardingRequest(t, h.ImportData, DataImportRequest{Data: DataPayload{Type: dataType, Version: dataVersion, Proxies: []DataProxy{}, Accounts: []DataAccount{{
		Name: "isolated-onboarding-" + platform, Platform: platform, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": key, "base_url": baseURL, "model_mapping": map[string]any{model: model}},
		Extra:       map[string]any{"openai_responses_supported": false}, Concurrency: 1, Priority: 50,
	}}}})
	var payload struct {
		Data DataImportResult `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil || w.Code != http.StatusOK || payload.Data.AccountCreated != 1 || payload.Data.AccountFailed != 0 {
		t.Fatalf("account import failed: status=%d created=%d failed=%d", w.Code, payload.Data.AccountCreated, payload.Data.AccountFailed)
	}
	a := repo.accounts[int64(len(repo.accounts))]
	if a == nil || a.Type != service.AccountTypeAPIKey {
		t.Fatal("import did not persist API key account")
	}
	return a
}

type onboardingUpstream struct {
	client        *http.Client
	allowedOrigin *url.URL
	model         string
	expectKey     string
	paths         []string
	statuses      []int
	invalidAuth   bool
}

func (u *onboardingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("invalid probe request")
	}
	u.paths = append(u.paths, req.URL.Path)
	if u.client != nil {
		if u.allowedOrigin == nil || req.URL.User != nil || req.URL.Opaque != "" ||
			!strings.EqualFold(req.URL.Scheme, u.allowedOrigin.Scheme) ||
			!strings.EqualFold(req.URL.Host, u.allowedOrigin.Host) ||
			(req.Host != "" && !strings.EqualFold(req.Host, u.allowedOrigin.Host)) {
			return nil, fmt.Errorf("probe request is outside the authorized origin")
		}
		// Bound the explicit paid smoke test while retaining the production
		// account test's endpoint, credentials and streaming response parser.
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/chat/completions") {
			var payload map[string]any
			if json.NewDecoder(req.Body).Decode(&payload) != nil {
				return nil, fmt.Errorf("invalid probe payload")
			}
			payload["max_completion_tokens"] = 256
			body, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}
		client := *u.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req) //nolint:gosec // G704: opt-in test requests are restricted to the configured base origin above, with redirects disabled.
		if resp != nil {
			u.statuses = append(u.statuses, resp.StatusCode)
		}
		return resp, err
	}
	validAuth := req.Header.Get("Authorization") == "Bearer "+u.expectKey || req.Header.Get("x-api-key") == u.expectKey || req.Header.Get("x-goog-api-key") == u.expectKey
	u.invalidAuth = u.invalidAuth || !validAuth
	body := ""
	contentType := "text/event-stream"
	switch {
	case req.Method == http.MethodGet:
		body = fmt.Sprintf("{\"data\":[{\"id\":%q}]}", u.model)
		contentType = "application/json"
	case strings.Contains(req.URL.Path, "streamGenerateContent"):
		body = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"
	case strings.HasSuffix(req.URL.Path, "/messages"):
		body = "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"ok\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n"
	case strings.HasSuffix(req.URL.Path, "/chat/completions"):
		body = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	default:
		body = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	}
	u.statuses = append(u.statuses, http.StatusOK)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func (u *onboardingUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

type onboardingRoundTripper func(*http.Request) (*http.Response, error)

func (f onboardingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOnboardingUpstreamRejectsUnauthorizedOrigins(t *testing.T) {
	for _, tc := range []struct {
		name     string
		url      string
		host     string
		noOrigin bool
	}{
		{name: "different_host", url: "https://other.example/v1/models"},
		{name: "different_scheme", url: "http://authorized.example/v1/models"},
		{name: "different_port", url: "https://authorized.example:8443/v1/models"},
		{name: "embedded_credentials", url: "https://user:pass@authorized.example/v1/models"},
		{name: "host_override", url: "https://authorized.example/v1/models", host: "other.example"},
		{name: "missing_authorization", url: "https://authorized.example/v1/models", noOrigin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := &onboardingUpstream{
				allowedOrigin: &url.URL{Scheme: "https", Host: "authorized.example"},
				client: &http.Client{Transport: onboardingRoundTripper(func(*http.Request) (*http.Response, error) {
					calls++
					return nil, fmt.Errorf("unexpected transport call")
				})},
			}
			if tc.noOrigin {
				upstream.allowedOrigin = nil
			}
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if tc.host != "" {
				req.Host = tc.host
			}
			resp, err := upstream.Do(req, "", 0, 0)
			if err == nil || resp != nil || calls != 0 {
				t.Fatalf("unauthorized request reached transport: calls=%d response=%t error=%v", calls, resp != nil, err)
			}
		})
	}
}

func TestOnboardingUpstreamDisablesRedirects(t *testing.T) {
	calls := 0
	upstream := &onboardingUpstream{
		allowedOrigin: &url.URL{Scheme: "https", Host: "authorized.example"},
		client: &http.Client{Transport: onboardingRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://other.example/v1/models"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		})},
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://authorized.example/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := upstream.Do(req, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect was followed: calls=%d status=%d", calls, resp.StatusCode)
	}
}

func verifyImportedAccountForward(t *testing.T, probe *service.AccountTestService, account *service.Account, model string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx = logger.IntoContext(ctx, zap.NewNop())
	models, err := probe.FetchUpstreamSupportedModels(ctx, account)
	if err != nil {
		t.Fatal("upstream model discovery failed (response body and credentials suppressed)")
	}
	found := false
	for _, available := range models {
		found = found || available == model
	}
	if !found {
		t.Fatal("configured model is absent from upstream model discovery")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/isolated/account-test", nil).WithContext(ctx)
	err = probe.TestAccountConnection(c, account.ID, model, "Reply with OK only.", service.AccountTestModeDefault)
	completed, content := false, false
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event service.TestEvent
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
			continue
		}
		completed = completed || event.Type == "test_complete" && event.Success
		content = content || event.Type == "content" && strings.TrimSpace(event.Text) != ""
	}
	if err != nil || !completed || !content {
		t.Fatalf("text forwarding failed: status=%d success=%t content=%t (upstream details suppressed)", w.Code, completed, content)
	}
	t.Logf("platform=%s imported=true model=%s discovery=true text_success=true", account.Platform, model)
}

func TestAPIKeyOnboardingSixPlatformContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ platform, model, path string }{
		{service.PlatformAnthropic, "claude-sonnet-test", "/v1/messages"},
		{service.PlatformOpenAI, "gpt-test", "/v1/chat/completions"},
		{service.PlatformGemini, "gemini-test", "/v1beta/models/gemini-test:streamGenerateContent"},
		{service.PlatformAntigravity, "claude-sonnet-test", "/antigravity/v1/messages"},
		{service.PlatformGrok, "grok-test", "/v1/responses"},
		{service.PlatformCustom, "custom-test", "/v1/chat/completions"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			repo := newOnboardingRepo()
			upstream := &onboardingUpstream{model: tc.model, expectKey: "fixture-key"}
			h, probe := newOnboardingHandler(repo, upstream)
			baseURL := "https://upstream.example"
			a := importAPIKeyAccount(t, h, repo, tc.platform, baseURL, "fixture-key", tc.model)
			verifyImportedAccountForward(t, probe, a, tc.model)
			if upstream.invalidAuth || len(upstream.paths) != 2 || upstream.paths[1] != tc.path {
				t.Fatalf("unexpected upstream contract: paths=%v authentication_valid=%t", upstream.paths, !upstream.invalidAuth)
			}
		})
	}
}

func TestAPIKeyOnboardingRejectsLegacyCreationPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, typ := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeUpstream, service.AccountTypeBedrock, service.AccountTypeServiceAccount} {
		t.Run(typ, func(t *testing.T) {
			repo := newOnboardingRepo()
			h, _ := newOnboardingHandler(repo, nil)
			item := CreateAccountRequest{Name: "rejected", Platform: service.PlatformOpenAI, Type: typ, Credentials: map[string]any{"api_key": "fixture-key"}}
			if w := onboardingRequest(t, h.Create, item); w.Code != http.StatusBadRequest {
				t.Fatalf("create status=%d", w.Code)
			}
			valid := item
			valid.Type = service.AccountTypeAPIKey
			if w := onboardingRequest(t, h.BatchCreate, map[string]any{"accounts": []CreateAccountRequest{valid, item}}); w.Code != http.StatusBadRequest {
				t.Fatalf("batch status=%d", w.Code)
			}
			w := onboardingRequest(t, h.ImportData, DataImportRequest{Data: DataPayload{Proxies: []DataProxy{}, Accounts: []DataAccount{{Name: item.Name, Platform: item.Platform, Type: typ, Credentials: item.Credentials}}}})
			var result struct {
				Data DataImportResult `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.AccountFailed != 1 || result.Data.AccountCreated != 0 {
				t.Fatal("legacy import was not rejected")
			}
			if repo.writes != 0 {
				t.Fatalf("rejected request caused %d writes", repo.writes)
			}
		})
	}
}

func TestAPIKeyOnboardingServiceCannotBypassPolicy(t *testing.T) {
	ctx := context.Background()
	for _, typ := range []string{service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeUpstream, service.AccountTypeBedrock, service.AccountTypeServiceAccount} {
		t.Run(typ, func(t *testing.T) {
			repo := newOnboardingRepo()
			admin := newOnboardingAdminService(repo)
			if _, err := admin.CreateAccount(ctx, &service.CreateAccountInput{Platform: service.PlatformOpenAI, Type: typ, Credentials: map[string]any{"api_key": "fixture-key"}, SkipDefaultGroupBind: true}); err == nil {
				t.Fatal("admin creation accepted legacy type")
			}
			if _, err := service.NewAccountService(repo, nil).Create(ctx, service.CreateAccountRequest{Platform: service.PlatformOpenAI, Type: typ, Credentials: map[string]any{"api_key": "fixture-key"}}); err == nil {
				t.Fatal("account service accepted legacy type")
			}
			repo.accounts[1] = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: typ, Credentials: map[string]any{"access_token": "fixture-token"}}
			if _, err := admin.DuplicateAccount(ctx, 1, "", ""); err == nil {
				t.Fatal("duplication accepted legacy type")
			}
			repo.accounts[2] = &service.Account{ID: 2, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key"}}
			if _, err := admin.UpdateAccount(ctx, 2, &service.UpdateAccountInput{Type: typ}); err == nil {
				t.Fatal("update converted API key account to legacy type")
			}
			if _, err := admin.UpdateAccount(ctx, 1, &service.UpdateAccountInput{Type: service.AccountTypeAPIKey}); err == nil {
				t.Fatal("legacy conversion accepted missing API key")
			}
			if repo.writes != 0 {
				t.Fatalf("rejected operations caused %d writes", repo.writes)
			}
			if _, err := admin.UpdateAccount(ctx, 1, &service.UpdateAccountInput{Name: "renamed legacy account", Type: typ}); err != nil {
				t.Fatal("existing legacy account maintenance was rejected")
			}
		})
	}
}

func TestAPIKeyOnboardingUpdatePreservesOmittedType(t *testing.T) {
	repo := newOnboardingRepo()
	repo.accounts[1] = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-old"}}
	updated, err := newOnboardingAdminService(repo).UpdateAccount(context.Background(), 1, &service.UpdateAccountInput{Credentials: map[string]any{"api_key": "fixture-new"}})
	if err != nil || updated == nil || updated.Type != service.AccountTypeAPIKey {
		t.Fatal("valid API key update with omitted type failed")
	}
}

func TestAPIKeyOnboardingBulkUpdateRejectsInvalidCredentials(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformOpenAI, service.PlatformGemini, service.PlatformAntigravity, service.PlatformGrok, service.PlatformCustom} {
		for _, emptyKey := range []any{nil, "", "   "} {
			t.Run(platform+fmt.Sprintf("/key-%v", emptyKey), func(t *testing.T) {
				repo := newOnboardingRepo()
				repo.accounts[1] = &service.Account{ID: 1, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://gateway.example"}}
				_, err := newOnboardingAdminService(repo).BulkUpdateAccounts(context.Background(), &service.BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{"api_key": emptyKey}})
				if err == nil || repo.writes != 0 {
					t.Fatal("bulk update accepted an invalid API key or wrote data before validation")
				}
			})
		}
	}
	for _, platform := range []string{service.PlatformAntigravity, service.PlatformCustom} {
		t.Run(platform+"/empty-base", func(t *testing.T) {
			repo := newOnboardingRepo()
			repo.accounts[1] = &service.Account{ID: 1, Platform: platform, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://gateway.example"}}
			_, err := newOnboardingAdminService(repo).BulkUpdateAccounts(context.Background(), &service.BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{"base_url": ""}})
			if err == nil || repo.writes != 0 {
				t.Fatal("bulk update removed a required upstream URL")
			}
		})
	}
}

// Opt-in only: the caller securely supplies credentials. The tests never read a
// production DB and never print request headers, response content or API keys.
func TestLiveAPIKeyImportAndForward(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("LIVE_ACCOUNT_TEST_API_KEY"))
	if key == "" {
		t.Skip("LIVE_ACCOUNT_TEST_API_KEY is not configured")
	}
	baseURL := strings.TrimSpace(os.Getenv("LIVE_ACCOUNT_TEST_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("LIVE_ACCOUNT_TEST_MODEL"))
	if baseURL == "" || model == "" {
		t.Fatal("LIVE_ACCOUNT_TEST_BASE_URL and LIVE_ACCOUNT_TEST_MODEL are required")
	}
	allowedOrigin, err := url.Parse(baseURL)
	if err != nil || allowedOrigin.Hostname() == "" || allowedOrigin.User != nil ||
		(allowedOrigin.Scheme != "https" && allowedOrigin.Scheme != "http") {
		t.Fatal("LIVE_ACCOUNT_TEST_BASE_URL must be an HTTP(S) URL without embedded credentials")
	}
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformCustom} {
		t.Run(platform, func(t *testing.T) {
			repo := newOnboardingRepo()
			upstream := &onboardingUpstream{client: &http.Client{Timeout: 75 * time.Second}, allowedOrigin: allowedOrigin}
			h, probe := newOnboardingHandler(repo, upstream)
			a := importAPIKeyAccount(t, h, repo, platform, baseURL, key, model)
			verifyImportedAccountForward(t, probe, a, model)
			t.Logf("platform=%s upstream_statuses=%v", platform, upstream.statuses)
		})
	}
}
