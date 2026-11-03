//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type degradationGatewayRecorder struct {
	mu       sync.Mutex
	requests []degradationRecordedRequest
}

type degradationRecordedRequest struct {
	path     string
	header   http.Header
	body     []byte
	attested bool
}

func (r *degradationGatewayRecorder) record(req *http.Request, attestor *ChannelMonitorAttestor, apiKey string) degradationRecordedRequest {
	attested := attestor.ValidateRequest(req, apiKey)
	body, _ := io.ReadAll(req.Body)
	recorded := degradationRecordedRequest{path: req.URL.Path, header: req.Header.Clone(), body: body, attested: attested}
	r.mu.Lock()
	r.requests = append(r.requests, recorded)
	r.mu.Unlock()
	return recorded
}

func (r *degradationGatewayRecorder) all() []degradationRecordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]degradationRecordedRequest(nil), r.requests...)
}

func writeDegradationSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, event := range events {
		_, _ = io.WriteString(w, event+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func responsesSuccessEvents(html string) []string {
	half := len(html) / 2
	first, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": html[:half]})
	second, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": html[half:]})
	return []string{
		`event: response.created` + "\n" + `data: {"type":"response.created","response":{"model":"gpt-5.5-2026-01"}}`,
		`event: response.output_text.delta` + "\n" + "data: " + string(first),
		`event: response.output_text.delta` + "\n" + "data: " + string(second),
		`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"model":"gpt-5.5-2026-01","usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":20},"output_tokens":900,"output_tokens_details":{"reasoning_tokens":400}}}}`,
	}
}

func (env *degradationTestEnv) apiKeyFor(t *testing.T, monitor *DegradationMonitor) string {
	t.Helper()
	stored, err := env.repo.GetMonitor(context.Background(), monitor.ID)
	require.NoError(t, err)
	key, err := env.keys.GetManagedMonitorKey(context.Background(), stored.APIKeyID)
	require.NoError(t, err)
	return key.Key
}

func latestRun(t *testing.T, env *degradationTestEnv, monitorID int64) *DegradationRun {
	t.Helper()
	runs := env.repo.monitorRuns(monitorID)
	require.NotEmpty(t, runs)
	return runs[0]
}

func TestDegradationExecuteRunResponsesSuccessSyncsUsage(t *testing.T) {
	recorder := &degradationGatewayRecorder{}
	var env *degradationTestEnv
	var monitor *DegradationMonitor
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder.record(req, env.attestor, env.apiKeyFor(t, monitor))
		writeDegradationSSE(w, responsesSuccessEvents("<!doctype html><html><body><svg viewBox='0 0 10 10'></svg></body></html>")...)
	}))
	defer server.Close()

	env = newDegradationTestEnv(true, server.URL)
	ctx := context.Background()
	prompt := env.repo.addPrompt("Pelican", "draw a pelican riding a bicycle", 10)
	var err error
	monitor, err = env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt-5.5", PromptID: &prompt.ID, Enabled: true}, 7)
	require.NoError(t, err)
	env.repo.usage = []*DegradationUsageSummary{
		{Requests: 1, InputTokens: 100, OutputTokens: 900, CacheReadTokens: 20, Cost: 0.0123},
		{Requests: 1, InputTokens: 100, OutputTokens: 900, CacheReadTokens: 20, Cost: 0.0123},
	}

	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)

	requests := recorder.all()
	require.Len(t, requests, 1)
	request := requests[0]
	require.Equal(t, "/v1/responses", request.path)
	require.True(t, request.attested, "managed requests must carry a valid attestation")
	require.Equal(t, "Bearer "+env.apiKeyFor(t, monitor), request.header.Get("Authorization"))
	require.Equal(t, "text/event-stream", request.header.Get("Accept"))
	require.Equal(t, "gpt-5.5", gjson.GetBytes(request.body, "model").String())
	require.True(t, gjson.GetBytes(request.body, "stream").Bool())
	require.False(t, gjson.GetBytes(request.body, "store").Bool())
	require.Equal(t, degradationSystemInstructions, gjson.GetBytes(request.body, "instructions").String())
	require.Equal(t, "draw a pelican riding a bicycle", gjson.GetBytes(request.body, "input.0.content.0.text").String())
	require.Equal(t, "high", gjson.GetBytes(request.body, "reasoning.effort").String())

	run := latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusSuccess, run.Status, run.ErrorMessage)
	require.Equal(t, DegradationTriggerSchedule, run.TriggerSource)
	require.Equal(t, "<!doctype html><html><body><svg viewBox='0 0 10 10'></svg></body></html>", run.Output)
	require.Equal(t, "gpt-5.5-2026-01", run.ResponseModel)
	require.Equal(t, "Pelican", run.PromptName)
	require.Equal(t, prompt.ID, *run.PromptID)
	require.Equal(t, 1, run.Attempts)
	require.NotNil(t, run.FirstTokenMs)
	require.NotNil(t, run.DurationMs)
	require.NotNil(t, run.FinishedAt)
	require.Equal(t, int64(400), run.ReasoningTokens)
	require.True(t, run.UsageSynced)
	require.Equal(t, int64(100), run.InputTokens)
	require.Equal(t, int64(20), run.CacheReadTokens)
	require.InDelta(t, 0.0123, *run.Cost, 1e-12)
}

func TestDegradationExecuteRunRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, `{"error":{"message":"No available accounts"}}`, http.StatusServiceUnavailable)
			return
		}
		writeDegradationSSE(w, responsesSuccessEvents("<svg xmlns='http://www.w3.org/2000/svg'></svg>")...)
	}))
	defer server.Close()

	env := newDegradationTestEnv(true, server.URL)
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerManual)

	run := latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusSuccess, run.Status, run.ErrorMessage)
	require.Equal(t, 2, run.Attempts)
	require.Equal(t, DegradationTriggerManual, run.TriggerSource)
	require.Equal(t, DegradationPelicanPrompt, run.Prompt, "an empty preset table falls back to the built-in pelican")
	stored, err := env.repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.LastRunAt, "manual runs stamp the schedule")
}

func TestDegradationExecuteRunFailureRedactsKeyAndStopsAfterRetries(t *testing.T) {
	var calls atomic.Int32
	var env *degradationTestEnv
	var monitor *DegradationMonitor
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		key := env.apiKeyFor(t, monitor)
		http.Error(w, fmt.Sprintf(`{"error":{"message":"invalid key %s"}}`, key), http.StatusUnauthorized)
	}))
	defer server.Close()

	env = newDegradationTestEnv(true, server.URL)
	ctx := context.Background()
	_, err := env.svc.UpdateConfig(ctx, DegradationConfig{DefaultIntervalMinutes: 60, TimeoutMinutes: 1, MaxRetries: 2, MaxDisplayPerGroup: 25, DefaultReasoningEffort: "high"})
	require.NoError(t, err)
	monitor, err = env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)

	require.Equal(t, int32(3), calls.Load())
	run := latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusFailed, run.Status)
	require.Equal(t, 3, run.Attempts)
	require.Contains(t, run.ErrorMessage, "upstream HTTP 401")
	require.NotContains(t, run.ErrorMessage, env.apiKeyFor(t, monitor))
	require.Contains(t, run.ErrorMessage, "REDACTED")
}

