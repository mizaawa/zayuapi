package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	degradationAnthropicMaxTokens = 32000
	degradationMaxSSELineBytes    = 4 << 20
	degradationErrorBodyBytes     = 4 << 10
)

var errDegradationOutputTooLarge = errors.New("generated output exceeded the 1 MiB limit")

// degradationHTTPClient only talks to this deployment's own gateway listener.
// Streaming answers may take many minutes, so the per-attempt context carries
// the deadline instead of a client-wide timeout. Redirects are refused so the
// managed credential never leaves the configured endpoint.
var degradationHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext:         monitorDialer.DialContext,
		MaxIdleConns:        8,
		IdleConnTimeout:     monitorIdleConnTimeout,
		TLSHandshakeTimeout: monitorTLSHandshakeTimeout,
	},
	CheckRedirect: refuseMonitorRedirect,
}

// degradationRequest describes one generation request sent to the gateway.
type degradationRequest struct {
	Endpoint string
	APIKey   string
	Protocol string
	Model    string
	Effort   string
	Prompt   string
	Timeout  time.Duration
	Attestor *ChannelMonitorAttestor
}

// degradationAttempt is the outcome of one request.
type degradationAttempt struct {
	Output          string
	ResponseModel   string
	FirstTokenMs    *int
	DurationMs      int
	Usage           DegradationUsageSummary
	ReasoningTokens int64
	Sent            bool
	Err             error
}

// degradationEffortForProtocol adapts effort labels that only one family understands.
func degradationEffortForProtocol(protocol, effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if protocol == DegradationProtocolAnthropic {
		if effort == "minimal" {
			return "low"
		}
		return effort
	}
	if effort == "max" {
		return "xhigh"
	}
	return effort
}

// buildDegradationRequest returns the gateway path, JSON body and headers.
func buildDegradationRequest(req degradationRequest) (string, []byte, map[string]string, error) {
	effort := degradationEffortForProtocol(req.Protocol, req.Effort)
	var path string
	body := map[string]any{"model": req.Model, "stream": true}
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "text/event-stream",
	}
	switch req.Protocol {
	case DegradationProtocolOpenAIResponses:
		path = providerOpenAIResponsesPath
		body["store"] = false
		body["instructions"] = degradationSystemInstructions
		body["input"] = []map[string]any{{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": req.Prompt}},
		}}
		if effort != "" {
			body["reasoning"] = map[string]any{"effort": effort}
		}
		headers["Authorization"] = "Bearer " + req.APIKey
	case DegradationProtocolOpenAIChat:
		path = providerOpenAIPath
		body["stream_options"] = map[string]any{"include_usage": true}
		body["messages"] = []map[string]any{
			{"role": "system", "content": degradationSystemInstructions},
			{"role": "user", "content": req.Prompt},
		}
		if effort != "" {
			body["reasoning_effort"] = effort
		}
		headers["Authorization"] = "Bearer " + req.APIKey
	case DegradationProtocolAnthropic:
		path = providerAnthropicPath
		body["max_tokens"] = degradationAnthropicMaxTokens
		body["system"] = degradationSystemInstructions
		body["messages"] = []map[string]any{{"role": "user", "content": req.Prompt}}
		if effort != "" && effort != "none" {
			body["thinking"] = map[string]any{"type": "adaptive"}
			body["output_config"] = map[string]any{"effort": effort}
		}
		headers["x-api-key"] = req.APIKey
		headers["anthropic-version"] = monitorAnthropicAPIVersion
	default:
		return "", nil, nil, ErrDegradationInvalidProtocol
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, nil, fmt.Errorf("marshal degradation request: %w", err)
	}
	return path, payload, headers, nil
}

