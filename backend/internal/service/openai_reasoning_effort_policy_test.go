package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyReasoningEffortPoliciesIgnoredForAllPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			for _, ceiling := range []string{"max", "low", "future-level"} {
				value, err := normalizeMaxReasoningEffortForPlatform(platform, ceiling)
				require.NoError(t, err)
				require.Empty(t, value)
				require.Empty(t, NormalizeMaxReasoningEffort(ceiling))
			}
			mappings, err := NormalizeReasoningEffortMappings(platform, []ReasoningEffortMapping{
				{From: "max", To: "low"},
				{From: "max", To: "future-level"},
				{From: "invalid"},
			})
			require.NoError(t, err)
			require.Empty(t, mappings)

			group := &Group{Platform: platform, MaxReasoningEffort: "low", ReasoningEffortMappings: []ReasoningEffortMapping{{From: "max", To: "low"}}}
			sanitizeGroupReasoningEffortPolicy(group)
			require.Empty(t, group.MaxReasoningEffort)
			require.Empty(t, group.ReasoningEffortMappings)
		})
	}
}

func TestLegacyReasoningEffortPolicyPreservesRequest(t *testing.T) {
	for _, body := range []string{
		`{"model":"future-model","reasoning":{"effort":"max"}}`,
		`{"reasoning_effort":"xhigh"}`,
		`{"output_config":{"effort":"future-level"}}`,
		`{"generationConfig":{"thinkingConfig":{"thinkingLevel":"future-level"}}}`,
		`{"reasoning":{"effort":" MAX "},"reasoning_effort":"future-level"}`,
		`{"reasoning_effort":{"level":"future-level"}}`,
		`{"model":"future-model"}`,
		`invalid json`,
	} {
		t.Run(body, func(t *testing.T) {
			mappings := []ReasoningEffortMapping{{From: "max", To: "low"}, {From: "future-level", To: "medium"}}
			got, changed := ApplyOpenAIReasoningEffortPolicy([]byte(body), "low", mappings)
			require.False(t, changed)
			require.Equal(t, body, string(got))

			ctx := WithOpenAIReasoningEffortPolicy(context.Background(), "low", mappings)
			got, changed = ApplyOpenAIReasoningEffortPolicyFromContext(ctx, []byte(body))
			require.False(t, changed)
			require.Equal(t, body, string(got))
		})
	}
}
