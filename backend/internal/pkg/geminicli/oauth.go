package geminicli

import (
	"net/http"
	"os"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       string
}

// EffectiveOAuthConfig returns the effective OAuth configuration.
// oauthType: "code_assist" or "ai_studio" (defaults to "code_assist" if empty).
//
// If ClientID/ClientSecret is not provided, this falls back to the built-in Gemini CLI OAuth client.
//
// Note: The built-in Gemini CLI OAuth client is restricted and may reject some scopes (e.g.
// https://www.googleapis.com/auth/generative-language), which will surface as
// "restricted_client" / "Unregistered scope(s)" errors during browser authorization.
func EffectiveOAuthConfig(cfg OAuthConfig, oauthType string) (OAuthConfig, error) {
	effective := OAuthConfig{
		ClientID:     strings.TrimSpace(cfg.ClientID),
		ClientSecret: strings.TrimSpace(cfg.ClientSecret),
		Scopes:       strings.TrimSpace(cfg.Scopes),
	}

	// Normalize scopes: allow comma-separated input but send space-delimited scopes to Google.
	if effective.Scopes != "" {
		effective.Scopes = strings.Join(strings.Fields(strings.ReplaceAll(effective.Scopes, ",", " ")), " ")
	}

	// Fall back to built-in Gemini CLI OAuth client when not configured.
	// SECURITY: This repo does not embed the built-in client secret; it must be provided via env.
	if effective.ClientID == "" && effective.ClientSecret == "" {
		secret := strings.TrimSpace(GeminiCLIOAuthClientSecret)
		if secret == "" {
			if v, ok := os.LookupEnv(GeminiCLIOAuthClientSecretEnv); ok {
				secret = strings.TrimSpace(v)
			}
		}
		if secret == "" {
			return OAuthConfig{}, infraerrors.Newf(http.StatusBadRequest, "GEMINI_CLI_OAUTH_CLIENT_SECRET_MISSING", "built-in Gemini CLI OAuth client_secret is not configured; set %s or provide a custom OAuth client", GeminiCLIOAuthClientSecretEnv)
		}
		effective.ClientID = GeminiCLIOAuthClientID
		effective.ClientSecret = secret
	} else if effective.ClientID == "" || effective.ClientSecret == "" {
		return OAuthConfig{}, infraerrors.New(http.StatusBadRequest, "GEMINI_OAUTH_CLIENT_NOT_CONFIGURED", "OAuth client not configured: please set both client_id and client_secret (or leave both empty to use the built-in Gemini CLI client)")
	}

	isBuiltinClient := effective.ClientID == GeminiCLIOAuthClientID

	if effective.Scopes == "" {
		// Use different default scopes based on OAuth type
		switch oauthType {
		case "ai_studio":
			// Built-in client can't request some AI Studio scopes (notably generative-language).
			if isBuiltinClient {
				effective.Scopes = DefaultCodeAssistScopes
			} else {
				effective.Scopes = DefaultAIStudioScopes
			}
		case "google_one":
			// Google One always uses built-in Gemini CLI client (same as code_assist)
			// Built-in client can't request restricted scopes like generative-language.retriever or drive.readonly
			effective.Scopes = DefaultCodeAssistScopes
		default:
			// Default to Code Assist scopes
			effective.Scopes = DefaultCodeAssistScopes
		}
	} else if (oauthType == "ai_studio" || oauthType == "google_one") && isBuiltinClient {
		// If user overrides scopes while still using the built-in client, strip restricted scopes.
		parts := strings.Fields(effective.Scopes)
		filtered := make([]string, 0, len(parts))
		for _, s := range parts {
			if hasRestrictedScope(s) {
				continue
			}
			filtered = append(filtered, s)
		}
		if len(filtered) == 0 {
			effective.Scopes = DefaultCodeAssistScopes
		} else {
			effective.Scopes = strings.Join(filtered, " ")
		}
	}

	// Backward compatibility: normalize older AI Studio scope to the currently documented one.
	if oauthType == "ai_studio" && effective.Scopes != "" {
		parts := strings.Fields(effective.Scopes)
		for i := range parts {
			if parts[i] == "https://www.googleapis.com/auth/generative-language" {
				parts[i] = "https://www.googleapis.com/auth/generative-language.retriever"
			}
		}
		effective.Scopes = strings.Join(parts, " ")
	}

	return effective, nil
}

func hasRestrictedScope(scope string) bool {
	return strings.HasPrefix(scope, "https://www.googleapis.com/auth/generative-language") ||
		strings.HasPrefix(scope, "https://www.googleapis.com/auth/drive")
}
