package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures follow QuantumNous/new-api as of e449cc7 (2026-10-28) and the
// releases before its July 2026 sign-in rewrite. Tokens are placeholders.
const testNewAPIPersonalToken = "nap_EXAMPLE_PERSONAL_ACCESS_TOKEN"

func newAPITokenSecret(t *testing.T, token string) string {
	t.Helper()
	secret, err := encodeProviderToken(token, "", "Bearer")
	require.NoError(t, err)
	return secret
}

func TestNewAPIPersonalAccessTokenProbeSendsOnlyBearer(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		assert.Empty(t, r.Header.Values("New-Api-User"), r.URL.Path)
		switch r.URL.Path {
		case "/api/user/self":
			assert.Equal(t, "Bearer "+testNewAPIPersonalToken, r.Header.Get("Authorization"))
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"id":42,"username":"ops","quota":1000000,"used_quota":250000,"group":"default"}}`)
		case "/api/status":
			assert.Empty(t, r.Header.Get("Authorization"), "the public status endpoint never receives the token")
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota_per_unit":500000}}`)
		case "/api/log/self/stat":
			assert.Equal(t, "Bearer "+testNewAPIPersonalToken, r.Header.Get("Authorization"))
			assert.Equal(t, "2", r.URL.Query().Get("type"))
			assert.NotEmpty(t, r.URL.Query().Get("start_timestamp"))
			assert.NotEmpty(t, r.URL.Query().Get("end_timestamp"))
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota":100000,"rpm":0,"tpm":0}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	probe, err := probeProviderAccount(context.Background(), providerRecord{SiteType: "newapi", AuthMode: "token", BaseURL: server.URL},
		newAPITokenSecret(t, testNewAPIPersonalToken), server.Client())
	require.NoError(t, err)
	require.Equal(t, 2.0, *probe.metrics.Balance)
	require.Equal(t, 0.5, *probe.metrics.TotalSpend)
	require.Equal(t, 0.2, *probe.metrics.TodaySpend)
	require.Equal(t, "42", probe.userID)
	require.Equal(t, []string{"/api/user/self", "/api/status", "/api/log/self/stat"}, paths)
}

