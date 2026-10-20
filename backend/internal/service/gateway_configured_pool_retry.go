package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// explicitlyConfiguredPoolHTTPRetry only extends the fixed failover status list
// when an administrator has opted this pool account into that exact status.
// The existing handler owns the bounded same-account/next-account retry budget.
func explicitlyConfiguredPoolHTTPRetry(account *Account, status int) bool {
	if account == nil || !account.IsPoolMode() || account.GetPoolModeRetryCount() <= 0 || status < 400 || status > 599 {
		return false
	}
	if account.Platform != PlatformOpenAI && account.Platform != PlatformAnthropic && account.Platform != PlatformCustom {
		return false
	}
	return account.GetPoolModeRetryStatusCodes() != nil && account.IsPoolModeRetryableStatus(status)
}

func configuredPoolHTTPFailover(c *gin.Context, account *Account, resp *http.Response, body []byte, cfg *config.Config, passthrough bool, requestBodies ...[]byte) *UpstreamFailoverError {
	if resp == nil || !explicitlyConfiguredPoolHTTPRetry(account, resp.StatusCode) || c == nil || c.Request == nil || c.Writer == nil {
		return nil
	}
	ctx := c.Request.Context()
	if ctx.Err() != nil || ctx.Value(mediaWorkbenchPrepaymentKey{}) != nil || HasOpsClientBusinessLimited(c) || IsResponseCommitted(c) {
		return nil
	}
	// Limit the new policy to ordinary text API requests. Queued media operations
	// and their separately settled prepayments must not be submitted again here.
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	textRequest := false
	for _, suffix := range []string{"/messages", "/responses", "/responses/compact", "/chat/completions"} {
		textRequest = textRequest || strings.HasSuffix(path, suffix)
	}
	if c.Request.Method != http.MethodPost || !textRequest {
		return nil
	}
	if imageIntent, _ := getOpenAIImageIntentHint(c); imageIntent {
		return nil
	}
	// Responses also carries image tools. Inspect an already captured wire body
	// when available; never consume the inbound stream to decide whether to retry.
	for _, requestBody := range requestBodies {
		if IsImageGenerationIntent(path, "", requestBody) {
			return nil
		}
	}
	message := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if APIKeyFailoverModelUnavailable(resp.StatusCode, body) || isOpenAIContextWindowError(message, body) {
		return nil
	}
	if hit, _, _ := detectOpenAICyberPolicy(body); hit {
		return nil
	}
	markers := message
	for _, field := range []string{"error.code", "error.type", "response.error.code", "response.error.type", "code", "type"} {
		markers += " " + gjson.GetBytes(body, field).String()
	}
	markers = strings.ToLower(markers)
	for _, marker := range []string{"invalid_request", "policy", "safety", "moderation", "cyber", "violat", "not allowed", "prompt is too long", "prompt too long", "request too large"} {
		if strings.Contains(markers, marker) {
			return nil
		}
	}
	// Stop the concurrent compact heartbeat before inspecting the writer. Once
	// any SSE/response bytes have reached the client, the request is never replayed.
	if StopOpenAICompactSSEKeepaliveCommitted(c) || c.Writer.Written() {
		return nil
	}
	detail := ""
	if cfg != nil && cfg.Gateway.LogUpstreamErrorBody {
		limit := cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if limit <= 0 {
			limit = 2048
		}
		detail = truncateString(string(body), limit)
	}
	setOpsUpstreamError(c, resp.StatusCode, message, detail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: upstreamHTTPErrorRequestID(resp.Header),
		Kind: "failover", Reason: "configured_pool_http_status", Message: message, Detail: detail,
		Passthrough: passthrough,
	})
	return &UpstreamFailoverError{
		StatusCode: resp.StatusCode, ResponseBody: body, ResponseHeaders: resp.Header.Clone(),
		RetryableOnSameAccount: true,
	}
}

func upstreamHTTPErrorRequestID(headers http.Header) string {
	if requestID := strings.TrimSpace(headers.Get("x-request-id")); requestID != "" {
		return requestID
	}
	return strings.TrimSpace(headers.Get("request-id"))
}
