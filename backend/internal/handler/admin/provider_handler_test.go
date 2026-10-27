package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type providerTestSettingsRepo struct {
	service.SettingRepository
	value string
}

func (r *providerTestSettingsRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}

func (r *providerTestSettingsRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}

type providerTestEncryptor struct{}

func (providerTestEncryptor) Encrypt(value string) (string, error) {
	return "encrypted:" + base64.StdEncoding.EncodeToString([]byte(value)), nil
}

func (providerTestEncryptor) Decrypt(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "encrypted:"))
	return string(decoded), err
}

func providerTestRequest(t *testing.T, handler gin.HandlerFunc, method, path string, input providerInput, canceled bool) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if canceled {
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		request = request.WithContext(ctx)
	}
	recorder := httptest.NewRecorder()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	route := path
	if strings.HasPrefix(path, "/providers/") {
		route = "/providers/:id"
	}
	router.Handle(method, route, handler)
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestCreateProviderSavesDisconnectedWhenRemoteProbeFails(t *testing.T) {
	repo := &providerTestSettingsRepo{}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, nil), providerTestEncryptor{})
	input := providerInput{
		Name: "Remote", SiteType: "sub2api", BaseURL: "https://example.com", AuthMode: "password",
		Account: "person@example.com", Password: "private-password", ExchangeRate: 1,
	}
	// A canceled transport request deterministically exercises a failed remote
	// probe without depending on a public provider or a network timeout.
	response := providerTestRequest(t, h.CreateProvider, http.MethodPost, "/providers", input, true)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var payload struct {
		Data providerView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Equal(t, "disconnected", payload.Data.Status)
	require.NotEmpty(t, payload.Data.LastError)
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.Len(t, records, 1)
	require.NotEmpty(t, records[0].ID)
	require.Equal(t, "disconnected", records[0].Status)
	require.NotEmpty(t, records[0].LastError)
	require.NotContains(t, records[0].LastError, input.Password)
	require.NotContains(t, response.Body.String(), input.Password)
	require.NotContains(t, repo.value, input.Password)
	secret, err := h.providerEncryptor.Decrypt(records[0].SecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, input.Password, secret)
}

func TestProbeProviderStillReportsRemoteFailureWithoutSaving(t *testing.T) {
	repo := &providerTestSettingsRepo{}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, nil), providerTestEncryptor{})
	input := providerInput{
		Name: "Remote", SiteType: "sub2api", BaseURL: "https://example.com", AuthMode: "password",
		Account: "person@example.com", Password: "private-password", ExchangeRate: 1,
	}
	response := providerTestRequest(t, h.ProbeProvider, http.MethodPost, "/providers/probe", input, true)
	require.Equal(t, http.StatusBadGateway, response.Code, response.Body.String())
	require.Empty(t, repo.value)
	require.NotContains(t, response.Body.String(), input.Password)
}

func TestUpdateProviderSavesDisconnectedAndRetainsCredentialOnFailedProbe(t *testing.T) {
	repo := &providerTestSettingsRepo{}
	h := &AccountHandler{}
	h.SetProviderDependencies(service.NewSettingService(repo, nil), providerTestEncryptor{})
	input := providerInput{
		Name: "Remote", SiteType: "sub2api", BaseURL: "https://example.com", AuthMode: "password",
		Account: "person@example.com", Password: "private-password", ExchangeRate: 1,
	}
	created := providerTestRequest(t, h.CreateProvider, http.MethodPost, "/providers", input, true)
	require.Equal(t, http.StatusCreated, created.Code)
	var records []providerRecord
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	input.Name = "Renamed Remote"
	input.Password = ""
	updated := providerTestRequest(t, h.UpdateProvider, http.MethodPut, "/providers/"+records[0].ID, input, true)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.NoError(t, h.providerSettings.LoadProviderRecords(context.Background(), &records))
	require.Len(t, records, 1)
	require.Equal(t, "Renamed Remote", records[0].Name)
	require.Equal(t, "disconnected", records[0].Status)
	secret, err := h.providerEncryptor.Decrypt(records[0].SecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, "private-password", secret)
}

func TestProviderPublicViewNeverExposesStoredCredential(t *testing.T) {
	record := providerRecord{ID: "sample", Name: "Vendor", SiteType: "sub2api", AuthMode: "password", SecretEncrypted: "ciphertext", Status: "connected"}
	data, err := json.Marshal(record.view())
	require.NoError(t, err)
	require.NotContains(t, string(data), "ciphertext")
	require.NotContains(t, string(data), "secret_encrypted")
}