func TestNewAPIPersonalAccessTokenImportsKeysWithoutUserID(t *testing.T) {
	var reveals atomic.Int32
	var created atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer "+testNewAPIPersonalToken, r.Header.Get("Authorization"), r.URL.Path)
		assert.Empty(t, r.Header.Values("New-Api-User"), r.URL.Path)
		switch {
		case r.URL.Path == "/api/token/" && r.Method == http.MethodGet:
			rows := `{"id":7,"name":"vip key","key":"abcd**********wxyz","status":1,"group":"vip","expired_time":-1,"remain_quota":0,"unlimited_quota":true}`
			total := 1
			if created.Load() {
				rows += `,{"id":8,"name":"[杂鱼]vip","key":"efgh**********stuv","status":1,"group":"vip","expired_time":-1,"remain_quota":0,"unlimited_quota":true}`
				total = 2
			}
			writeProviderJSON(w, http.StatusOK, fmt.Sprintf(`{"success":true,"message":"","data":{"page":1,"page_size":100,"total":%d,"items":[%s]}}`, total, rows))
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "vip", body["group"])
			created.Store(true)
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":""}`)
		case r.URL.Path == "/api/token/7/key" && r.Method == http.MethodPost:
			reveals.Add(1)
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"key":"full-vip-secret"}}`)
		case r.URL.Path == "/api/user/self/groups":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"vip":{"ratio":0.5,"desc":"VIP"},"auto":{"ratio":"auto","desc":"Auto"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", Name: "Relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "token"},
		newAPITokenSecret(t, testNewAPIPersonalToken))

	listed := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/relay/keys", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	keys := decodeProviderData[struct {
		Keys []providerKeyView `json:"keys"`
	}](t, listed).Keys
	require.Len(t, keys, 1)
	require.Equal(t, 0.5, *keys[0].GroupMultiplier)

	groups := providerKeyTestRequest(t, h.ListProviderKeyGroups, http.MethodGet, "/providers/:id/groups", "/providers/relay/groups", "")
	require.Equal(t, http.StatusOK, groups.Code, groups.Body.String())
	groupViews := decodeProviderData[struct {
		Groups []providerGroupView `json:"groups"`
	}](t, groups).Groups
	require.Len(t, groupViews, 2)
	require.Equal(t, "vip", groupViews[1].ID)
	require.Equal(t, 0.5, *groupViews[1].Multiplier)

	createdKey := providerKeyTestRequest(t, h.CreateProviderKey, http.MethodPost, "/providers/:id/keys", "/providers/relay/keys", `{"group_id":"vip"}`)
	require.Equal(t, http.StatusOK, createdKey.Code, createdKey.Body.String())
	require.Equal(t, "8", decodeProviderData[struct {
		KeyID string `json:"key_id"`
	}](t, createdKey).KeyID)

	req := CreateAccountRequest{Name: "imported", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{}, ProviderID: "relay", ProviderKeyID: "7"}
	require.NoError(t, h.applyProviderKeyBinding(context.Background(), &req))
	require.Equal(t, "sk-full-vip-secret", req.Credentials["api_key"])
	require.EqualValues(t, 1, reveals.Load())
}

func TestNewAPIPasswordLoginUsesReturnedBearerToken(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login/encryption-key":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"enabled":false}}`)
			return
		case "/api/user/login":
			logins.Add(1)
			var body map[string]string
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, map[string]string{"username": "ops", "password": "dashboard-password"}, body)
			http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "browser-refresh", Path: "/api/user/auth", Secure: true, HttpOnly: true})
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"access_token":"short-lived-access","token_type":"Bearer","access_expires_at":1893456000,"session":{"id":"s1"},"user":{"id":5,"username":"ops"}}}`)
			return
		}
		assert.Equal(t, "Bearer short-lived-access", r.Header.Get("Authorization"), r.URL.Path)
		assert.Empty(t, r.Header.Values("New-Api-User"), r.URL.Path)
		switch r.URL.Path {
		case "/api/token/":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"page":1,"page_size":100,"total":1,"items":[{"id":3,"name":"main","key":"abcd**********wxyz","status":1,"group":"default","expired_time":-1,"unlimited_quota":true}]}}`)
		case "/api/user/self/groups":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"default":{"ratio":1,"desc":"Default"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "password", Account: "ops"}, "dashboard-password")

	recorder := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/relay/keys", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, decodeProviderData[struct {
		Keys []providerKeyView `json:"keys"`
	}](t, recorder).Keys, 1)
	require.EqualValues(t, 1, logins.Load())
}

func TestNewAPIPasswordLoginStopsAtVerification(t *testing.T) {
	for _, data := range []string{
		`{"require_verification":true,"flow_token":"flow","expires_at":1893456000,"methods":[{"method":"totp","available":true}]}`,
		`{"require_2fa":true}`,
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/user/login" {
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":`+data+`}`)
				return
			}
			http.NotFound(w, r)
		}))
		_, err := probeProviderAccount(context.Background(), providerRecord{SiteType: "newapi", AuthMode: "password", BaseURL: server.URL, Account: "ops"}, "dashboard-password", server.Client())
		server.Close()
		require.ErrorIs(t, err, errProviderTwoFactor, data)
		require.Equal(t, "auth", providerFailureKind(err))
	}
}

// decryptNewAPIPassword mirrors New API's common.DecryptPassword.
func decryptNewAPIPassword(key *rsa.PrivateKey, keyID, payload string) (string, error) {
	if !strings.HasPrefix(payload, "v2.") {
		ciphertext, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", err
		}
		plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, ciphertext, nil)
		return string(plain), err
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 4 {
		return "", errors.New("malformed v2 payload")
	}
	decoded := make([][]byte, 3)
	for i, part := range parts[1:] {
		value, err := base64.StdEncoding.Strict().DecodeString(part)
		if err != nil {
			return "", err
		}
		decoded[i] = value
	}
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, decoded[0], []byte("password-v2"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, decoded[1], decoded[2], []byte("password-v2:"+keyID))
	return string(plain), err
}

