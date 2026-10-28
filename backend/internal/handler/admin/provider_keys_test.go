package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useProviderTestServer routes provider sessions to a local TLS server. Each
// session receives its own cookie jar, as providerHTTPClient does.
func useProviderTestServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	previous := providerSessionHTTPClient
	providerSessionHTTPClient = func() (*http.Client, error) {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, err
		}
		client := *server.Client()
		client.Jar = jar
		return &client, nil
	}
	t.Cleanup(func() { providerSessionHTTPClient = previous })
}

func newProviderKeyTestHandler(t *testing.T, record providerRecord, secret string) *AccountHandler {
	t.Helper()
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(&providerTestSettingsRepo{}, &config.Config{}), providerTestEncryptor{})
	ciphertext, err := providerTestEncryptor{}.Encrypt(secret)
	require.NoError(t, err)
	record.SecretEncrypted = ciphertext
	require.NoError(t, h.providerSettings.SaveProviderRecords(context.Background(), []providerRecord{record}))
	return h
}

func providerKeyTestRequest(t *testing.T, handler gin.HandlerFunc, method, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, route, handler)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func writeProviderJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func decodeProviderData[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var payload struct {
		Data T `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), recorder.Body.String())
	return payload.Data
}

func providerErrorReason(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), recorder.Body.String())
	return payload.Reason
}

func TestListProviderKeysReturnsSub2APIMetadataWithoutSecrets(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"access_token":"session-token"}}`)
		case "/api/v1/keys":
			assert.Equal(t, "Bearer session-token", r.Header.Get("Authorization"))
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"total":4,"page":1,"page_size":100,"pages":1,"items":[
				{"id":11,"key":"sk-active-secret","name":"[杂鱼]Codex &amp; Claude","group_id":7,"status":"active","quota":0,"quota_used":3,"group":{"id":7,"name":"CodexPro","platform":"openai","rate_multiplier":0.5}},
				{"id":12,"key":"sk-disabled-secret","name":"off","group_id":7,"status":"inactive","group":{"id":7,"name":"CodexPro","platform":"openai","rate_multiplier":0.5}},
				{"id":13,"key":"sk-expired-secret","name":"old","group_id":8,"status":"active","expires_at":"2001-01-01T00:00:00Z","group":{"id":8,"name":"Gemini","platform":"gemini","rate_multiplier":1}},
				{"id":14,"key":"sk-spent-secret","name":"spent","group_id":8,"status":"active","quota":5,"quota_used":5,"group":{"id":8,"name":"Gemini","platform":"gemini","rate_multiplier":1}}
			]}}`)
		case "/api/v1/groups/rates":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"7":0.2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "vendor", Name: "Vendor", SiteType: "sub2api", BaseURL: server.URL, AuthMode: "password", Account: "ops@example.com"}, "dashboard-password")

	recorder := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/vendor/keys", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "secret", "model API keys must never reach the browser")
	data := decodeProviderData[struct {
		ProviderID string            `json:"provider_id"`
		Endpoint   string            `json:"endpoint"`
		Keys       []providerKeyView `json:"keys"`
	}](t, recorder)
	require.Equal(t, "vendor", data.ProviderID)
	require.Equal(t, server.URL, data.Endpoint)
	keys := data.Keys
	require.Len(t, keys, 4)
	require.Equal(t, "11", keys[0].ID)
	require.Equal(t, "[杂鱼]Codex & Claude", keys[0].Name)
	require.Equal(t, "CodexPro", keys[0].GroupName)
	require.Equal(t, "openai", keys[0].GroupPlatform)
	require.Equal(t, 0.2, *keys[0].GroupMultiplier, "the account's own group rate wins")
	require.Equal(t, 1.0, *keys[2].GroupMultiplier)
	require.Equal(t, []string{"active", "disabled", "expired", "quota_exhausted"}, []string{keys[0].Status, keys[1].Status, keys[2].Status, keys[3].Status})
	require.Equal(t, []bool{true, false, false, false}, []bool{keys[0].Available, keys[1].Available, keys[2].Available, keys[3].Available})
}

func TestProviderSessionRenewsSub2APITokenAndPersistsRotation(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshes.Add(1)
			assert.Empty(t, r.Header.Get("Authorization"))
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"access_token":"fresh-access","refresh_token":"fresh-refresh","token_type":"Bearer"}}`)
		case "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer fresh-access" {
				writeProviderJSON(w, http.StatusUnauthorized, `{"code":401,"message":"token expired"}`)
				return
			}
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"total":0,"items":[]}}`)
		case "/api/v1/groups/rates":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	secret, err := encodeProviderToken("stale-access", "single-use-refresh", "Bearer")
	require.NoError(t, err)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "vendor", SiteType: "sub2api", BaseURL: server.URL, AuthMode: "token"}, secret)

	recorder := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/vendor/keys", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.EqualValues(t, 1, refreshes.Load())
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	stored, err := h.providerEncryptor.Decrypt(records[0].SecretEncrypted)
	require.NoError(t, err)
	require.Contains(t, stored, "fresh-access")
	require.Contains(t, stored, "fresh-refresh", "a rotated refresh token must survive the request")
}

func TestApplyProviderKeyBindingRevealsMaskedNewAPIKey(t *testing.T) {
	var reveals atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer system-token", r.Header.Get("Authorization"))
		assert.Equal(t, "42", r.Header.Get("New-Api-User"))
		switch r.URL.Path {
		case "/api/token/":
			assert.Equal(t, "1", r.URL.Query().Get("p"))
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"page":1,"page_size":100,"total":2,"items":[
				{"id":7,"name":"vip key","key":"abcd**********wxyz","status":1,"group":"vip","expired_time":-1,"remain_quota":0,"unlimited_quota":true},
				{"id":8,"name":"default key","key":"efgh**********stuv","status":1,"group":"","expired_time":-1,"remain_quota":100,"unlimited_quota":false}
			]}}`)
		case "/api/token/7/key":
			assert.Equal(t, http.MethodPost, r.Method)
			reveals.Add(1)
			writeProviderJSON(w, http.StatusOK, `{"success":true,"data":{"key":"full-vip-secret"}}`)
		case "/api/user/self/groups":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"data":{"vip":{"ratio":0.5,"desc":"VIP"},"default":{"ratio":1,"desc":"Default"},"auto":{"ratio":"自动","desc":"Auto"}}}`)
		case "/api/user/self":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"data":{"group":"default"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	secret, err := encodeProviderToken("system-token", "", "Bearer")
	require.NoError(t, err)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", Name: "Relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "token", UserID: "42"}, secret)

	recorder := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/relay/keys", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	keys := decodeProviderData[struct {
		Keys []providerKeyView `json:"keys"`
	}](t, recorder).Keys
	require.Len(t, keys, 2)
	require.Equal(t, "vip", keys[0].GroupName)
	require.Equal(t, 0.5, *keys[0].GroupMultiplier)
	require.Equal(t, "default", keys[1].GroupName, "a token without a group uses the account's group")
	require.Equal(t, 1.0, *keys[1].GroupMultiplier)
	require.Zero(t, reveals.Load(), "listing must not reveal complete keys")

	req := CreateAccountRequest{
		Name: "imported", Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"a": "b"}}, ProviderID: "relay", ProviderKeyID: "7",
	}
	require.NoError(t, h.applyProviderKeyBinding(context.Background(), &req))
	require.Equal(t, "sk-full-vip-secret", req.Credentials["api_key"])
	require.Equal(t, server.URL+"/v1", req.Credentials["base_url"])
	require.Equal(t, map[string]any{"a": "b"}, req.Credentials["model_mapping"])
	binding, ok := req.Extra[providerKeyBindingExtraKey].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "relay", binding["provider_id"])
	require.Equal(t, "7", binding["key_id"])
	require.Equal(t, "vip", binding["group_name"])
	require.Equal(t, 0.5, binding["group_multiplier"])
	require.NotContains(t, fmt.Sprint(binding), "secret")
	require.EqualValues(t, 1, reveals.Load())
	require.NoError(t, service.ValidateNewAccountCredentials(req.Platform, req.Type, req.Credentials))
}

func TestApplyProviderKeyBindingRefusesUnsafeSelections(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"access_token":"session-token"}}`)
		case "/api/v1/keys":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"total":3,"items":[
				{"id":11,"key":"sk-live-11","name":"live","group_id":7,"status":"active","group":{"id":7,"name":"Claude","platform":"anthropic","rate_multiplier":1}},
				{"id":12,"key":"sk-off-12","name":"off","group_id":7,"status":"disabled"},
				{"id":13,"key":"sk-****","name":"masked","group_id":7,"status":"active"}
			]}}`)
		case "/api/v1/groups/rates", "/api/v1/groups/available":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "vendor", Name: "Vendor", SiteType: "sub2api", BaseURL: server.URL, AuthMode: "password", Account: "ops@example.com"}, "dashboard-password")
	ctx := context.Background()
	reason := func(req CreateAccountRequest) string {
		if req.Credentials == nil {
			req.Credentials = map[string]any{}
		}
		return infraerrors.Reason(h.applyProviderKeyBinding(ctx, &req))
	}

	require.Equal(t, "PROVIDER_BINDING_CONFLICT", reason(CreateAccountRequest{Platform: "openai", Credentials: map[string]any{"api_key": "sk-manual"}, ProviderID: "vendor", ProviderKeyID: "11"}))
	require.Equal(t, "PROVIDER_BINDING_INCOMPLETE", reason(CreateAccountRequest{Platform: "openai", ProviderID: "vendor"}))
	require.Equal(t, "PROVIDER_KEY_ID_INVALID", reason(CreateAccountRequest{Platform: "openai", ProviderID: "vendor", ProviderKeyID: "../11"}))
	require.Equal(t, "PROVIDER_NOT_FOUND", reason(CreateAccountRequest{Platform: "openai", ProviderID: "deleted", ProviderKeyID: "11"}))
	require.Equal(t, "PROVIDER_KEY_UNAVAILABLE", reason(CreateAccountRequest{Platform: "openai", ProviderID: "vendor", ProviderKeyID: "12"}))
	require.Equal(t, "PROVIDER_KEY_SECRET_UNAVAILABLE", reason(CreateAccountRequest{Platform: "openai", ProviderID: "vendor", ProviderKeyID: "13"}))
	require.Equal(t, "PROVIDER_KEY_NOT_FOUND", reason(CreateAccountRequest{Platform: "openai", ProviderID: "vendor", ProviderKeyID: "99"}))

	manual := CreateAccountRequest{Platform: "openai", Credentials: map[string]any{"api_key": "sk-manual"}}
	require.NoError(t, h.applyProviderKeyBinding(ctx, &manual))
	require.Equal(t, map[string]any{"api_key": "sk-manual"}, manual.Credentials, "manual API keys are untouched")
	require.Nil(t, manual.Extra)

	imported := CreateAccountRequest{Platform: service.PlatformAnthropic, Credentials: map[string]any{}, ProviderID: "vendor", ProviderKeyID: "11"}
	require.NoError(t, h.applyProviderKeyBinding(ctx, &imported))
	require.Equal(t, "sk-live-11", imported.Credentials["api_key"])
	require.Equal(t, server.URL, imported.Credentials["base_url"])
}

func TestCreateProviderKeyUsesChosenGroupAndNeverRetriesCreation(t *testing.T) {
	var posts atomic.Int32
	var failCreation atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"access_token":"session-token"}}`)
		case "/api/v1/groups/available":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":[
				{"id":12,"name":"Gemini-默认通道","platform":"gemini","rate_multiplier":0.25,"status":"active"},
				{"id":13,"name":"CodexPro","platform":"openai","rate_multiplier":0.18,"status":"active"}
			]}`)
		case "/api/v1/groups/rates":
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{}}`)
		case "/api/v1/keys":
			assert.Equal(t, http.MethodPost, r.Method)
			posts.Add(1)
			assert.True(t, strings.HasPrefix(r.Header.Get("Idempotency-Key"), "provider-key-"))
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "[杂鱼]Gemini-默认通道", body["name"])
			assert.Equal(t, float64(12), body["group_id"])
			if failCreation.Load() {
				writeProviderJSON(w, http.StatusBadGateway, `{"code":502,"message":"gateway timeout"}`)
				return
			}
			writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"id":99,"name":"[杂鱼]Gemini-默认通道"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "vendor", SiteType: "sub2api", BaseURL: server.URL, AuthMode: "password", Account: "ops@example.com"}, "dashboard-password")

	groupsRecorder := providerKeyTestRequest(t, h.ListProviderKeyGroups, http.MethodGet, "/providers/:id/groups", "/providers/vendor/groups", "")
	require.Equal(t, http.StatusOK, groupsRecorder.Code, groupsRecorder.Body.String())
	groups := decodeProviderData[struct {
		Groups []providerGroupView `json:"groups"`
	}](t, groupsRecorder).Groups
	require.Len(t, groups, 2)
	require.Equal(t, "[杂鱼]Gemini-默认通道", groups[0].KeyName)
	require.Equal(t, "gemini", groups[0].Platform)
	require.Equal(t, 0.25, *groups[0].Multiplier)

	created := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", "/providers/vendor/keys", `{"group_id":"12"}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	result := decodeProviderData[struct {
		Created bool   `json:"created"`
		Name    string `json:"name"`
		GroupID string `json:"group_id"`
		KeyID   string `json:"key_id"`
	}](t, created)
	require.True(t, result.Created)
	require.Equal(t, "[杂鱼]Gemini-默认通道", result.Name)
	require.Equal(t, "12", result.GroupID)
	require.Equal(t, "99", result.KeyID)
	require.EqualValues(t, 1, posts.Load())

	unavailable := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", "/providers/vendor/keys", `{"group_id":"404"}`)
	require.Equal(t, http.StatusBadRequest, unavailable.Code)
	require.Equal(t, "PROVIDER_GROUP_UNAVAILABLE", providerErrorReason(t, unavailable))
	require.EqualValues(t, 1, posts.Load(), "an unavailable group must not reach the creation endpoint")

	failCreation.Store(true)
	uncertain := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", "/providers/vendor/keys", `{"group_id":"12"}`)
	require.Equal(t, http.StatusBadGateway, uncertain.Code)
	require.Equal(t, "PROVIDER_KEY_CREATE_UNCERTAIN", providerErrorReason(t, uncertain))
	require.EqualValues(t, 2, posts.Load(), "a failed creation is never retried")
}

