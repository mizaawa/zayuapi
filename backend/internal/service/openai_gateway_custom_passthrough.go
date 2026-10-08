package service

// Custom API-key accounts are deliberately boring: the gateway authenticates
// and schedules the request, then forwards the payload to the configured
// OpenAI-compatible endpoint without changing its protocol or model fields.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	customResponsesEndpoint       = "/v1/responses"
	customChatCompletionsEndpoint = "/v1/chat/completions"
	customMessagesEndpoint        = "/v1/messages"
)

// forwardCustomTransparent dispatches OpenAI-compatible requests without
// protocol conversion. Account-level model mapping is the only body rewrite;
// all other request fields and the upstream response remain transparent.
func (s *OpenAIGatewayService) forwardCustomTransparent(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	endpoint string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil || !account.IsCustom() || account.Type != AccountTypeAPIKey {
		return nil, errors.New("custom transparent forwarding requires a Custom API-key account")
	}

	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if model == "" {
		if c != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
				"type": "invalid_request_error", "message": "model is required",
			}})
		}
		return nil, errors.New("missing model in Custom request")
	}
	upstreamModel := model
	forwardBody := body
	if mappedModel := strings.TrimSpace(account.GetMappedModel(model)); mappedModel != "" && mappedModel != model {
		upstreamModel = mappedModel
		forwardBody = ReplaceModelInBody(body, mappedModel)
	}
	promptProtocol := apiKeySystemPromptResponses
	if endpoint == customMessagesEndpoint {
		promptProtocol = apiKeySystemPromptAnthropic
	}
	var promptErr error
	forwardBody, promptErr = applyAPIKeySystemPrompt(c, forwardBody, promptProtocol)
	if promptErr != nil {
		return nil, promptErr
	}
	stream := gjson.GetBytes(forwardBody, "stream").Bool()
	serviceTier := extractOpenAIServiceTierFromBody(forwardBody)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(forwardBody, model)

	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	baseURL, err := requireOpenAIBaseURL(account)
	if err != nil {
		return nil, err
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	targetURL := buildOpenAIEndpointURL(validatedURL, endpoint)
	SetActualOpenAIUpstreamEndpoint(c, endpoint)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(forwardBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build Custom upstream request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	if endpoint == customMessagesEndpoint {
		copyCustomAnthropicHeaders(c, req.Header, s.isOpenAIPassthroughTimeoutHeadersAllowed())
	} else {
		s.copyCustomClientHeaders(c, req.Header)
	}
	// Never forward the caller's credential. The account key is the only
	// credential allowed to reach the configured upstream. Native Anthropic
	// Messages endpoints use x-api-key; OpenAI-compatible routes use Bearer.
	req.Header.Del("Authorization")
	req.Header.Del("X-Api-Key")
	req.Header.Del("X-Goog-Api-Key")
	if endpoint == customMessagesEndpoint {
		req.Header.Set("X-Api-Key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if endpoint == customMessagesEndpoint && req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if req.Header.Get("Accept") == "" {
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		} else {
			req.Header.Set("Accept", "application/json")
		}
	}
	// Explicit account overrides are applied last, matching the other OpenAI
	// API-key paths and allowing an upstream-specific header when configured.
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
	}
	if resp == nil {
		return nil, errors.New("custom upstream returned no response")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		responseBody := s.readUpstreamErrorBody(resp)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(responseBody))
		if shouldFailoverOpenAIPassthroughResponse(account, resp.StatusCode, responseBody) {
			err := s.handleFailoverErrorResponsePassthrough(ctx, resp, c, account, body, responseBody)
			var failoverErr *UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				failoverErr.RawResponsePassthrough = true
			}
			return nil, err
		}
		return nil, s.handleCustomRawErrorResponse(ctx, resp, c, account, body, responseBody)
	}

	var usage OpenAIUsage
	responseID := ""
	clientDisconnected := false
	firstTokenMs := (*int)(nil)
	if stream {
		usage, responseID, firstTokenMs, clientDisconnected, err = s.forwardCustomStreamResponse(ctx, c, resp, startTime)
	} else {
		usage, responseID, err = s.forwardCustomBufferedResponse(ctx, c, resp)
	}
	if err != nil {
		return nil, err
	}
	// Only Responses IDs are valid HTTP continuation ownership keys. Anthropic
	// message IDs are still returned in the forwarding result for observability,
	// but must not be registered as OpenAI response bindings.
	if strings.HasPrefix(responseID, "resp_") {
		s.bindHTTPResponseAccount(ctx, c, account, responseID)
	}
	return &OpenAIForwardResult{
		RequestID:                     resp.Header.Get("x-request-id"),
		ResponseID:                    responseID,
		Usage:                         usage,
		Model:                         model,
		UpstreamModel:                 upstreamModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamEndpoint:              endpoint,
		ServiceTier:                   serviceTier,
		UpstreamServiceTier:           observedUpstreamServiceTier(c),
		ReasoningEffort:               reasoningEffort,
		Stream:                        stream,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ClientDisconnect:              clientDisconnected,
	}, nil
}

