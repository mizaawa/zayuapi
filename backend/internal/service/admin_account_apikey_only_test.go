//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAccountCreationOnlyAPIKey(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformCustom} {
		for _, typ := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeUpstream, AccountTypeBedrock, AccountTypeServiceAccount, "", "unknown"} {
			t.Run(platform+"/"+typ, func(t *testing.T) {
				account, err := buildAccountForCreate(&CreateAccountInput{Name: "test", Platform: platform, Type: typ, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.com"}}, map[string]any{})
				require.Error(t, err, "non-API-key account creation must be rejected")
				require.Nil(t, account)
			})
		}
		t.Run(platform+"/apikey", func(t *testing.T) {
			account, err := buildAccountForCreate(&CreateAccountInput{Name: "test", Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.com"}}, map[string]any{})
			require.NoError(t, err)
			require.Equal(t, AccountTypeAPIKey, account.Type)
		})
	}
}
func TestAccountDuplicationOnlyAPIKey(t *testing.T) {
	for _, typ := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeUpstream, AccountTypeBedrock, AccountTypeServiceAccount, "", "unknown"} {
		require.False(t, canDuplicateAccountType(typ), "cannot duplicate type %s", typ)
	}
	require.True(t, canDuplicateAccountType(AccountTypeAPIKey))
}