func TestNewAPIPasswordLoginEncryptsWhenTheSiteRequiresIt(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	publicKey, err := json.Marshal(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	require.NoError(t, err)
	const keyID = "0123456789abcdef0123456789abcdef"

	for _, password := range []string{"dashboard-password", strings.Repeat("长密码", 40)} {
		var received sync.Map
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/user/login/encryption-key":
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"enabled":true,"kid":"`+keyID+`","public_key":`+string(publicKey)+`}}`)
			case "/api/user/login":
				var body map[string]string
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, plain := body["password"]
				assert.False(t, plain, "an encrypted login must not also send the plain password")
				assert.Equal(t, keyID, body["encryption_key_id"])
				decrypted, err := decryptNewAPIPassword(key, body["encryption_key_id"], body["password_encrypted"])
				assert.NoError(t, err)
				received.Store("password", decrypted)
				received.Store("v2", strings.HasPrefix(body["password_encrypted"], "v2."))
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"access_token":"short-lived-access","token_type":"Bearer","user":{"id":5}}}`)
			case "/api/user/self":
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"id":5,"quota":500000}}`)
			case "/api/status":
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota_per_unit":500000}}`)
			default:
				http.NotFound(w, r)
			}
		}))
		probe, err := probeProviderAccount(context.Background(), providerRecord{SiteType: "newapi", AuthMode: "password", BaseURL: server.URL, Account: "ops"}, password, server.Client())
		server.Close()
		require.NoError(t, err)
		require.Equal(t, 1.0, *probe.metrics.Balance)
		decrypted, _ := received.Load("password")
		require.Equal(t, password, decrypted)
		usedV2, _ := received.Load("v2")
		require.Equal(t, len(password) > 190, usedV2, "only a password longer than the RSA limit uses the v2 envelope")
	}
}

func TestNewAPIAccessRefusalsAreReportedDistinctly(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		check   func(t *testing.T, err error)
		kind    string
		message string
	}{
		{
			name:   "missing scope",
			status: http.StatusForbidden,
			body:   `{"success":false,"message":"The access token lacks the required permission: profile:read","message_key":"The access token lacks the required permission: {{scope}}","message_params":{"scope":"profile:read"},"code":"ACCESS_TOKEN_SCOPE_DENIED"}`,
			check: func(t *testing.T, err error) {
				var scope *providerScopeError
				require.ErrorAs(t, err, &scope)
				require.Equal(t, "profile:read", scope.scope)
				require.NotErrorIs(t, err, errProviderAuthentication, "a missing permission is not a failed login")
			},
			kind: "scope", message: "profile:read",
		},
		{
			name:   "expired token",
			status: http.StatusUnauthorized,
			body:   `{"success":false,"message":"Unauthorized, the access token has expired","message_key":"Unauthorized, the access token has expired","code":"ACCESS_TOKEN_EXPIRED"}`,
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errProviderTokenExpired)
				require.ErrorIs(t, err, errProviderAuthentication)
			},
			kind: "auth", message: "expired",
		},
		{
			name:   "retired legacy token",
			status: http.StatusUnauthorized,
			body:   `{"success":false,"message":"Legacy access tokens have been retired. Create a new access token in security settings","code":"ACCESS_TOKEN_LEGACY_RETIRED"}`,
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errProviderTokenRetired)
			},
			kind: "auth", message: "nap_",
		},
		{
			name:   "legacy refusal with HTTP 200",
			status: http.StatusOK,
			body:   `{"success":false,"message":"无权进行此操作，access token 无效"}`,
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, errProviderAuthentication)
			},
			kind: "auth", message: "Provider login failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/user/self" {
					writeProviderJSON(w, tc.status, tc.body)
					return
				}
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota_per_unit":500000}}`)
			}))
			defer server.Close()
			_, err := probeProviderAccount(context.Background(), providerRecord{SiteType: "newapi", AuthMode: "token", BaseURL: server.URL},
				newAPITokenSecret(t, testNewAPIPersonalToken), server.Client())
			require.Error(t, err)
			tc.check(t, err)
			require.Equal(t, tc.kind, providerFailureKind(err))
			require.Contains(t, providerPublicError(err), tc.message)
		})
	}
}