func (s *OpenAIGatewayService) handleCustomRawErrorResponse(
	ctx context.Context,
	resp *http.Response,
	c *gin.Context,
	account *Account,
	requestBody, responseBody []byte,
) error {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(responseBody), maxBytes)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, upstreamDetail)
	logOpenAIInstructionsRequiredDebug(ctx, c, account, resp.StatusCode, upstreamMsg, requestBody, responseBody)
	reqModel, _, _ := extractOpenAIRequestMetaFromBody(requestBody)
	canonicalModel := canonicalOpenAIAccountSchedulingModel(account, reqModel)
	_ = s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, responseBody, canonicalModel)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:             account.Platform,
		AccountID:            account.ID,
		AccountName:          account.Name,
		UpstreamStatusCode:   resp.StatusCode,
		UpstreamRequestID:    resp.Header.Get("x-request-id"),
		Passthrough:          true,
		Kind:                 "http_error",
		Message:              upstreamMsg,
		Detail:               upstreamDetail,
		UpstreamResponseBody: upstreamDetail,
	})
	s.WriteCustomRawUpstreamResponse(c, resp.StatusCode, resp.Header, responseBody)
	return fmt.Errorf("custom upstream error: %d", resp.StatusCode)
}

// WriteCustomRawUpstreamResponse preserves Custom's upstream HTTP contract
// while retaining the gateway response-header allowlist.
func (s *OpenAIGatewayService) WriteCustomRawUpstreamResponse(
	c *gin.Context,
	statusCode int,
	headers http.Header,
	body []byte,
) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return false
	}
	var filter *responseheaders.CompiledHeaderFilter
	if s != nil {
		filter = s.responseHeaderFilter
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), headers, filter)
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	MarkResponseCommitted(c)
	c.Data(statusCode, contentType, body)
	return true
}

func (s *OpenAIGatewayService) copyCustomClientHeaders(c *gin.Context, dst http.Header) {
	if c == nil || c.Request == nil || dst == nil {
		return
	}
	allowTimeout := s.isOpenAIPassthroughTimeoutHeadersAllowed()
	for key, values := range c.Request.Header {
		lower := strings.ToLower(strings.TrimSpace(key))
		if !isOpenAIPassthroughAllowedRequestHeader(lower, allowTimeout) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// copyCustomAnthropicHeaders preserves the protocol negotiation headers that
// Claude clients send to /v1/messages. They are intentionally separate from
// the OpenAI allowlist so an arbitrary Custom request cannot smuggle unrelated
// proxy or credential headers upstream.
func copyCustomAnthropicHeaders(c *gin.Context, dst http.Header, allowTimeoutHeaders bool) {
	if c == nil || c.Request == nil || dst == nil {
		return
	}
	for key, values := range c.Request.Header {
		lower := strings.ToLower(strings.TrimSpace(key))
		if !isCustomAnthropicPassthroughAllowedHeader(lower, allowTimeoutHeaders) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isCustomAnthropicPassthroughAllowedHeader(lowerKey string, allowTimeoutHeaders bool) bool {
	if lowerKey == "" {
		return false
	}
	if isOpenAIPassthroughTimeoutHeader(lowerKey) {
		return allowTimeoutHeaders
	}
	switch lowerKey {
	case "accept", "accept-language", "content-type", "user-agent",
		"anthropic-version", "anthropic-beta", "anthropic-dangerous-direct-browser-access",
		"x-app", "x-client-request-id", "x-claude-code-session-id",
		"x-stainless-retry-count", "x-stainless-lang", "x-stainless-package-version",
		"x-stainless-os", "x-stainless-arch", "x-stainless-runtime",
		"x-stainless-runtime-version", "x-stainless-helper-method":
		return true
	default:
		return false
	}
}

func endpointForCustomObserver(c *gin.Context) string {
	return GetActualOpenAIUpstreamEndpoint(c)
}

// extractCustomUsageFromJSONBytes accepts both OpenAI usage envelopes and the
// Anthropic Messages shapes emitted by native-compatible upstreams. Anthropic
// streaming usage is split between message_start.message.usage and
// message_delta.usage, so callers merge the returned partial values.
func extractCustomUsageFromJSONBytes(body []byte) (OpenAIUsage, bool) {
	if usage, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		return usage, true
	}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return OpenAIUsage{}, false
	}
	for _, path := range []string{"message.usage", "delta.usage", "data.usage"} {
		if usage, ok := openAIUsageFromGJSON(gjson.GetBytes(body, path)); ok {
			return usage, true
		}
	}
	return OpenAIUsage{}, false
}

func mergeCustomObservedUsage(base, next OpenAIUsage) OpenAIUsage {
	if next.InputTokens > 0 {
		base.InputTokens = next.InputTokens
	}
	if next.ImageInputTokens > 0 {
		base.ImageInputTokens = next.ImageInputTokens
	}
	if next.OutputTokens > 0 {
		base.OutputTokens = next.OutputTokens
	}
	if next.CacheCreationInputTokens > 0 {
		base.CacheCreationInputTokens = next.CacheCreationInputTokens
	}
	if next.CacheReadInputTokens > 0 {
		base.CacheReadInputTokens = next.CacheReadInputTokens
	}
	if next.ImageOutputTokens > 0 {
		base.ImageOutputTokens = next.ImageOutputTokens
	}
	return base
}

// extractCustomResponseIDFromJSONBytes observes canonical Responses and
// Anthropic message IDs while ignoring ordinary SSE event IDs.
func extractCustomResponseIDFromJSONBytes(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"response.id", "response_id", "message.id"} {
		if id := strings.TrimSpace(gjson.GetBytes(body, path).String()); id != "" {
			return id
		}
	}
	id := strings.TrimSpace(gjson.GetBytes(body, "id").String())
	if id == "" {
		return ""
	}
	// Anthropic uses msg_*; OpenAI uses resp_*. A provider-specific ID is
	// accepted for buffered responses, but event IDs (evt_*) are not promoted
	// to request ownership state.
	if strings.HasPrefix(id, "evt_") || strings.HasPrefix(id, "event_") {
		return ""
	}
	return id
}

func observeCustomSSEBody(observer *upstreamResponseModelObserver, body, endpoint string) {
	if observer == nil || strings.TrimSpace(body) == "" {
		return
	}
	forEachOpenAISSEDataPayload(body, func(payload []byte) {
		if strings.HasPrefix(endpoint, customMessagesEndpoint) {
			observer.ObserveAnthropic(payload)
			return
		}
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		observer.ObserveOpenAI(payload, eventType)
	})
}

// forwardCustomBufferedResponse writes the upstream body byte-for-byte. It
// only observes usage/model metadata for billing and operations.
func (s *OpenAIGatewayService) forwardCustomBufferedResponse(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
) (OpenAIUsage, string, error) {
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return OpenAIUsage{}, "", err
	}
	usage, _ := extractCustomUsageFromJSONBytes(body)
	responseID := extractCustomResponseIDFromJSONBytes(body)
	if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
		if bodyHasSSEFraming(body) {
			observeCustomSSEBody(observer, string(body), endpointForCustomObserver(c))
		} else {
			observer.ObserveAnthropic(body)
			observer.ObserveOpenAI(body, strings.TrimSpace(gjson.GetBytes(body, "type").String()))
		}
	}
	if c != nil {
		writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, body)
	}
	return usage, responseID, nil
}