func TestCreateNewAPIKeyFindsAcknowledgedTokenByName(t *testing.T) {
	var created atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/login" {
			assert.True(t, strings.Contains(r.Header.Get("Cookie"), "session=signed-in"))
			assert.Equal(t, "5", r.Header.Get("New-Api-User"))
		}
		switch {
		case r.URL.Path == "/api/user/login":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "signed-in", Path: "/", Secure: true})
			writeProviderJSON(w, http.StatusOK, `{"success":true,"data":{"id":5,"username":"ops"}}`)
		case r.URL.Path == "/api/user/self/groups":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"data":{"vip":{"ratio":0.5,"desc":"VIP"}}}`)
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "[杂鱼]vip", body["name"])
			assert.Equal(t, "vip", body["group"])
			assert.Equal(t, true, body["unlimited_quota"])
			created.Store(true)
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":""}`)
		case r.URL.Path == "/api/token/":
			rows := `{"id":3,"name":"[杂鱼]vip","key":"old","status":1}`
			total := 1
			if created.Load() {
				rows += `,{"id":4,"name":"[杂鱼]vip","key":"new","status":1}`
				total = 2
			}
			writeProviderJSON(w, http.StatusOK, fmt.Sprintf(`{"success":true,"data":{"total":%d,"items":[%s]}}`, total, rows))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "password", Account: "ops"}, "dashboard-password")

	recorder := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", "/providers/relay/keys", `{"group_id":"vip"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	result := decodeProviderData[struct {
		Name  string `json:"name"`
		KeyID string `json:"key_id"`
	}](t, recorder)
	require.Equal(t, "[杂鱼]vip", result.Name)
	require.Equal(t, "4", result.KeyID, "the pre-existing token with the same name is not the new one")
}

