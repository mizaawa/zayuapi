package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromServicePreservesCustomSystemPromptDraft(t *testing.T) {
	source := &service.APIKey{
		CustomSystemPromptEnabled: false,
		CustomSystemPromptForce:   true,
		CustomSystemPrompt:        "  Draft project instructions\n",
	}
	out := APIKeyFromService(source)
	require.False(t, out.CustomSystemPromptEnabled)
	require.True(t, out.CustomSystemPromptForce)
	require.Equal(t, source.CustomSystemPrompt, out.CustomSystemPrompt)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(encoded, &wire))
	require.Equal(t, false, wire["custom_system_prompt_enabled"])
	require.Equal(t, true, wire["custom_system_prompt_force"])
	require.Equal(t, source.CustomSystemPrompt, wire["custom_system_prompt"])
}
