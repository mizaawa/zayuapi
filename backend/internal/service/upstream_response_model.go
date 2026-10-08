package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// responseModelAuditBypassOn reports whether client-visible model fields should
// be restored to the model requested by the downstream client. Services built
// without a SettingService keep the historical rewrite behavior used by small
// compatibility instances and unit tests.
func responseModelAuditBypassOn(ctx context.Context, settingService *SettingService) bool {
	if settingService == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return settingService.ResponseModelAuditBypassEnabled(ctx)
}

func downstreamResponseModel(ctx context.Context, settingService *SettingService, requestedModel, upstreamModel string) string {
	requestedModel = downstreamRequestedModel(ctx, requestedModel)
	if responseModelAuditBypassOn(ctx, settingService) || strings.TrimSpace(upstreamModel) == "" {
		return requestedModel
	}
	return strings.TrimSpace(upstreamModel)
}

// downstreamResponseModelSeed is used by streaming protocol converters. An
// empty seed lets the converter adopt the model declared by the first upstream
// event; a requested-model seed intentionally masks that declaration.
func downstreamResponseModelSeed(ctx context.Context, settingService *SettingService, requestedModel string) string {
	if responseModelAuditBypassOn(ctx, settingService) {
		return downstreamRequestedModel(ctx, requestedModel)
	}
	return ""
}

func ginRequestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// anthropicResponseModel returns the model declared by an Anthropic response
// or message_start event without changing the payload.
func anthropicResponseModel(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	return firstTrimmedGJSONModel(
		gjson.GetBytes(body, "message.model"),
		gjson.GetBytes(body, "model"),
	)
}

// rewriteAnthropicResponseModel changes only client-visible Anthropic model
// fields. For valid message responses it also supplies the model when an
// upstream-compatible implementation omitted it.
func rewriteAnthropicResponseModel(body []byte, model string) []byte {
	model = strings.TrimSpace(model)
	if len(body) == 0 || model == "" || !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return body
	}

	updated := body
	message := gjson.GetBytes(body, "message")
	if message.IsObject() {
		current := gjson.GetBytes(body, "message.model")
		if !current.Exists() || current.String() != model {
			if next, err := sjson.SetBytes(updated, "message.model", model); err == nil {
				updated = next
			}
		}
	}

	topLevelModel := gjson.GetBytes(body, "model")
	responseType := strings.TrimSpace(gjson.GetBytes(body, "type").String())
	if topLevelModel.Exists() || (!message.IsObject() && (responseType == "" || responseType == "message")) {
		if !topLevelModel.Exists() || topLevelModel.String() != model {
			if next, err := sjson.SetBytes(updated, "model", model); err == nil {
				updated = next
			}
		}
	}
	return updated
}

// rewriteGeminiResponseModel changes only client-visible Gemini modelVersion
// fields. Callers observe the original payload before invoking this helper so
// internal routing and audit data continue to reflect the upstream model.
func rewriteGeminiResponseModel(body []byte, model string) []byte {
	if len(body) == 0 || strings.TrimSpace(model) == "" {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	changed := false
	if _, ok := payload["modelVersion"]; ok {
		payload["modelVersion"] = model
		changed = true
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if _, exists := response["modelVersion"]; exists {
			response["modelVersion"] = model
			changed = true
		}
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

const (
	upstreamResponseModelObserverContextKey = "upstream_response_model_observer"
	upstreamResponseModelMaxLength          = 200
)

// upstreamResponseModelObserver tracks one forwarding attempt (or one WS turn).
// A terminal declaration wins over an earlier declaration; otherwise the first
// declaration is retained. Conflicts are diagnostic only and never affect the
// forwarding or billing path.
type upstreamResponseModelObserver struct {
	first       string
	terminal    string
	conflict    bool
	serviceTier *string
}

func (o *upstreamResponseModelObserver) Observe(model string, terminal bool) {
	model = normalizeObservedUpstreamResponseModel(model)
	if model == "" {
		return
	}
	current := o.Model()
	if current != "" && !strings.EqualFold(current, model) {
		o.conflict = true
	}
	if terminal {
		o.terminal = model
		return
	}
	if o.first == "" {
		o.first = model
	}
}

func normalizeObservedUpstreamResponseModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	runes := []rune(model)
	if len(runes) > upstreamResponseModelMaxLength {
		model = string(runes[:upstreamResponseModelMaxLength])
	}
	return model
}

func (o *upstreamResponseModelObserver) ObserveOpenAI(payload []byte, eventType string) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return
	}
	model := firstTrimmedGJSONModel(
		gjson.GetBytes(payload, "response.model"),
		gjson.GetBytes(payload, "model"),
	)
	o.Observe(model, isUpstreamResponseModelTerminalEvent(eventType))
	// Created/in-progress Responses events can still describe requested intent.
	// Only terminal responses or Chat Completions chunks can inform billing downgrades.
	if eventType == "" || isUpstreamResponseModelTerminalEvent(eventType) {
		response := gjson.GetBytes(payload, "response")
		if response.IsObject() {
			o.serviceTier = extractOpenAIServiceTierFromBody([]byte(response.Raw))
		} else if tier := extractOpenAIServiceTierFromBody(payload); tier != nil {
			o.serviceTier = tier
		}
	}
}