func TestNewAPIKeyOperationsReportScopeAndExpiry(t *testing.T) {
	var expired atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expired.Load() {
			writeProviderJSON(w, http.StatusUnauthorized, `{"success":false,"message":"Unauthorized, the access token has expired","code":"ACCESS_TOKEN_EXPIRED"}`)
			return
		}
		switch r.URL.Path {
		case "/api/token/":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"page":1,"page_size":100,"total":1,"items":[{"id":7,"name":"vip key","key":"abcd**********wxyz","status":1,"group":"vip","expired_time":-1,"unlimited_quota":true}]}}`)
		case "/api/token/7/key":
			writeProviderJSON(w, http.StatusForbidden, `{"success":false,"message":"The access token lacks the required permission: api_key:reveal","message_params":{"scope":"api_key:reveal"},"code":"ACCESS_TOKEN_SCOPE_DENIED"}`)
		case "/api/user/self/groups":
			writeProviderJSON(w, http.StatusForbidden, `{"success":false,"message":"The access token lacks the required permission: profile:read","message_params":{"scope":"profile:read"},"code":"ACCESS_TOKEN_SCOPE_DENIED"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", Name: "Relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "token"},
		newAPITokenSecret(t, testNewAPIPersonalToken))

	req := CreateAccountRequest{Platform: service.PlatformOpenAI, Credentials: map[string]any{}, ProviderID: "relay", ProviderKeyID: "7"}
	err := h.applyProviderKeyBinding(context.Background(), &req)
	require.Equal(t, "PROVIDER_SCOPE_DENIED", infraerrors.Reason(err))
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "api_key:reveal", appErr.Metadata["scope"])
	require.Empty(t, req.Credentials["api_key"])

	groups := providerKeyTestRequest(t, h.ListProviderKeyGroups, http.MethodGet, "/providers/:id/groups", "/providers/relay/groups", "")
	require.Equal(t, http.StatusBadGateway, groups.Code)
	require.Equal(t, "PROVIDER_SCOPE_DENIED", providerErrorReason(t, groups))

	expired.Store(true)
	listed := providerKeyTestRequest(t, h.ListProviderKeys, http.MethodGet, "/providers/:id/keys", "/providers/relay/keys", "")
	require.Equal(t, http.StatusBadGateway, listed.Code)
	require.Equal(t, "PROVIDER_TOKEN_EXPIRED", providerErrorReason(t, listed))

	require.Equal(t, "PROVIDER_KEY_CREATE_UNCERTAIN", infraerrors.Reason(providerKeyError(providerCreateFailure(errors.New("connection reset")))))
	require.NotEqual(t, "PROVIDER_KEY_CREATE_UNCERTAIN", infraerrors.Reason(providerKeyError(providerCreateFailure(&providerScopeError{scope: "api_key:write"}))),
		"a refused permission proves that nothing was created")
}

func TestNewAPITokenUserIDRules(t *testing.T) {
	input := func(userID, token string) *providerInput {
		return &providerInput{Name: "Relay", SiteType: "newapi", BaseURL: "https://relay.example.com", AuthMode: "token",
			UserID: userID, SystemAccessToken: token, ExchangeRate: 1}
	}
	record, secret, err := validateProviderInput(input("", testNewAPIPersonalToken), nil)
	require.NoError(t, err, "a personal access token needs no user ID")
	require.Empty(t, record.UserID)
	require.Contains(t, secret, testNewAPIPersonalToken)

	record, _, err = validateProviderInput(input("42", testNewAPIPersonalToken), nil)
	require.NoError(t, err)
	require.Equal(t, "42", record.UserID)

	_, _, err = validateProviderInput(input("", "legacy-system-token"), nil)
	require.ErrorContains(t, err, "user ID")

	previous := providerRecord{ID: "relay", SiteType: "newapi", BaseURL: "https://relay.example.com", AuthMode: "token", SecretEncrypted: "ciphertext"}
	record, secret, err = validateProviderInput(input("", ""), &previous)
	require.NoError(t, err, "a kept personal access token keeps working without a user ID")
	require.Empty(t, secret)
	require.Equal(t, "ciphertext", record.SecretEncrypted)

	record, _, err = validateProviderInput(&providerInput{Name: "Relay", SiteType: "newapi", BaseURL: "https://relay.example.com",
		AuthMode: "password", Account: "ops", Password: "secret", UserID: "42", ExchangeRate: 1}, nil)
	require.NoError(t, err)
	require.Empty(t, record.UserID, "only the token login stores a user ID")
}

