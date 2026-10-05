//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeySystemPromptValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled bool
		prompt  string
		want    error
	}{
		{"disabled empty draft", false, "", nil},
		{"enabled empty", true, "", ErrAPIKeySystemPromptEmpty},
		{"enabled whitespace", true, " \t\n\u3000", ErrAPIKeySystemPromptEmpty},
		{"ascii byte limit", true, strings.Repeat("x", MaxAPIKeySystemPromptBytes), nil},
		{"unicode byte limit", true, strings.Repeat("\u4e2d", MaxAPIKeySystemPromptBytes/3) + "xx", nil},
		{"unicode over byte limit", true, strings.Repeat("\u4e2d", MaxAPIKeySystemPromptBytes/3+1), ErrAPIKeySystemPromptTooLong},
		{"disabled over byte limit", false, strings.Repeat("x", MaxAPIKeySystemPromptBytes+1), ErrAPIKeySystemPromptTooLong},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, validateAPIKeySystemPrompt(tt.enabled, tt.prompt), tt.want)
		})
	}
}

func TestAPIKeyUpdateSystemPromptUsesMergedState(t *testing.T) {
	enabled, disabled := true, false
	empty := " \t"
	prompt := "  Project instructions\n"
	for _, tt := range []struct {
		name string
		key  APIKey
		req  UpdateAPIKeyRequest
		want error
	}{
		{"enable empty draft", APIKey{}, UpdateAPIKeyRequest{CustomSystemPromptEnabled: &enabled}, ErrAPIKeySystemPromptEmpty},
		{"enable saved draft", APIKey{CustomSystemPrompt: prompt}, UpdateAPIKeyRequest{CustomSystemPromptEnabled: &enabled}, nil},
		{"clear enabled prompt", APIKey{CustomSystemPromptEnabled: true, CustomSystemPrompt: prompt}, UpdateAPIKeyRequest{CustomSystemPrompt: &empty}, ErrAPIKeySystemPromptEmpty},
		{"disable and clear draft", APIKey{CustomSystemPromptEnabled: true, CustomSystemPrompt: prompt}, UpdateAPIKeyRequest{CustomSystemPromptEnabled: &disabled, CustomSystemPrompt: &empty}, nil},
		{"managed unchanged rejected", APIKey{Purpose: APIKeyPurposeChannelMonitor, CustomSystemPrompt: prompt}, UpdateAPIKeyRequest{CustomSystemPrompt: &prompt}, ErrManagedAPIKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tt.key.ID, tt.key.UserID, tt.key.Status = 1, 7, StatusActive
			svc, repo := newUpdateFieldsAPIKeyService(&tt.key)
			_, err := svc.Update(context.Background(), 1, 7, tt.req)
			require.ErrorIs(t, err, tt.want)
			if tt.want != nil {
				require.Empty(t, repo.updateFields)
			}
		})
	}
}

func TestAPIKeyUpdateDisablingSystemPromptRetainsDraftAndForce(t *testing.T) {
	disabled := false
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Status: StatusActive,
		CustomSystemPromptEnabled: true, CustomSystemPromptForce: true, CustomSystemPrompt: "  Draft instructions\n",
		QuotaUsed: 30, Usage5h: 12, Usage1d: 18, Usage7d: 24,
		User: &User{Balance: 50},
	})
	got, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{CustomSystemPromptEnabled: &disabled})
	require.NoError(t, err)
	require.False(t, got.CustomSystemPromptEnabled)
	require.True(t, got.CustomSystemPromptForce)
	require.Equal(t, "  Draft instructions\n", got.CustomSystemPrompt)
	require.Equal(t, []APIKeyUpdateFields{{CustomSystemPromptEnabled: true}}, repo.updateFields)
	require.Equal(t, float64(30), got.QuotaUsed)
	require.Equal(t, float64(12), got.Usage5h)
	require.Equal(t, float64(18), got.Usage1d)
	require.Equal(t, float64(24), got.Usage7d)
	require.Equal(t, float64(50), got.User.Balance)
}

func TestAPIKeyUpdateSystemPromptInvalidatesAuthCache(t *testing.T) {
	prompt := "Updated project instructions"
	svc, _ := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-prompt-invalidation", Status: StatusActive,
	})
	cache := &authCacheStub{}
	svc.cache = cache
	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{CustomSystemPrompt: &prompt})
	require.NoError(t, err)
	require.Equal(t, []string{svc.authCacheKey("sk-prompt-invalidation")}, cache.deleteAuthKeys)
}