// runDegradationAttempt sends one streaming request and collects the generated document.
func runDegradationAttempt(ctx context.Context, client *http.Client, req degradationRequest) *degradationAttempt {
	attempt := &degradationAttempt{}
	start := time.Now()
	defer func() { attempt.DurationMs = int(time.Since(start) / time.Millisecond) }()

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = time.Duration(DegradationDefaultTimeoutMinutes) * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	path, payload, headers, err := buildDegradationRequest(req)
	if err != nil {
		attempt.Err = err
		return attempt
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(req.Endpoint, path), bytes.NewReader(payload))
	if err != nil {
		attempt.Err = fmt.Errorf("build request: %w", err)
		return attempt
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set("User-Agent", "sub2api-degradation-monitor/1")
	if err := req.Attestor.SignRequest(httpReq, req.APIKey, payload); err != nil {
		attempt.Err = fmt.Errorf("sign managed request: %w", err)
		return attempt
	}

	attempt.Sent = true
	resp, err := client.Do(httpReq)
	if err != nil {
		attempt.Err = degradationContextError(ctx, timeout, fmt.Errorf("request failed: %w", err))
		return attempt
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, degradationErrorBodyBytes))
		attempt.Err = fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, truncateForErrorBody(degradationErrorText(snippet)))
		return attempt
	}

	state := newDegradationStreamState(start)
	if isDegradationJSONResponse(resp.Header.Get("Content-Type")) {
		err = state.consumeJSON(req.Protocol, resp.Body)
	} else {
		err = readServerSentEvents(resp.Body, func(event string, data []byte) error {
			return state.handle(req.Protocol, event, data)
		})
	}
	state.applyTo(attempt)
	attempt.Err = state.outcome(ctx, timeout, err)
	return attempt
}

func degradationContextError(ctx context.Context, timeout time.Duration, fallback error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("request did not finish within %d minutes", int(timeout/time.Minute))
	}
	if ctx.Err() != nil {
		return errors.New("check interrupted")
	}
	return fallback
}

func isDegradationJSONResponse(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

// degradationErrorText prefers a structured upstream error message.
func degradationErrorText(body []byte) string {
	for _, path := range []string{"error.message", "message", "error"} {
		if value := gjson.GetBytes(body, path); value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return value.String()
		}
	}
	return string(body)
}

// readServerSentEvents dispatches each SSE event. A handler error stops reading.
func readServerSentEvents(r io.Reader, handle func(event string, data []byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), degradationMaxSSELineBytes)
	var event string
	var data bytes.Buffer
	dispatch := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		err := handle(event, data.Bytes())
		event = ""
		data.Reset()
		return err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			event = string(value)
		case "data":
			if data.Len() > 0 {
				_ = data.WriteByte('\n')
			}
			_, _ = data.Write(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return dispatch()
}

// degradationStreamState accumulates one streamed answer.
type degradationStreamState struct {
	start           time.Time
	output          strings.Builder
	firstTokenMs    *int
	model           string
	usage           DegradationUsageSummary
	reasoningTokens int64
	completed       bool
	failure         string
}

func newDegradationStreamState(start time.Time) *degradationStreamState {
	return &degradationStreamState{start: start}
}

func (s *degradationStreamState) appendText(text string) error {
	if text == "" {
		return nil
	}
	if s.firstTokenMs == nil {
		ms := int(time.Since(s.start) / time.Millisecond)
		s.firstTokenMs = &ms
	}
	if s.output.Len()+len(text) > DegradationMaxOutputBytes {
		return errDegradationOutputTooLarge
	}
	_, _ = s.output.WriteString(text)
	return nil
}

func (s *degradationStreamState) setModel(model string) {
	if model = strings.TrimSpace(model); model != "" {
		s.model = model
	}
}

func (s *degradationStreamState) handle(protocol, event string, data []byte) error {
	switch protocol {
	case DegradationProtocolOpenAIResponses:
		return s.handleResponsesEvent(event, data)
	case DegradationProtocolOpenAIChat:
		return s.handleChatChunk(data)
	case DegradationProtocolAnthropic:
		return s.handleAnthropicEvent(event, data)
	default:
		return ErrDegradationInvalidProtocol
	}
}