func TestProviderRefreshRecordsNewAPIAccountAndImportState(t *testing.T) {
	var mode atomic.Value
	mode.Store("ok")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			switch mode.Load() {
			case "expired":
				writeProviderJSON(w, http.StatusUnauthorized, `{"success":false,"message":"Unauthorized, the access token has expired","code":"ACCESS_TOKEN_EXPIRED"}`)
			case "scope":
				writeProviderJSON(w, http.StatusForbidden, `{"success":false,"message":"The access token lacks the required permission: profile:read","message_params":{"scope":"profile:read"},"code":"ACCESS_TOKEN_SCOPE_DENIED"}`)
			default:
				writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"id":42,"quota":500000}}`)
			}
		case "/api/status":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota_per_unit":500000}}`)
		case "/api/log/self/stat":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota":0}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", Name: "Relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "token"},
		newAPITokenSecret(t, testNewAPIPersonalToken))
	load := func() providerRecord {
		var records []providerRecord
		require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
		require.Len(t, records, 1)
		return records[0]
	}
	state := func() string {
		recorder := providerKeyTestRequest(t, h.ListProviderOptions, http.MethodGet, "/providers/options", "/providers/options", "")
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		options := decodeProviderData[[]providerOptionView](t, recorder)
		require.Len(t, options, 1)
		return options[0].State
	}
	refresh := func() int {
		return providerKeyTestRequest(t, h.RefreshProvider, http.MethodPost, "/providers/:id", "/providers/relay", "").Code
	}
	require.Equal(t, "pending", state())

	require.Equal(t, http.StatusOK, refresh())
	record := load()
	require.Equal(t, "connected", record.Status)
	require.Equal(t, "42", record.UserID, "the account ID reported by the site fills an empty user ID")
	require.Equal(t, "connected", state())

	mode.Store("expired")
	require.Equal(t, http.StatusBadGateway, refresh())
	record = load()
	require.Equal(t, "auth", record.ErrorKind)
	require.Contains(t, record.LastError, "expired")
	require.Equal(t, "42", record.UserID, "a failed refresh keeps the known account ID")
	require.Equal(t, "not_authenticated", state())

	mode.Store("scope")
	require.Equal(t, http.StatusBadGateway, refresh())
	require.Equal(t, "scope", load().ErrorKind)
	require.Equal(t, "sync_failed", state(), "a token that lacks a permission still signs in")

	mode.Store("ok")
	require.Equal(t, http.StatusOK, refresh())
	require.Empty(t, load().ErrorKind)
	require.Equal(t, "connected", state())
}

func TestProviderRefreshKeepsConfiguredNewAPIUserID(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			assert.Equal(t, "7", r.Header.Get("New-Api-User"), "a legacy token sends the configured ID")
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota":500000}}`)
		case "/api/status":
			writeProviderJSON(w, http.StatusOK, `{"success":true,"message":"","data":{"quota_per_unit":500000}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	useProviderTestServer(t, server)
	h := newProviderKeyTestHandler(t, providerRecord{ID: "relay", Name: "Relay", SiteType: "newapi", BaseURL: server.URL, AuthMode: "token", UserID: "7"},
		newAPITokenSecret(t, "legacy-system-token"))

	recorder := providerKeyTestRequest(t, h.RefreshProvider, http.MethodPost, "/providers/:id", "/providers/relay", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.Equal(t, "7", records[0].UserID, "a profile without an ID never clears the configured one")
}