// forwardCustomStreamResponse copies each read chunk unchanged while observing
// SSE usage and IDs. ReadBytes preserves line endings, unlike Scanner/Text.
func (s *OpenAIGatewayService) forwardCustomStreamResponse(
	ctx context.Context,
	c *gin.Context,
	resp *http.Response,
	startTime time.Time,
) (OpenAIUsage, string, *int, bool, error) {
	var usage OpenAIUsage
	responseID := ""
	var firstTokenMs *int
	clientDisconnected := false
	if c != nil {
		writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "text/event-stream"))
		c.Writer.WriteHeader(resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	flusher, _ := writerFlusher(c)
	for {
		chunk, readErr := reader.ReadBytes('\n')
		if len(chunk) > 0 {
			if payload, ok := extractOpenAISSEDataLine(strings.TrimRight(string(chunk), "\r\n")); ok {
				trimmed := strings.TrimSpace(payload)
				if trimmed != "" && trimmed != "[DONE]" {
					if parsed, ok := extractCustomUsageFromJSONBytes([]byte(trimmed)); ok {
						usage = mergeCustomObservedUsage(usage, parsed)
					} else if parsed := extractCCStreamUsage(trimmed); parsed != nil {
						usage = mergeCustomObservedUsage(usage, *parsed)
					}
					if responseID == "" {
						responseID = extractCustomResponseIDFromJSONBytes([]byte(trimmed))
					}
					if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
						if strings.HasPrefix(endpointForCustomObserver(c), customMessagesEndpoint) {
							observer.ObserveAnthropic([]byte(trimmed))
						} else {
							observer.ObserveOpenAI([]byte(trimmed), strings.TrimSpace(gjson.GetBytes([]byte(trimmed), "type").String()))
						}
					}
					if firstTokenMs == nil {
						firstTokenMsValue := int(time.Since(startTime).Milliseconds())
						firstTokenMs = &firstTokenMsValue
					}
				}
			}
			if c != nil && !clientDisconnected {
				if _, writeErr := c.Writer.Write(chunk); writeErr != nil {
					clientDisconnected = true
					logger.L().Debug("custom upstream client disconnected; draining response", zap.Error(writeErr))
				} else if flusher != nil {
					flusher.Flush()
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return usage, responseID, firstTokenMs, clientDisconnected, readErr
		}
	}
	return usage, responseID, firstTokenMs, clientDisconnected, nil
}

type customWriterFlusher interface {
	Flush()
}

func writerFlusher(c *gin.Context) (customWriterFlusher, bool) {
	if c == nil || c.Writer == nil {
		return nil, false
	}
	f, ok := c.Writer.(customWriterFlusher)
	return f, ok
}