func (s *degradationStreamState) handleResponsesEvent(event string, data []byte) error {
	kind := gjson.GetBytes(data, "type").String()
	if kind == "" {
		kind = event
	}
	switch kind {
	case "response.output_text.delta":
		return s.appendText(gjson.GetBytes(data, "delta").String())
	case "response.created", "response.in_progress":
		s.setModel(gjson.GetBytes(data, "response.model").String())
	case "response.completed":
		response := gjson.GetBytes(data, "response")
		s.applyResponsesFinal(response)
		s.completed = true
	case "response.incomplete":
		response := gjson.GetBytes(data, "response")
		s.applyResponsesFinal(response)
		reason := response.Get("incomplete_details.reason").String()
		if reason == "" {
			reason = "unknown"
		}
		s.failure = "response incomplete: " + reason
	case "response.failed":
		s.failure = firstNonEmptyString(gjson.GetBytes(data, "response.error.message").String(), "response failed")
	case "error":
		s.failure = firstNonEmptyString(gjson.GetBytes(data, "error.message").String(), gjson.GetBytes(data, "message").String(), "stream error")
	}
	return nil
}

func (s *degradationStreamState) applyResponsesFinal(response gjson.Result) {
	s.setModel(response.Get("model").String())
	s.applyOpenAIUsage(
		response.Get("usage.input_tokens"),
		response.Get("usage.input_tokens_details.cached_tokens"),
		response.Get("usage.output_tokens"),
		response.Get("usage.output_tokens_details.reasoning_tokens"),
	)
	if s.output.Len() == 0 && response.Exists() {
		_ = s.appendText(extractOpenAIResponsesText([]byte(response.Raw)))
	}
}

func (s *degradationStreamState) handleChatChunk(data []byte) error {
	if string(bytes.TrimSpace(data)) == "[DONE]" {
		s.completed = true
		return nil
	}
	if message := gjson.GetBytes(data, "error.message").String(); message != "" {
		s.failure = message
		return nil
	}
	s.setModel(gjson.GetBytes(data, "model").String())
	var appendErr error
	gjson.GetBytes(data, "choices").ForEach(func(_, choice gjson.Result) bool {
		if err := s.appendText(choice.Get("delta.content").String()); err != nil {
			appendErr = err
			return false
		}
		switch choice.Get("finish_reason").String() {
		case "stop":
			s.completed = true
		case "length":
			s.failure = "output truncated: finish_reason=length"
		case "content_filter":
			s.failure = "output blocked: finish_reason=content_filter"
		}
		return true
	})
	if appendErr != nil {
		return appendErr
	}
	if usage := gjson.GetBytes(data, "usage"); usage.IsObject() {
		s.applyOpenAIUsage(
			usage.Get("prompt_tokens"),
			usage.Get("prompt_tokens_details.cached_tokens"),
			usage.Get("completion_tokens"),
			usage.Get("completion_tokens_details.reasoning_tokens"),
		)
	}
	return nil
}

func (s *degradationStreamState) applyOpenAIUsage(input, cached, output, reasoning gjson.Result) {
	if !input.Exists() && !output.Exists() {
		return
	}
	inputTokens := input.Int()
	cachedTokens := cached.Int()
	if cachedTokens > inputTokens {
		cachedTokens = inputTokens
	}
	s.usage.InputTokens = inputTokens - cachedTokens
	s.usage.CacheReadTokens = cachedTokens
	s.usage.CacheCreationTokens = 0
	s.usage.OutputTokens = output.Int()
	s.reasoningTokens = reasoning.Int()
}

