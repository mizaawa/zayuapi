package admin

import (
	"encoding/json"
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
