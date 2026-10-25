//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGrokOAuthClientRefreshUsesFormFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client-id", r.Form.Get("client_id"))

		switch r.Form.Get("grant_type") {
		case "refresh_token":
			require.Equal(t, "refresh-token", r.Form.Get("refresh_token"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refresh-access",
				"refresh_token": "refresh-rotated",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
		default:
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	t.Setenv(xai.EnvTokenURL, server.URL)

	client := NewGrokOAuthClient()
	refreshed, err := client.RefreshToken(context.Background(), "refresh-token", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "refresh-access", refreshed.AccessToken)
	require.Equal(t, "refresh-rotated", refreshed.RefreshToken)
	require.Equal(t, int64(7200), refreshed.ExpiresIn)
}

func TestGrokOAuthClientRefreshForbiddenClassifiesOnlyExplicitEntitlement(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantReason string
	}{
		{name: "explicit entitlement", body: `{"error":"access_denied"}`, wantReason: "GROK_OAUTH_ENTITLEMENT_DENIED"},
		{name: "generic forbidden", body: `{"error":"forbidden"}`, wantReason: "GROK_OAUTH_TOKEN_REFRESH_FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			t.Setenv(xai.EnvTokenURL, server.URL)

			client := NewGrokOAuthClient()
			_, err := client.RefreshToken(context.Background(), "refresh-token", "", "client-id")
			require.Error(t, err)
			require.Contains(t, strings.ToUpper(err.Error()), tt.wantReason)
		})
	}
}

func TestGrokOAuthClientStatusErrorRedactsSensitiveResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","access_token":"access-secret","refresh_token":"refresh-secret","code_verifier":"verifier-secret"}`))
	}))
	defer server.Close()
	t.Setenv(xai.EnvTokenURL, server.URL)

	client := NewGrokOAuthClient()
	_, err := client.RefreshToken(context.Background(), "refresh-secret", "", "client-id")
	require.Error(t, err)

	errText := err.Error()
	require.Contains(t, errText, "status 400")
	require.Contains(t, errText, `\"refresh_token\":\"***\"`)
	require.NotContains(t, errText, "access-secret")
	require.NotContains(t, errText, "refresh-secret")
	require.NotContains(t, errText, "verifier-secret")
}

func TestGrokOAuthEntitlementDenialRequiresExplicitEvidence(t *testing.T) {
	t.Parallel()

	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"error":"access_denied"}`))
	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"code":"entitlement_denied"}`))
	require.True(t, grokOAuthHasExplicitEntitlementDenial(`{"message":"no active Grok subscription"}`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`{"error":"forbidden","message":"request forbidden"}`))
	require.False(t, grokOAuthHasExplicitEntitlementDenial(`<html>403 Forbidden</html>`))
}
