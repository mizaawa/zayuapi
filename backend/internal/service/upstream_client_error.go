package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

var (
	upstreamErrorBearerPattern = regexp.MustCompile(`(?i)\b(Bearer|Basic|AgentAssertion)\s+[A-Za-z0-9_./+=~-]+`)
	upstreamErrorKeyPattern    = regexp.MustCompile(`\b(?:sk-|xai-)[A-Za-z0-9_-]{12,}`)
	upstreamErrorURLPattern    = regexp.MustCompile(`(?i)\b(?:https?|socks5h?|wss?)://[^\s<>"'\\]+`)
)

// redactUpstreamErrorCredentials also handles credentials embedded in free text,
// not just JSON fields. Upstreams sometimes echo authentication headers on 400.
func redactUpstreamErrorCredentials(message string) string {
	message = upstreamErrorBearerPattern.ReplaceAllString(message, "[REDACTED]")
	message = upstreamErrorKeyPattern.ReplaceAllString(message, "[REDACTED]")
	message = sanitizeErrorMessage(message)
	return logredact.RedactText(message, "api_key", "api-key", "x-api-key", "x-goog-api-key", "authorization", "cookie", "token", "secret")
}

// sanitizeUpstreamClientMessage retains actionable validation errors without
// publishing the provider's address or credential values. Email addresses remain visible.
func sanitizeUpstreamClientMessage(message string, account *Account) string {
	if account != nil {
		secrets := make([]string, 0, len(SensitiveCredentialKeys))
		for _, key := range SensitiveCredentialKeys {
			if value := account.GetCredential(key); strings.TrimSpace(value) != "" {
				secrets = append(secrets, value)
			}
		}
		for _, value := range account.GetHeaderOverrides() {
			secrets = append(secrets, value)
		}
		message = sanitizeErrorMessageWithSecrets(message, secrets...)
	}
	message = redactUpstreamErrorCredentials(message)
	message = upstreamErrorURLPattern.ReplaceAllString(message, "[upstream URL redacted]")
	return message
}

func isPrivateUpstreamErrorField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if isSensitiveKey(key) || IsSensitiveCredentialKey(key) {
		return true
	}
	switch key {
	case "account", "account_id", "account_name", "upstream_endpoint", "upstream_url", "base_url", "proxy_url":
		return true
	}
	return false
}

// sanitizeUpstreamClientErrorBody is an egress boundary, including for historical
// ops records which may have been stored before credential redaction was added.
func sanitizeUpstreamClientErrorBody(body []byte, account *Account) []byte {
	if len(body) == 0 {
		return body
	}
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		return []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
	}
	changed := false
	var redact func(any, int) any
	redact = func(value any, depth int) any {
		if depth > 32 {
			changed = true
			return "[REDACTED]"
		}
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if isPrivateUpstreamErrorField(key) {
					if text, ok := item.(string); !ok || text != "[REDACTED]" {
						changed = true
					}
					v[key] = "[REDACTED]"
				} else {
					v[key] = redact(item, depth+1)
				}
			}
		case []any:
			for i, item := range v {
				v[i] = redact(item, depth+1)
			}
		case string:
			text := sanitizeUpstreamClientMessage(v, account)
			changed = changed || text != v
			return text
		}
		return value
	}
	payload = redact(payload, 0)
	if !changed {
		return body
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
	}
	return encoded
}
