package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReasoningEffortPreservedAcrossProtocols(t *testing.T) {
	for _, effort := range []string{"max", "xhigh", "future-effort", "UnannouncedLevel"} {
		t.Run(effort, func(t *testing.T) {
			anthropicReq := &AnthropicRequest{
				Model:        "future-model",
				Messages:     []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
				OutputConfig: &AnthropicOutputConfig{Effort: effort},
			}
			responsesReq, err := AnthropicToResponses(anthropicReq)
			require.NoError(t, err)
			require.Equal(t, effort, responsesReq.Reasoning.Effort)

			chatReq, err := AnthropicToChatCompletionsRequest(anthropicReq)
			require.NoError(t, err)
			require.Equal(t, effort, chatReq.ReasoningEffort)

			fromResponses, err := ResponsesToAnthropicRequest(responsesReq)
			require.NoError(t, err)
			require.Equal(t, effort, fromResponses.OutputConfig.Effort)
			require.Equal(t, "enabled", fromResponses.Thinking.Type)
			require.Positive(t, fromResponses.Thinking.BudgetTokens)

			fromChat, err := ChatCompletionsToResponses(chatReq)
			require.NoError(t, err)
			require.Equal(t, effort, fromChat.Reasoning.Effort)
		})
	}
}