func (o *upstreamResponseModelObserver) ServiceTier() *string {
	if o == nil {
		return nil
	}
	return o.serviceTier
}

func observedUpstreamServiceTier(c *gin.Context) *string {
	return upstreamResponseModelObserverFromContext(c).ServiceTier()
}

func (o *upstreamResponseModelObserver) ObserveAnthropic(payload []byte) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return
	}
	model := firstTrimmedGJSONModel(
		gjson.GetBytes(payload, "message.model"),
		gjson.GetBytes(payload, "model"),
	)
	o.Observe(model, false)
}

func (o *upstreamResponseModelObserver) ObserveGemini(payload []byte) {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return
	}
	model := firstTrimmedGJSONModel(
		gjson.GetBytes(payload, "modelVersion"),
		gjson.GetBytes(payload, "response.modelVersion"),
	)
	// Gemini streaming has no universal terminal event carrying modelVersion;
	// treating each declaration as terminal retains the latest chunk.
	o.Observe(model, true)
}

func (o *upstreamResponseModelObserver) Model() string {
	if o == nil {
		return ""
	}
	if o.terminal != "" {
		return o.terminal
	}
	return o.first
}

func (o *upstreamResponseModelObserver) Conflict() bool {
	return o != nil && o.conflict
}

func beginUpstreamResponseModelObservation(c *gin.Context) *upstreamResponseModelObserver {
	observer := &upstreamResponseModelObserver{}
	if c != nil {
		c.Set(upstreamResponseModelObserverContextKey, observer)
	}
	return observer
}

func upstreamResponseModelObserverFromContext(c *gin.Context) *upstreamResponseModelObserver {
	if c == nil {
		return nil
	}
	value, ok := c.Get(upstreamResponseModelObserverContextKey)
	if !ok {
		return nil
	}
	observer, _ := value.(*upstreamResponseModelObserver)
	return observer
}

func observedUpstreamResponseModel(c *gin.Context) string {
	return upstreamResponseModelObserverFromContext(c).Model()
}

func observedUpstreamResponseModelConflict(c *gin.Context) bool {
	return upstreamResponseModelObserverFromContext(c).Conflict()
}

func observeOpenAISSEBody(observer *upstreamResponseModelObserver, body string) {
	if observer == nil || strings.TrimSpace(body) == "" {
		return
	}
	forEachOpenAISSEDataPayload(body, func(payload []byte) {
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		observer.ObserveOpenAI(payload, eventType)
	})
}

func firstTrimmedGJSONModel(values ...gjson.Result) string {
	for _, value := range values {
		if !value.Exists() || value.Type != gjson.String {
			continue
		}
		if model := strings.TrimSpace(value.String()); model != "" {
			return model
		}
	}
	return ""
}

func isUpstreamResponseModelTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func upstreamModelMismatch(sentModel, responseModel string) *bool {
	responseModel = strings.TrimSpace(responseModel)
	if responseModel == "" {
		return nil
	}
	sentModel = strings.TrimSpace(sentModel)
	mismatch := sentModel == "" || !strings.EqualFold(sentModel, responseModel)
	return &mismatch
}

func upstreamSentModel(requestedModel, upstreamModel string) string {
	sentModel := strings.TrimSpace(upstreamModel)
	if sentModel == "" {
		sentModel = strings.TrimSpace(requestedModel)
	}
	return sentModel
}
