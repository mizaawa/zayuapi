package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAnthropicPayloadSignalsCyber(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    bool
	}{
		{"non-streaming cyber refusal", `{"type":"message","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined because it could enable cyber harm."}}`, true},
		{"streaming message_delta", `{"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null,"stop_details":{"type":"refusal","category":"cyber"}},"usage":{"output_tokens":0}}`, true},
		{"category with stray spaces", `{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":" cyber "}}}`, true},
		{"streaming fallback block", `{"type":"content_block_start","index":0,"content_block":{"type":"fallback","from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"cyber"}}}`, true},
		{"non-streaming fallback block", `{"type":"message","content":[{"type":"fallback","from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"cyber"}},{"type":"text","text":"Hi"}],"stop_reason":"end_turn","stop_details":null}`, true},
		{"relayed cyber_policy error", `{"type":"error","error":{"type":"invalid_request_error","code":"cyber_policy","message":"flagged"}}`, true},
		{"bio refusal", `{"type":"message","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"bio"}}`, false},
		{"uncategorized refusal", `{"type":"message","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":null,"explanation":null}}`, false},
		{"fallback triggered by another category", `{"type":"content_block_start","index":0,"content_block":{"type":"fallback","trigger":{"type":"refusal","category":"frontier_llm"}}}`, false},
		{"model text about cyber", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"category cyber: cyber_policy and stop_details explained"}}`, false},
		{"text block named fallback", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"cyber","trigger":{"category":"cyber"}}}`, false},
		{"invalid json", `{"stop_details":{"category":"cyber"`, false},
		{"empty", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, anthropicPayloadSignalsCyber([]byte(tc.payload)))
		})
	}
}

func TestUpstreamCyberSignalObservedFollowsTheCurrentAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	require.False(t, UpstreamCyberSignalObserved(nil))
	require.False(t, UpstreamCyberSignalObserved(c), "no observation started yet")
	observeAnthropicCyberSignal(c, `{"type":"message_delta","delta":{"stop_details":{"category":"cyber"}}}`)
	require.False(t, UpstreamCyberSignalObserved(c), "without an observer there is nothing to record on")

	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"model":"claude-opus-5-5"}}`))
	require.False(t, UpstreamCyberSignalObserved(c))
	observer.ObserveAnthropic([]byte(`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"}}}`))
	require.True(t, UpstreamCyberSignalObserved(c))
	require.Equal(t, "claude-opus-5-5", observer.Model(), "model observation keeps working")
	observer.ObserveAnthropic([]byte(`{"type":"message_stop"}`))
	require.True(t, UpstreamCyberSignalObserved(c), "a later event does not clear the hit")

	beginUpstreamResponseModelObservation(c)
	require.False(t, UpstreamCyberSignalObserved(c), "the next attempt starts clean")
	observeAnthropicCyberSignal(c, `{"type":"content_block_start","content_block":{"type":"fallback","trigger":{"category":"cyber"}}}`)
	require.True(t, UpstreamCyberSignalObserved(c))
}

func TestOpenAIAndGeminiObservationsNeverRaiseTheClaudeCyberSignal(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"type":"response.failed","response":{"error":{"code":"cyber_policy"}}}`), "response.failed")
	observer.ObserveGemini([]byte(`{"candidates":[{"finishReason":"SAFETY"}],"error":{"code":"cyber_policy"}}`))
	require.False(t, observer.CyberSignal(), "OpenAI cyber_policy is tracked by MarkOpsCyberPolicy; Gemini has no cyber signal")
}
