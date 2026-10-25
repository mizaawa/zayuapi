package service

import (
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ValidateNewAccountType is shared by every administrative creation/import path.
// Runtime support for existing legacy credentials does not permit new accounts.
func ValidateNewAccountType(accountType string) error {
	if accountType != AccountTypeAPIKey {
		return infraerrors.BadRequest("ACCOUNT_TYPE_APIKEY_REQUIRED", "new accounts only support API key credentials")
	}
	return nil
}

// ValidateNewAccountCredentials validates API-key onboarding for the six supported platforms.
func ValidateNewAccountCredentials(platform, accountType string, credentials map[string]any) error {
	if err := ValidateNewAccountType(accountType); err != nil {
		return err
	}
	switch platform {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformCustom:
	default:
		return infraerrors.BadRequest("ACCOUNT_PLATFORM_INVALID", "unsupported account platform")
	}
	key, _ := credentials["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		return infraerrors.BadRequest("ACCOUNT_API_KEY_REQUIRED", "API key is required")
	}
	base, _ := credentials["base_url"].(string)
	base = strings.TrimSpace(base)
	if base == "" {
		if platform == PlatformAntigravity || platform == PlatformCustom {
			return infraerrors.BadRequest("ACCOUNT_BASE_URL_REQUIRED", "this platform requires a base URL")
		}
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return infraerrors.BadRequest("ACCOUNT_BASE_URL_INVALID", "base URL must be an HTTP or HTTPS URL without embedded credentials or fragments")
	}
	return nil
}
