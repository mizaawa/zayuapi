package openai_ws_v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// WS passthrough must account image tokens the way the HTTP Responses path
// does: image input from input/prompt_tokens_details, and the hosted
// tool_usage.image_gen breakdown filling counters the usage object omits.
func TestRelay_ImageUsageMatchesHTTPResponsesAccounting(t *testing.T) {
	t.Parallel()

	messages := []string{
		`{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":60},"tool_usage":{"image_gen":{"input_tokens_details":{"image_tokens":80},"output_tokens_details":{"image_tokens":50}}}}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":30,"output_tokens":15,"input_tokens_details":{"image_tokens":20},"output_tokens_details":{"image_tokens":10}},"tool_usage":{"image_gen":{"input_tokens_details":{"image_tokens":800},"output_tokens_details":{"image_tokens":500}}}}}`,
		`{"type":"response.completed","response":{"usage":{"prompt_tokens":12,"completion_tokens":2,"prompt_tokens_details":{"image_tokens":7}}}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4}}}`,
	}
	state := &relayState{}
	var turns []RelayTurnResult
	for _, message := range messages {
		parseUsageAndAccumulate(state, []byte(message), "response.completed", nil)
		turns = append(turns, RelayTurnResult{Usage: finalizeRelayTurnUsage(state)})
	}
	result := RelayResult{Usage: state.usage}

	require.Equal(t, 80, turns[0].Usage.ImageInputTokens, "tool_usage.image_gen fills a missing image input counter")
	require.Equal(t, 50, turns[0].Usage.ImageOutputTokens, "tool_usage.image_gen fills a missing image output counter")
	require.Equal(t, 20, turns[1].Usage.ImageInputTokens, "response usage takes precedence over the hosted tool breakdown")
	require.Equal(t, 10, turns[1].Usage.ImageOutputTokens, "response usage takes precedence over the hosted tool breakdown")
	require.Equal(t, 7, turns[2].Usage.ImageInputTokens)
	require.Zero(t, turns[3].Usage.ImageInputTokens, "a text turn must not reuse earlier image usage")
	require.Zero(t, turns[3].Usage.ImageOutputTokens, "a text turn must not reuse earlier image usage")

	require.Equal(t, 107, result.Usage.ImageInputTokens)
	require.Equal(t, 60, result.Usage.ImageOutputTokens)
	require.Equal(t, 145, result.Usage.InputTokens)
	require.Equal(t, 81, result.Usage.OutputTokens)
}

func TestRelay_ImageUsageRejectsMalformedHostedTokens(t *testing.T) {
	for _, value := range []string{"-1", "1.5", "1e100", `"7"`} {
		state := &relayState{}
		message := []byte(`{"response":{"usage":{"input_tokens":100,"output_tokens":60},"tool_usage":{"image_gen":{"input_tokens_details":{"image_tokens":` + value + `}}}}}`)
		usage := parseUsageAndAccumulate(state, message, "response.completed", nil)
		require.Zero(t, usage.InputTokens)
		require.Zero(t, finalizeRelayTurnUsage(state).ImageInputTokens)
		require.Zero(t, state.usage.InputTokens)
	}
}
