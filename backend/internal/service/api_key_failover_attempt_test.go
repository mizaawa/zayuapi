package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverAttemptsAreOptInAndIsolated(t *testing.T) {
	ctx := context.Background()
	require.False(t, APIKeyFailoverAttemptEnabled(ctx))
	require.False(t, APIKeyFailoverAttemptFailed(ctx))
	require.Equal(t, 5, APIKeyFailoverAccountSwitchLimit(ctx, 5))
	RecordAPIKeyFailoverUpstreamFailure(ctx, http.StatusServiceUnavailable, nil)
	require.False(t, APIKeyFailoverAttemptFailed(ctx))

	first := WithAPIKeyFailoverAttempt(ctx)
	second := WithAPIKeyFailoverAttempt(ctx)
	require.True(t, APIKeyFailoverAttemptEnabled(first))
	require.Equal(t, 0, APIKeyFailoverAccountSwitchLimit(first, 5))
	RecordAPIKeyFailoverUpstreamFailure(first, http.StatusNotFound, []byte(`{"error":{"code":"model_not_found"}}`))
	require.True(t, APIKeyFailoverAttemptFailed(first))
	require.True(t, APIKeyFailoverAttemptModelUnavailable(first))
	require.False(t, APIKeyFailoverAttemptFailed(second))
	require.False(t, APIKeyFailoverAttemptModelUnavailable(second))
	RecordAPIKeyFailoverUpstreamFailure(first, 0, nil)
	require.True(t, APIKeyFailoverAttemptModelUnavailable(first), "later guard failures preserve the provider's original missing-model error")
}

func TestAPIKeyFailoverModelUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"openai structured code", 404, `{"error":{"code":"model_not_found","message":"not available"}}`, true},
		{"other upstream model code", 400, `{"error":{"code":"model_not_exist"}}`, true},
		{"unknown model code", 422, `{"code":"unknown_model"}`, true},
		{"openai model message", 404, `{"error":{"message":"The model 'gpt-missing' does not exist or you do not have access to it."}}`, true},
		{"anthropic model message", 404, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-missing"}}`, true},
		{"gemini missing model", 404, `{"error":{"code":404,"status":"NOT_FOUND","message":"models/gemini-missing is not found for API version v1beta, or is not supported for generateContent"}}`, true},
		{"numeric code preserves model message", 400, `{"code":400,"message":"Unknown model gemini-missing"}`, true},
		{"numeric response code preserves model message", 400, `{"response":{"error":{"code":404,"message":"Unknown model gemini-missing"}}}`, true},
		{"gemini missing endpoint", 404, `{"error":{"code":404,"status":"NOT_FOUND","message":"The requested endpoint was not found"}}`, false},
		{"gemini model rate limit", 429, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded for models/gemini-test"}}`, false},
		{"plain model error", 400, "Unknown model gpt-missing", true},
		{"string provider error", 404, `{"error":"model not found"}`, true},
		{"localized model error", 400, `{"error":{"message":"\u6a21\u578b\u4e0d\u5b58\u5728"}}`, true},
		{"stream semantic model error", 400, `{"type":"response.failed","response":{"error":{"code":"model_not_found"}}}`, true},
		{"codex unsupported model", 400, `{"detail":"The 'gpt-missing' model is not supported when using Codex with a ChatGPT account."}`, true},
		{"missing endpoint", 404, `{"error":{"message":"The requested endpoint was not found"}}`, false},
		{"unrelated model mention", 404, `{"error":{"message":"Request for model gpt-5 failed: path not found"}}`, false},
		{"capacity temporarily unavailable", 503, `{"error":{"message":"The model is temporarily unavailable due to capacity"}}`, false},
		{"rate limit", 429, `{"error":{"code":"rate_limit_exceeded"}}`, false},
		{"unauthorized", 401, `{"error":{"message":"Invalid API key"}}`, false},
		{"bad parameter", 400, `{"error":{"message":"Invalid model parameter type"}}`, false},
		{"bare 404", 404, "", false},
		{"success payload", 200, `{"code":"model_not_found"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, APIKeyFailoverModelUnavailable(tt.status, []byte(tt.body)))
		})
	}
}
