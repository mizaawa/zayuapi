package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// DegradationRun is a stored degradation check result. Prompt and Output are
// only present on detail reads so list responses stay small.
type DegradationRun struct {
	ID                  int64    `json:"id"`
	MonitorID           int64    `json:"monitor_id"`
	Status              string   `json:"status"`
	TriggerSource       string   `json:"trigger_source"`
	Model               string   `json:"model"`
	ResponseModel       string   `json:"response_model"`
	ReasoningEffort     string   `json:"reasoning_effort"`
	Protocol            string   `json:"protocol"`
	PromptName          string   `json:"prompt_name"`
	Prompt              *string  `json:"prompt,omitempty"`
	Output              *string  `json:"output,omitempty"`
	OutputBytes         int64    `json:"output_bytes"`
	ErrorMessage        string   `json:"error_message"`
	Attempts            int      `json:"attempts"`
	FirstTokenMs        *int     `json:"first_token_ms"`
	DurationMs          *int     `json:"duration_ms"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	ReasoningTokens     int64    `json:"reasoning_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
	CacheRate           *float64 `json:"cache_rate"`
	Cost                *float64 `json:"cost"`
	UsageSynced         bool     `json:"usage_synced"`
	StartedAt           string   `json:"started_at"`
	FinishedAt          *string  `json:"finished_at"`
}

// DegradationRunFromService maps a result. withBody includes the prompt and the
// generated document.
func DegradationRunFromService(run *service.DegradationRun, withBody bool) DegradationRun {
	out := DegradationRun{
		ID:                  run.ID,
		MonitorID:           run.MonitorID,
		Status:              run.Status,
		TriggerSource:       run.TriggerSource,
		Model:               run.Model,
		ResponseModel:       run.ResponseModel,
		ReasoningEffort:     run.ReasoningEffort,
		Protocol:            run.Protocol,
		PromptName:          run.PromptName,
		OutputBytes:         run.OutputBytes,
		ErrorMessage:        run.ErrorMessage,
		Attempts:            run.Attempts,
		FirstTokenMs:        run.FirstTokenMs,
		DurationMs:          run.DurationMs,
		InputTokens:         run.InputTokens,
		OutputTokens:        run.OutputTokens,
		CacheReadTokens:     run.CacheReadTokens,
		CacheCreationTokens: run.CacheCreationTokens,
		ReasoningTokens:     run.ReasoningTokens,
		TotalTokens:         run.TotalTokens(),
		CacheRate:           run.CacheRate(),
		Cost:                run.Cost,
		UsageSynced:         run.UsageSynced,
		StartedAt:           run.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:          formatOptionalRFC3339(run.FinishedAt),
	}
	if withBody {
		prompt := run.Prompt
		output := run.Output
		out.Prompt = &prompt
		out.Output = &output
	}
	return out
}

// DegradationUserRun is the read-only result shown to users. Failure reasons,
// retry counts, latency to the first token, token usage and cost stay on the
// admin side, and the generated document is only returned for a successful
// check.
type DegradationUserRun struct {
	ID              int64   `json:"id"`
	MonitorID       int64   `json:"monitor_id"`
	Status          string  `json:"status"`
	Model           string  `json:"model"`
	ResponseModel   string  `json:"response_model"`
	ReasoningEffort string  `json:"reasoning_effort"`
	Protocol        string  `json:"protocol"`
	PromptName      string  `json:"prompt_name"`
	Prompt          *string `json:"prompt,omitempty"`
	Output          *string `json:"output,omitempty"`
	DurationMs      *int    `json:"duration_ms"`
	StartedAt       string  `json:"started_at"`
	FinishedAt      *string `json:"finished_at"`
}

// DegradationUserRunFromService maps a result for users. withBody adds the
// prompt, plus the document when the check succeeded.
func DegradationUserRunFromService(run *service.DegradationRun, withBody bool) DegradationUserRun {
	out := DegradationUserRun{
		ID:              run.ID,
		MonitorID:       run.MonitorID,
		Status:          run.Status,
		Model:           run.Model,
		ResponseModel:   run.ResponseModel,
		ReasoningEffort: run.ReasoningEffort,
		Protocol:        run.Protocol,
		PromptName:      run.PromptName,
		DurationMs:      run.DurationMs,
		StartedAt:       run.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:      formatOptionalRFC3339(run.FinishedAt),
	}
	if withBody {
		prompt := run.Prompt
		out.Prompt = &prompt
		if run.Status == service.DegradationRunStatusSuccess {
			output := run.Output
			out.Output = &output
		}
	}
	return out
}

// DegradationPrompt is a prompt preset.
type DegradationPrompt struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	BuiltinKey string `json:"builtin_key"`
	SortOrder  int    `json:"sort_order"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// DegradationPromptFromService maps a prompt preset.
func DegradationPromptFromService(prompt *service.DegradationPrompt) DegradationPrompt {
	return DegradationPrompt{
		ID:         prompt.ID,
		Name:       prompt.Name,
		Prompt:     prompt.Prompt,
		BuiltinKey: prompt.BuiltinKey,
		SortOrder:  prompt.SortOrder,
		CreatedAt:  prompt.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  prompt.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func formatOptionalRFC3339(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
