package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SeedanceTaskKey keeps Ark task ownership separate from the legacy Grok and
// OpenAI-compatible video task namespaces.
func SeedanceTaskKey(id string) string { return "seedance:" + strings.TrimSpace(id) }

// ParseSeedanceRequest validates the required Ark fields while extracting only
// the text/images needed by the existing moderation and routing pipeline.
// ForwardSeedance sends the original JSON back upstream, so unknown fields and
// the complete multimodal content array are preserved.
func ParseSeedanceRequest(body []byte) (GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return info, fmt.Errorf("request body must be a JSON object")
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return info, fmt.Errorf("model is required")
	}
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return info, fmt.Errorf("content must be a non-empty array")
	}
	info.Model = strings.TrimSpace(model.String())
	media := ParseGrokMediaRequest("application/json", body)
	info.DurationSeconds, info.RawDurationSeconds = media.DurationSeconds, media.RawDurationSeconds
	info.Resolution, info.ResolutionValid = media.Resolution, media.ResolutionValid
	info.N = 1
	var texts []string
	for _, item := range content.Array() {
		switch strings.TrimSpace(item.Get("type").String()) {
		case "text":
			if text := strings.TrimSpace(item.Get("text").String()); text != "" {
				texts = append(texts, text)
			}
		case "image_url":
			if imageURL := strings.TrimSpace(item.Get("image_url.url").String()); imageURL != "" {
				info.InputImageURLs = append(info.InputImageURLs, imageURL)
			}
		case "video_url":
			info.HasReferenceVideo = true
		}
	}
	info.Prompt = strings.Join(texts, "\n")
	return info, nil
}

func buildSeedanceURL(base string, endpoint GrokMediaEndpoint, taskID string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("seedance base URL is empty")
	}
	if !strings.HasSuffix(base, "/api/v3") && !strings.HasSuffix(base, "/v3") {
		base += "/api/v3"
	}
	base += "/contents/generations/tasks"
	if endpoint != SeedanceEndpointCreate {
		taskID = strings.TrimPrefix(strings.TrimSpace(taskID), "seedance:")
		if taskID == "" || validateUpstreamPathSegment("Seedance task ID", taskID) != nil {
			return "", fmt.Errorf("invalid seedance task ID")
		}
		base += "/" + taskID
	}
	return base, nil
}

// ForwardSeedance preserves the native Ark response and only rewrites model
// for an account-level model mapping. Async creation is buffered so the handler
// can bind the task before exposing success.
func (s *OpenAIGatewayService) ForwardSeedance(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint GrokMediaEndpoint,
	taskID string,
	body []byte,
) (*OpenAIForwardResult, error) {
	if account == nil || !endpoint.IsSeedance() {
		return nil, fmt.Errorf("seedance account and endpoint are required")
	}
	if account.Platform != PlatformOpenAI && account.Platform != PlatformCustom {
		return nil, fmt.Errorf("account platform %s is not supported for seedance", account.Platform)
	}
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) {
		return nil, fmt.Errorf("seedance is not enabled for this account")
	}
	baseURL := account.GetOpenAIBaseURL()
	validatedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	targetURL, err := buildSeedanceURL(validatedBaseURL, endpoint, taskID)
	if err != nil {
		return nil, err
	}

	method := endpoint.httpMethod()
	requestBody := []byte(nil)
	requestModel := ""
	upstreamModel := ""
	if endpoint == SeedanceEndpointCreate {
		info, parseErr := ParseSeedanceRequest(body)
		if parseErr != nil {
			return nil, parseErr
		}
		requestModel = info.Model
		upstreamModel = account.GetMappedModel(requestModel)
		requestBody, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, fmt.Errorf("rewrite seedance model: %w", err)
		}
	}

	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	var bodyReader *bytes.Reader
	if endpoint == SeedanceEndpointCreate {
		bodyReader = bytes.NewReader(requestBody)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	if endpoint == SeedanceEndpointCreate {
		upstreamReq.Header.Set("Content-Type", "application/json")
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	started := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	originalResponseBody := resp.Body
	defer func() { _ = originalResponseBody.Close() }()
	requestIDHeader := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		s.WriteCustomRawUpstreamResponse(c, resp.StatusCode, resp.Header, responseBody)
		return nil, fmt.Errorf("seedance upstream status %d", resp.StatusCode)
	}

	result := &OpenAIForwardResult{
		RequestID:       requestIDHeader,
		Model:           requestModel,
		BillingModel:    requestModel,
		UpstreamModel:   upstreamModel,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(started),
	}
	if endpoint == SeedanceEndpointCreate {
		id := strings.TrimSpace(gjson.GetBytes(responseBody, "id").String())
		if id == "" {
			return nil, fmt.Errorf("seedance create response missing task ID")
		}
		result.ResponseID = SeedanceTaskKey(id)
	} else {
		result.ResponseID = SeedanceTaskKey(strings.TrimPrefix(strings.TrimSpace(taskID), "seedance:"))
		result.UpstreamModel = strings.TrimSpace(gjson.GetBytes(responseBody, "model").String())
		if strings.EqualFold(strings.TrimSpace(gjson.GetBytes(responseBody, "status").String()), "succeeded") {
			result.Usage.OutputTokens = max(0, int(gjson.GetBytes(responseBody, "usage.completion_tokens").Int()))
		}
	}

	if endpoint == SeedanceEndpointCreate || endpoint == SeedanceEndpointStatus && c.GetBool("media_billing_buffer") {
		result.BufferedResponseStatus = resp.StatusCode
		result.BufferedResponseHeaders = resp.Header.Clone()
		result.BufferedResponseBody = append([]byte(nil), responseBody...)
		return result, nil
	}
	writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
	return result, nil
}