func TestLegacyNewAPIKeyPagesStartAtZero(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages := map[string]string{"0": `[{"id":1},{"id":2}]`, "1": `[{"id":3}]`}
		rows, ok := pages[r.URL.Query().Get("p")]
		if !ok {
			rows = `[]`
		}
		writeProviderJSON(w, http.StatusOK, `{"success":true,"data":`+rows+`}`)
	}))
	defer server.Close()
	session := &providerSession{record: providerRecord{SiteType: "newapi"}, remote: providerRemote{client: server.Client(), base: server.URL}}
	rows, err := session.newAPIKeyRows(context.Background())
	require.NoError(t, err)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, providerText(row["id"]))
	}
	require.Equal(t, []string{"1", "2", "3"}, ids)
}

func TestProviderKeyPagesWithoutTotalContinueAfterShortPage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some providers cap page_size below the requested 100 and omit total.
		pages := map[string]string{"1": `[{"id":1}]`, "2": `[{"id":2}]`}
		rows := pages[r.URL.Query().Get("page")]
		if rows == "" {
			rows = `[]`
		}
		writeProviderJSON(w, http.StatusOK, `{"code":0,"data":{"items":`+rows+`}}`)
	}))
	defer server.Close()
	session := &providerSession{record: providerRecord{SiteType: "sub2api"}, remote: providerRemote{client: server.Client(), base: server.URL}}
	rows, err := session.listRows(context.Background(), func(page int) string {
		return fmt.Sprintf("/keys?page=%d&page_size=100", page)
	}, false)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestProviderRequestTreatsExplainedForbiddenAsRefusal(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/explained" {
			writeProviderJSON(w, http.StatusForbidden, `{"code":403,"reason":"GROUP_NOT_ALLOWED","message":"user is not allowed to bind this group"}`)
			return
		}
		writeProviderJSON(w, http.StatusForbidden, `{}`)
	}))
	defer server.Close()
	remote := providerRemote{client: server.Client(), base: server.URL}
	_, _, err := remote.requestData(context.Background(), http.MethodPost, "/explained", map[string]any{}, nil)
	var refused *providerAPIError
	require.True(t, errors.As(err, &refused))
	require.Contains(t, providerFailureMessage(err), "not allowed to bind this group")
	_, _, err = remote.requestData(context.Background(), http.MethodGet, "/silent", nil, nil)
	require.ErrorIs(t, err, errProviderAuthentication)
}