func TestDegradationExecuteRunAnthropicProtocol(t *testing.T) {
	recorder := &degradationGatewayRecorder{}
	var env *degradationTestEnv
	var monitor *DegradationMonitor
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder.record(req, env.attestor, env.apiKeyFor(t, monitor))
		writeDegradationSSE(w,
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"model":"claude-opus-5","usage":{"input_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5,"output_tokens":1}}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"<svg>"}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"</svg>"}}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":700}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`,
		)
	}))
	defer server.Close()

	env = newDegradationTestEnv(true, server.URL)
	ctx := context.Background()
	var err error
	monitor, err = env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 2, Model: "claude-opus-5", ReasoningEffort: "minimal", Enabled: true}, 7)
	require.NoError(t, err)

	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)

	requests := recorder.all()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1/messages", requests[0].path)
	require.True(t, requests[0].attested)
	require.Equal(t, env.apiKeyFor(t, monitor), requests[0].header.Get("x-api-key"))
	require.Equal(t, monitorAnthropicAPIVersion, requests[0].header.Get("anthropic-version"))
	require.Equal(t, "adaptive", gjson.GetBytes(requests[0].body, "thinking.type").String())
	require.Equal(t, "low", gjson.GetBytes(requests[0].body, "output_config.effort").String())
	require.Equal(t, int64(degradationAnthropicMaxTokens), gjson.GetBytes(requests[0].body, "max_tokens").Int())
	require.Equal(t, degradationSystemInstructions, gjson.GetBytes(requests[0].body, "system").String())

	run := latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusSuccess, run.Status, run.ErrorMessage)
	require.Equal(t, "<svg></svg>", run.Output, "thinking deltas are not part of the document")
	require.Equal(t, "claude-opus-5", run.ResponseModel)
	require.Equal(t, "minimal", run.ReasoningEffort)
	require.False(t, run.UsageSynced, "no usage logs: stream usage is kept")
	require.Equal(t, int64(50), run.InputTokens)
	require.Equal(t, int64(10), run.CacheReadTokens)
	require.Equal(t, int64(5), run.CacheCreationTokens)
	require.Equal(t, int64(700), run.OutputTokens)
	require.Nil(t, run.Cost)
}

func TestDegradationAttemptOpenAIChatStream(t *testing.T) {
	attestor, err := NewChannelMonitorAttestor(degradationTestEncryptionKey)
	require.NoError(t, err)
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, "/v1/chat/completions", req.URL.Path)
		require.True(t, attestor.ValidateRequest(req, "sk-test-chat"))
		body, _ = io.ReadAll(req.Body)
		writeDegradationSSE(w,
			`data: {"model":"gpt-chat","choices":[{"delta":{"role":"assistant","content":""}}]}`,
			`data: {"model":"gpt-chat","choices":[{"delta":{"content":"<svg"}}]}`,
			`data: {"model":"gpt-chat","choices":[{"delta":{"content":"/>"},"finish_reason":"stop"}]}`,
			`data: {"model":"gpt-chat","choices":[],"usage":{"prompt_tokens":80,"prompt_tokens_details":{"cached_tokens":30},"completion_tokens":200,"completion_tokens_details":{"reasoning_tokens":50}}}`,
			`data: [DONE]`,
		)
	}))
	defer server.Close()

	attempt := runDegradationAttempt(context.Background(), server.Client(), degradationRequest{
		Endpoint: server.URL, APIKey: "sk-test-chat", Protocol: DegradationProtocolOpenAIChat,
		Model: "gpt-chat", Effort: "max", Prompt: "draw", Timeout: time.Minute, Attestor: attestor,
	})
	require.NoError(t, attempt.Err)
	require.Equal(t, "<svg/>", attempt.Output)
	require.Equal(t, "gpt-chat", attempt.ResponseModel)
	require.Equal(t, int64(50), attempt.Usage.InputTokens)
	require.Equal(t, int64(30), attempt.Usage.CacheReadTokens)
	require.Equal(t, int64(200), attempt.Usage.OutputTokens)
	require.Equal(t, int64(50), attempt.ReasoningTokens)
	require.Equal(t, "xhigh", gjson.GetBytes(body, "reasoning_effort").String())
	require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
	require.Equal(t, "system", gjson.GetBytes(body, "messages.0.role").String())
	require.Equal(t, "draw", gjson.GetBytes(body, "messages.1.content").String())
}

func TestDegradationAttemptFailureModes(t *testing.T) {
	attestor, err := NewChannelMonitorAttestor(degradationTestEncryptionKey)
	require.NoError(t, err)
	request := func(server *httptest.Server, protocol string, timeout time.Duration) *degradationAttempt {
		return runDegradationAttempt(context.Background(), server.Client(), degradationRequest{
			Endpoint: server.URL, APIKey: "sk-test", Protocol: protocol, Model: "m", Effort: "high",
			Prompt: "p", Timeout: timeout, Attestor: attestor,
		})
	}

	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			select {
			case <-release:
			case <-req.Context().Done():
			}
		}))
		defer server.Close()
		defer close(release)
		attempt := request(server, DegradationProtocolOpenAIResponses, 50*time.Millisecond)
		require.Error(t, attempt.Err)
		require.Contains(t, attempt.Err.Error(), "did not finish within")
	})

	t.Run("stream ends without completion", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeDegradationSSE(w, `data: {"type":"response.output_text.delta","delta":"<svg>"}`)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolOpenAIResponses, time.Minute)
		require.EqualError(t, attempt.Err, "stream ended before the response completed")
		require.Equal(t, "<svg>", attempt.Output)
	})

	t.Run("upstream stream error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeDegradationSSE(w, `event: response.failed`+"\n"+`data: {"type":"response.failed","response":{"error":{"message":"model overloaded"}}}`)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolOpenAIResponses, time.Minute)
		require.EqualError(t, attempt.Err, "model overloaded")
	})

	t.Run("truncated anthropic answer", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeDegradationSSE(w,
				`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"<svg>"}}`,
				`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":32000}}`,
				`data: {"type":"message_stop"}`,
			)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolAnthropic, time.Minute)
		require.EqualError(t, attempt.Err, "output truncated: stop_reason=max_tokens")
	})

	t.Run("empty answer", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeDegradationSSE(w, `data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`, `data: [DONE]`)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolOpenAIChat, time.Minute)
		require.EqualError(t, attempt.Err, "model returned an empty answer")
	})

	t.Run("output too large", func(t *testing.T) {
		chunk := strings.Repeat("a", 64*1024)
		event, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": chunk})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			events := make([]string, 0, 18)
			for i := 0; i < 18; i++ {
				events = append(events, "data: "+string(event))
			}
			writeDegradationSSE(w, events...)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolOpenAIResponses, time.Minute)
		require.ErrorIs(t, attempt.Err, errDegradationOutputTooLarge)
		require.LessOrEqual(t, len(attempt.Output), DegradationMaxOutputBytes)
	})

	t.Run("non streaming json fallback", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"model":"m-json","output":[{"type":"message","content":[{"type":"output_text","text":"<svg/>"}]}],"usage":{"input_tokens":5,"output_tokens":6}}`)
		}))
		defer server.Close()
		attempt := request(server, DegradationProtocolOpenAIResponses, time.Minute)
		require.NoError(t, attempt.Err)
		require.Equal(t, "<svg/>", attempt.Output)
		require.Equal(t, "m-json", attempt.ResponseModel)
	})
}

func TestDegradationExecuteRunHandlesUnavailableGroupAndMissingKey(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeDegradationSSE(w, responsesSuccessEvents("<svg></svg>")...)
	}))
	defer server.Close()

	env := newDegradationTestEnv(true, server.URL)
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	env.groups.groups[1].Status = "inactive"
	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)
	run := latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusFailed, run.Status)
	require.Contains(t, run.ErrorMessage, "group unavailable")
	require.Zero(t, calls.Load())

	// A managed key removed out of band is recreated before the next check.
	env.groups.groups[1].Status = StatusActive
	oldKeyID := monitor.APIKeyID
	delete(env.keys.keys, oldKeyID)
	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)
	run = latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusSuccess, run.Status, run.ErrorMessage)
	stored, err := env.repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.NotEqual(t, oldKeyID, stored.APIKeyID)
	require.Equal(t, int32(1), calls.Load())

	// A transient key lookup failure fails the check but keeps the credential.
	currentKeyID := stored.APIKeyID
	env.keys.getErr = fmt.Errorf("database unavailable")
	env.svc.ExecuteRun(ctx, monitor.ID, DegradationTriggerSchedule)
	run = latestRun(t, env, monitor.ID)
	require.Equal(t, DegradationRunStatusFailed, run.Status)
	require.Contains(t, run.ErrorMessage, "managed key unavailable")
	stored, err = env.repo.GetMonitor(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, currentKeyID, stored.APIKeyID)
	require.Equal(t, int32(1), calls.Load())
}

func TestDegradationRecoverStaleRunsAndPrune(t *testing.T) {
	env := newDegradationTestEnv(true, "")
	ctx := context.Background()
	monitor, err := env.svc.CreateMonitor(ctx, DegradationMonitorInput{GroupID: 1, Model: "gpt", Enabled: true}, 7)
	require.NoError(t, err)

	stale := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusRunning, StartedAt: time.Now().Add(-3 * time.Hour)}
	live := &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusRunning, StartedAt: time.Now()}
	require.NoError(t, env.repo.CreateRun(ctx, stale))
	require.NoError(t, env.repo.CreateRun(ctx, live))
	env.svc.RecoverStaleRuns(ctx)
	staleRun, err := env.repo.GetRun(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, DegradationRunStatusFailed, staleRun.Status)
	liveRun, err := env.repo.GetRun(ctx, live.ID)
	require.NoError(t, err)
	require.Equal(t, DegradationRunStatusRunning, liveRun.Status)

	for i := 0; i < DegradationRunRetention+5; i++ {
		require.NoError(t, env.repo.CreateRun(ctx, &DegradationRun{MonitorID: monitor.ID, Status: DegradationRunStatusSuccess}))
	}
	deleted, err := env.repo.PruneRuns(ctx, monitor.ID, DegradationRunRetention)
	require.NoError(t, err)
	require.Equal(t, int64(7), deleted)
	require.Len(t, env.repo.monitorRuns(monitor.ID), DegradationRunRetention)
}

func TestReadServerSentEventsHandlesMultilineAndComments(t *testing.T) {
	input := ": keepalive\r\nevent: custom\r\ndata: line one\r\ndata: line two\r\n\r\ndata: tail"
	var events []string
	err := readServerSentEvents(strings.NewReader(input), func(event string, data []byte) error {
		events = append(events, event+"|"+string(data))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"custom|line one\nline two", "|tail"}, events)
}
