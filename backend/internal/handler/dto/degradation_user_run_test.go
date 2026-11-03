package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Users only see what a result looks like, never why it failed or what it cost.
var degradationAdminOnlyFields = []string{
	"error_message", "attempts", "first_token_ms", "input_tokens", "output_tokens",
	"cache_read_tokens", "cache_creation_tokens", "reasoning_tokens", "total_tokens",
	"cache_rate", "cost", "usage_synced", "trigger_source", "output_bytes",
}

func degradationUserRunJSON(t *testing.T, run *service.DegradationRun, withBody bool) map[string]any {
	t.Helper()
	raw, err := json.Marshal(DegradationUserRunFromService(run, withBody))
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	return fields
}

func sampleDegradationRun(status string) *service.DegradationRun {
	started := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Minute)
	firstToken, duration := 1200, 180000
	cost := 0.42
	return &service.DegradationRun{
		ID: 9, MonitorID: 3, Status: status, TriggerSource: service.DegradationTriggerSchedule,
		Model: "gpt-6-astra", ResponseModel: "gpt-6-astra-2026", ReasoningEffort: "high",
		Protocol: service.DegradationProtocolOpenAIResponses, PromptName: "pelican", Prompt: "draw a pelican",
		Output: "<svg></svg>", OutputBytes: 11, ErrorMessage: "upstream HTTP 502: account sk-live-secret", Attempts: 2,
		FirstTokenMs: &firstToken, DurationMs: &duration, InputTokens: 81, OutputTokens: 7407,
		CacheReadTokens: 5, CacheCreationTokens: 1, ReasoningTokens: 476, Cost: &cost, UsageSynced: true,
		StartedAt: started, FinishedAt: &finished,
	}
}

func TestDegradationUserRunOmitsAdminOnlyFields(t *testing.T) {
	for _, status := range []string{service.DegradationRunStatusSuccess, service.DegradationRunStatusFailed} {
		for _, withBody := range []bool{false, true} {
			fields := degradationUserRunJSON(t, sampleDegradationRun(status), withBody)
			for _, key := range degradationAdminOnlyFields {
				require.NotContains(t, fields, key, "status=%s body=%v", status, withBody)
			}
			raw, err := json.Marshal(fields)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "sk-live-secret")
			require.Equal(t, "gpt-6-astra", fields["model"])
			require.Equal(t, "high", fields["reasoning_effort"])
			require.Equal(t, service.DegradationProtocolOpenAIResponses, fields["protocol"])
			require.EqualValues(t, 180000, fields["duration_ms"])
			require.Equal(t, "2026-11-03T08:03:00Z", fields["finished_at"])
		}
	}
}

func TestDegradationUserRunReturnsDocumentsOnlyForSuccess(t *testing.T) {
	list := degradationUserRunJSON(t, sampleDegradationRun(service.DegradationRunStatusSuccess), false)
	require.NotContains(t, list, "output")
	require.NotContains(t, list, "prompt")

	success := degradationUserRunJSON(t, sampleDegradationRun(service.DegradationRunStatusSuccess), true)
	require.Equal(t, "<svg></svg>", success["output"])
	require.Equal(t, "draw a pelican", success["prompt"])

	failed := degradationUserRunJSON(t, sampleDegradationRun(service.DegradationRunStatusFailed), true)
	require.NotContains(t, failed, "output", "a failed check never exposes partial output")
	require.Equal(t, "draw a pelican", failed["prompt"])
}