func TestProviderKeyErrorsNeverReportUnauthorized(t *testing.T) {
	for _, err := range []error{
		errProviderAuthentication, errProviderTurnstile, errProviderStoredLogin,
		&providerRemoteError{operation: "/api/v1/keys", status: http.StatusUnauthorized},
		errors.New("dial tcp: i/o timeout"),
	} {
		code := infraerrors.Code(providerKeyError(err))
		require.Equal(t, http.StatusBadGateway, code, err.Error())
	}
	uncertain := providerKeyError(providerCreateFailure(errors.New("connection reset")))
	require.Equal(t, "PROVIDER_KEY_CREATE_UNCERTAIN", infraerrors.Reason(uncertain))
	refused := providerCreateFailure(&providerAPIError{status: http.StatusOK, message: "令牌名称过长"})
	require.NotEqual(t, "PROVIDER_KEY_CREATE_UNCERTAIN", infraerrors.Reason(providerKeyError(refused)))
	require.Contains(t, infraerrors.Message(providerKeyError(refused)), "令牌名称过长")
}

func TestProviderAccountBaseURLMatchesPlatformRequestBuilders(t *testing.T) {
	origin := "https://relay.example/"
	for platform, want := range map[string]string{
		service.PlatformAnthropic: "https://relay.example",
		service.PlatformOpenAI:    "https://relay.example",
		service.PlatformGemini:    "https://relay.example",
		service.PlatformGrok:      "https://relay.example/v1",
		service.PlatformCustom:    "https://relay.example/v1",
	} {
		require.Equal(t, want, providerAccountBaseURL(origin, platform), platform)
	}
}

func TestProviderKeyNameFitsUpstreamLimits(t *testing.T) {
	require.Equal(t, "[杂鱼]Gemini-默认通道", providerKeyName("sub2api", " Gemini-默认通道 "))
	short := providerKeyName("newapi", "超长的分组名称超长的分组名称")
	require.LessOrEqual(t, len(short), 30)
	require.True(t, utf8.ValidString(short))
	require.True(t, strings.HasPrefix(short, providerKeyNamePrefix))
	escaped := providerKeyName("sub2api", strings.Repeat("&", 200))
	require.LessOrEqual(t, len(html.EscapeString(escaped)), 100)
}

func TestCompleteProviderSecretRejectsMaskedKeys(t *testing.T) {
	require.True(t, completeProviderSecret("sk-AbC123_-xyz"))
	for _, secret := range []string{"", "sk-abc****xyz", "sk-abc...xyz", "sk-abc xyz", "sk-abc•xyz", strings.Repeat("a", 4097)} {
		require.False(t, completeProviderSecret(secret), secret)
	}
}
