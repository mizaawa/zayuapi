package admin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	require.ErrorContains(t, err, "user ID")
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