func TestProviderTokenInputUsesDashboardCredentialsNotModelAPIKeys(t *testing.T) {
	sub, secret, err := validateProviderInput(&providerInput{
		Name: "Sub", SiteType: "sub2api", BaseURL: "https://www.zayuapi.com", AuthMode: "token",
		AccessToken: "dashboard-access", RefreshToken: "dashboard-refresh", TokenType: "Bearer", ExchangeRate: 1,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "token", sub.AuthMode)
	require.Contains(t, secret, "dashboard-access")
	require.Contains(t, secret, "dashboard-refresh")
	newAPI, secret, err := validateProviderInput(&providerInput{
		Name: "New", SiteType: "newapi", BaseURL: "https://provider.example.com", AuthMode: "token",
		UserID: "123", SystemAccessToken: "system-personal-token", ExchangeRate: 1,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "123", newAPI.UserID)
	require.Contains(t, secret, "system-personal-token")
	_, _, err = validateProviderInput(&providerInput{
		Name: "New", SiteType: "newapi", BaseURL: "https://provider.example.com", AuthMode: "token",
		UserID: "123", SystemAccessToken: "sk-model-key", ExchangeRate: 1,
	}, nil)
	require.ErrorContains(t, err, "not an sk- model API key")
	_, _, err = validateProviderInput(&providerInput{
		Name: "New", SiteType: "newapi", BaseURL: "https://provider.example.com", AuthMode: "token",
		UserID: "not-a-number", SystemAccessToken: "system-personal-token", ExchangeRate: 1,
	}, nil)
	require.NoError(t, err)
	for _, userID := range []string{"", "line\nbreak", strings.Repeat("a", 129)} {
		_, _, err = validateProviderInput(&providerInput{
			Name: "New", SiteType: "newapi", BaseURL: "https://provider.example.com", AuthMode: "token",
			UserID: userID, SystemAccessToken: "system-personal-token", ExchangeRate: 1,
		}, nil)
		require.ErrorContains(t, err, "user ID")
	}
	_, secret, err = validateProviderInput(&providerInput{
		Name: "Sub", SiteType: "sub2api", BaseURL: "https://provider.example.com", AuthMode: "token",
		RefreshToken: "refresh-only", ExchangeRate: 1,
	}, nil)
	require.NoError(t, err)
	require.Contains(t, secret, "refresh-only")
	require.NotContains(t, secret, "dashboard-access")
}

func TestProviderTokenEditKeepsCredentialOnlyForSameUserAndOrigin(t *testing.T) {
	previous := providerRecord{ID: "sample", SiteType: "newapi", BaseURL: "https://provider.example.com", AuthMode: "token", UserID: "123", SecretEncrypted: "ciphertext"}
	input := providerInput{Name: "New", SiteType: "newapi", BaseURL: previous.BaseURL, AuthMode: "token", UserID: "123", ExchangeRate: 1}
	record, secret, err := validateProviderInput(&input, &previous)
	require.NoError(t, err)
	require.Empty(t, secret)
	require.Equal(t, "ciphertext", record.SecretEncrypted)
	input.UserID = "124"
	_, _, err = validateProviderInput(&input, &previous)
	require.Error(t, err)
	input.UserID = "123"
	input.BaseURL = "https://other.example.com"
	_, _, err = validateProviderInput(&input, &previous)
	require.Error(t, err)
	require.NotContains(t, providerPublicError(err), "ciphertext")
	require.False(t, strings.Contains(providerPublicError(err), "system-personal-token"))
}

func TestProviderLegacyTokenModeIsPresentedAsToken(t *testing.T) {
	previous := providerRecord{ID: "sample", SiteType: "sub2api", BaseURL: "https://provider.example.com", AuthMode: "api_key", SecretEncrypted: "ciphertext"}
	require.Equal(t, "token", previous.view().AuthMode)
	input := providerInput{Name: "Sub", SiteType: "sub2api", BaseURL: previous.BaseURL, AuthMode: "token", ExchangeRate: 1}
	updated, _, err := validateProviderInput(&input, &previous)
	require.NoError(t, err)
	require.Equal(t, "ciphertext", updated.SecretEncrypted)
}

func TestProviderInputKeepsExistingCredentialOnlyForSameIdentity(t *testing.T) {
	previous := providerRecord{ID: "sample", SiteType: "sub2api", BaseURL: "https://www.zayuapi.com", AuthMode: "password", Account: "same@example.com", SecretEncrypted: "ciphertext"}
	input := providerInput{Name: "Vendor", SiteType: "sub2api", BaseURL: "https://www.zayuapi.com", AuthMode: "password", Account: "same@example.com"}
	record, secret, err := validateProviderInput(&input, &previous)
	require.NoError(t, err)
	require.Empty(t, secret)
	require.Equal(t, "ciphertext", record.SecretEncrypted)
	input.Account = "other@example.com"
	_, _, err = validateProviderInput(&input, &previous)
	require.Error(t, err)
	input.Account = "same@example.com"
	input.BaseURL = "https://other-provider.example.com"
	_, _, err = validateProviderInput(&input, &previous)
	require.Error(t, err, "an edited origin must not receive the saved login credential")
}
