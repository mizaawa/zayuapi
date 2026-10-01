package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
)

type apiKeyFailoverAttemptContextKey struct{}

type apiKeyFailoverAttemptState struct {
	upstreamCalled   atomic.Bool
	failed           atomic.Bool
	modelUnavailable atomic.Bool
}

var ErrAPIKeyFailoverAttemptExhausted = errors.New("API key failover upstream attempt already failed")

// WithAPIKeyFailoverAttempt isolates the upstream failure budget of one handler
// invocation. Successful multi-step calls may continue until an upstream fails.
func WithAPIKeyFailoverAttempt(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, apiKeyFailoverAttemptContextKey{}, &apiKeyFailoverAttemptState{})
}

func apiKeyFailoverAttemptStateFromContext(ctx context.Context) *apiKeyFailoverAttemptState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(apiKeyFailoverAttemptContextKey{}).(*apiKeyFailoverAttemptState)
	return state
}

func APIKeyFailoverAttemptEnabled(ctx context.Context) bool {
	return apiKeyFailoverAttemptStateFromContext(ctx) != nil
}

func APIKeyFailoverAttemptFailed(ctx context.Context) bool {
	state := apiKeyFailoverAttemptStateFromContext(ctx)
	return state != nil && state.failed.Load()
}

func RecordAPIKeyFailoverUpstreamCall(ctx context.Context) {
	if state := apiKeyFailoverAttemptStateFromContext(ctx); state != nil {
		state.upstreamCalled.Store(true)
	}
}

func APIKeyFailoverUpstreamCalled(ctx context.Context) bool {
	state := apiKeyFailoverAttemptStateFromContext(ctx)
	return state != nil && state.upstreamCalled.Load()
}

func APIKeyFailoverAttemptModelUnavailable(ctx context.Context) bool {
	state := apiKeyFailoverAttemptStateFromContext(ctx)
	return state != nil && state.modelUnavailable.Load()
}

// RecordAPIKeyFailoverUpstreamFailure preserves the original model-unavailable
// decision even when a handler replaces the provider's response with a generic error.
func RecordAPIKeyFailoverUpstreamFailure(ctx context.Context, status int, body []byte) {
	if state := apiKeyFailoverAttemptStateFromContext(ctx); state != nil {
		if APIKeyFailoverModelUnavailable(status, body) {
			state.modelUnavailable.Store(true)
		}
		state.failed.Store(true)
	}
}

func APIKeyFailoverAccountSwitchLimit(ctx context.Context, defaultLimit int) int {
	if APIKeyFailoverAttemptEnabled(ctx) {
		return 0
	}
	return defaultLimit
}

var apiKeyFailoverMissingModelMessagePattern = regexp.MustCompile(`(?i)\b(?:unknown\s+model|no\s+such\s+model|models/\S{1,160}\s+(?:is\s+)?not\s+found|model\s+(?:\S{1,160}\s+)?(?:not\s+found|does\s+not\s+exist|doesn't\s+exist|(?:is|was)\s+not\s+(?:found|supported)))\b`)

// APIKeyFailoverModelUnavailable excludes explicit missing-model failures,
// without treating a missing endpoint or an arbitrary HTTP 404 as missing models.
func APIKeyFailoverModelUnavailable(status int, body []byte) bool {
	if status < http.StatusBadRequest || len(body) == 0 {
		return false
	}
	var payload struct {
		Code     json.RawMessage `json:"code"`
		Type     string          `json:"type"`
		Message  string          `json:"message"`
		Detail   string          `json:"detail"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error struct {
				Code    json.RawMessage `json:"code"`
				Type    string          `json:"type"`
				Message string          `json:"message"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if apiKeyFailoverMissingModelJSONCode(payload.Code) || apiKeyFailoverMissingModelCode(payload.Type) {
			return true
		}
		if apiKeyFailoverMissingModelJSONCode(payload.Response.Error.Code) || apiKeyFailoverMissingModelCode(payload.Response.Error.Type) || apiKeyFailoverMissingModelMessage(payload.Response.Error.Message) {
			return true
		}
		if len(payload.Error) > 0 {
			var errorMessage string
			if json.Unmarshal(payload.Error, &errorMessage) == nil && apiKeyFailoverMissingModelMessage(errorMessage) {
				return true
			}
			var providerError struct {
				Code    json.RawMessage `json:"code"`
				Type    string          `json:"type"`
				Message string          `json:"message"`
			}
			if json.Unmarshal(payload.Error, &providerError) == nil {
				if apiKeyFailoverMissingModelJSONCode(providerError.Code) || apiKeyFailoverMissingModelCode(providerError.Type) {
					return true
				}
				if status == http.StatusNotFound && providerError.Type == "not_found_error" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(providerError.Message)), "model:") {
					return true
				}
				if apiKeyFailoverMissingModelMessage(providerError.Message) {
					return true
				}
			}
		}
		return apiKeyFailoverMissingModelMessage(payload.Message) || apiKeyFailoverMissingModelMessage(payload.Detail)
	}
	return apiKeyFailoverMissingModelMessage(string(body))
}

func apiKeyFailoverMissingModelJSONCode(raw json.RawMessage) bool {
	var code string
	return json.Unmarshal(raw, &code) == nil && apiKeyFailoverMissingModelCode(code)
}

func apiKeyFailoverMissingModelCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "model_not_found", "model_not_exist", "model_not_exists", "model_does_not_exist", "unknown_model":
		return true
	default:
		return false
	}
}

func apiKeyFailoverMissingModelMessage(message string) bool {
	if apiKeyFailoverMissingModelMessagePattern.MatchString(message) {
		return true
	}
	for _, phrase := range []string{
		"\u6ca1\u6709\u6b64\u6a21\u578b",
		"\u6a21\u578b\u4e0d\u5b58\u5728",
		"\u672a\u627e\u5230\u6a21\u578b",
		"\u672a\u627e\u5230\u8be5\u6a21\u578b",
		"\u627e\u4e0d\u5230\u6a21\u578b",
	} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