func (s *degradationStreamState) handleAnthropicEvent(event string, data []byte) error {
	kind := gjson.GetBytes(data, "type").String()
	if kind == "" {
		kind = event
	}
	switch kind {
	case "message_start":
		message := gjson.GetBytes(data, "message")
		s.setModel(message.Get("model").String())
		s.applyAnthropicUsage(message.Get("usage"))
	case "content_block_delta":
		if gjson.GetBytes(data, "delta.type").String() == "text_delta" {
			return s.appendText(gjson.GetBytes(data, "delta.text").String())
		}
	case "message_delta":
		s.applyAnthropicUsage(gjson.GetBytes(data, "usage"))
		switch gjson.GetBytes(data, "delta.stop_reason").String() {
		case "max_tokens":
			s.failure = "output truncated: stop_reason=max_tokens"
		case "refusal":
			s.failure = "model refused: stop_reason=refusal"
		}
	case "message_stop":
		s.completed = true
	case "error":
		s.failure = firstNonEmptyString(gjson.GetBytes(data, "error.message").String(), "stream error")
	}
	return nil
}

// applyAnthropicUsage merges cumulative usage counters from message events.
func (s *degradationStreamState) applyAnthropicUsage(usage gjson.Result) {
	if !usage.IsObject() {
		return
	}
	mergeMax := func(current *int64, value gjson.Result) {
		if value.Exists() && value.Int() > *current {
			*current = value.Int()
		}
	}
	mergeMax(&s.usage.InputTokens, usage.Get("input_tokens"))
	mergeMax(&s.usage.OutputTokens, usage.Get("output_tokens"))
	mergeMax(&s.usage.CacheReadTokens, usage.Get("cache_read_input_tokens"))
	mergeMax(&s.usage.CacheCreationTokens, usage.Get("cache_creation_input_tokens"))
}

// consumeJSON accepts a non-streaming answer when a gateway path ignores stream=true.
func (s *degradationStreamState) consumeJSON(protocol string, body io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(body, DegradationMaxOutputBytes*2))
	if err != nil {
		return err
	}
	switch protocol {
	case DegradationProtocolOpenAIResponses:
		s.applyResponsesFinal(gjson.ParseBytes(raw))
		s.completed = s.output.Len() > 0
	case DegradationProtocolOpenAIChat:
		s.setModel(gjson.GetBytes(raw, "model").String())
		if err := s.appendText(gjson.GetBytes(raw, "choices.0.message.content").String()); err != nil {
			return err
		}
		usage := gjson.GetBytes(raw, "usage")
		s.applyOpenAIUsage(usage.Get("prompt_tokens"), usage.Get("prompt_tokens_details.cached_tokens"),
			usage.Get("completion_tokens"), usage.Get("completion_tokens_details.reasoning_tokens"))
		s.completed = s.output.Len() > 0
	case DegradationProtocolAnthropic:
		s.setModel(gjson.GetBytes(raw, "model").String())
		s.applyAnthropicUsage(gjson.GetBytes(raw, "usage"))
		if err := s.appendText(extractAnthropicMonitorText(raw)); err != nil {
			return err
		}
		s.completed = s.output.Len() > 0
	}
	if !s.completed {
		s.failure = firstNonEmptyString(degradationErrorText(raw), "empty response")
	}
	return nil
}

func (s *degradationStreamState) applyTo(attempt *degradationAttempt) {
	attempt.Output = s.output.String()
	attempt.ResponseModel = s.model
	attempt.FirstTokenMs = s.firstTokenMs
	attempt.Usage = s.usage
	attempt.ReasoningTokens = s.reasoningTokens
}

// outcome classifies the finished stream. Any non-nil result counts as a failed attempt.
func (s *degradationStreamState) outcome(ctx context.Context, timeout time.Duration, readErr error) error {
	switch {
	case errors.Is(readErr, errDegradationOutputTooLarge):
		return readErr
	case ctx.Err() != nil:
		return degradationContextError(ctx, timeout, ctx.Err())
	case s.failure != "":
		return errors.New(s.failure)
	case readErr != nil:
		return fmt.Errorf("stream interrupted: %w", readErr)
	case !s.completed:
		return errors.New("stream ended before the response completed")
	case strings.TrimSpace(s.output.String()) == "":
		return errors.New("model returned an empty answer")
	default:
		return nil
	}
}
