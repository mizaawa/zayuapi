package xai

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// DecodeJWTClaims reads metadata from a token returned by the upstream refresh
// endpoint. These unverified claims are never used as an authorization decision.
func DecodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func JWTClaimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}
