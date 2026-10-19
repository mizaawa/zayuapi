package service

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// These limits are extracted from the bundled model catalog, rather than
// guessed from model families. They bound server-side history and multimodal
// expansion, and OAuth routes which do not support a requested output limit.
//
//go:embed token_admission_model_limits.json
var tokenAdmissionModelLimitsJSON []byte

type tokenAdmissionModelLimit struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

var tokenAdmissionModelLimits = func() map[string]tokenAdmissionModelLimit {
	limits := map[string]tokenAdmissionModelLimit{}
	if err := json.Unmarshal(tokenAdmissionModelLimitsJSON, &limits); err != nil {
		panic(err)
	}
	return limits
}()

var ErrTokenAdmissionUnbounded = errors.New("request has no reliable token cost bound")

type TokenAdmissionProtocol string

const (
	TokenAdmissionResponses  TokenAdmissionProtocol = "responses"
	TokenAdmissionChat       TokenAdmissionProtocol = "chat"
	TokenAdmissionMessages   TokenAdmissionProtocol = "messages"
	TokenAdmissionEmbeddings TokenAdmissionProtocol = "embeddings"
	TokenAdmissionGemini     TokenAdmissionProtocol = "gemini"
)

// TokenAdmissionInput is a request/account pricing snapshot. Quote methods
// deliberately call the normal RecordUsage pricing path in dry-run mode.
type TokenAdmissionInput struct {
	Body         []byte
	Protocol     TokenAdmissionProtocol
	Model        string
	MappedModel  string
	APIKey       *APIKey
	Account      *Account
	Subscription *UserSubscription
	PricingAt    time.Time
	ChannelUsageFields
	LongContextThreshold  int
	LongContextMultiplier float64
	// WebSocket histories can also be populated by conversation.item.create.
	ServerHistory bool
}

type tokenAdmissionBound struct {
	body                      []byte
	input, output, imageCount int
	model                     string
	imageModel                string
	tier                      *string
}

func uniqueTokenAdmissionJSON(dec *json.Decoder) bool {
	tok, err := dec.Token()
	if err != nil {
		return false
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
			if !uniqueTokenAdmissionJSON(dec) {
				return false
			}
		}
		end, err := dec.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for dec.More() {
			if !uniqueTokenAdmissionJSON(dec) {
				return false
			}
		}
		end, err := dec.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func tokenAdmissionPositiveInt(root []byte, field string, maximum int) (int, bool, error) {
	v := gjson.GetBytes(root, field)
	if !v.Exists() {
		return 0, false, nil
	}
	if v.Type != gjson.Number || v.Num < 1 || v.Num > float64(maximum) || math.Trunc(v.Num) != v.Num {
		return 0, true, fmt.Errorf("%w: %s must be a positive bounded integer", ErrTokenAdmissionUnbounded, field)
	}
	return int(v.Num), true, nil
}

// Function/custom/namespace tools are local declarations and are fully counted
// in the JSON byte bound. Provider tools and media/history references may add
// input which is not present in the request; quote the whole catalog context.
func tokenAdmissionHasServerInput(v gjson.Result) bool {
	if v.IsArray() {
		for _, child := range v.Array() {
			if tokenAdmissionHasServerInput(child) {
				return true
			}
		}
		return false
	}
	if !v.IsObject() {
		return false
	}
	opaque := false
	v.ForEach(func(key, child gjson.Result) bool {
		switch key.String() {
		case "previous_response_id", "conversation", "cachedContent", "cached_content", "file_id", "file_uri", "fileUri", "image_url", "input_image", "inlineData", "inline_data", "fileData", "file_data", "input_audio", "audio", "video", "document", "source":
			if child.Type != gjson.Null && child.Raw != `""` {
				opaque = true
			}
		case "type":
			switch child.String() {
			case "web_search", "web_search_preview", "file_search", "code_interpreter", "computer_use", "computer_use_preview", "mcp", "image_generation", "input_image", "input_file", "input_audio", "image", "audio", "video", "document":
				opaque = true
			}
		}
		if !opaque && tokenAdmissionHasServerInput(child) {
			opaque = true
		}
		return !opaque
	})
	return opaque
}

func tokenAdmissionCatalogLimit(model string) (tokenAdmissionModelLimit, bool) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if limit, ok := tokenAdmissionModelLimits[model]; ok {
		return limit, true
	}
	// Provider prefixes are routing labels; strip only those explicitly used
	// by the built-in pricing catalog, never fuzzy-match an unknown model.
	for _, prefix := range []string{"openai/", "anthropic/", "google/", "google_ai_studio/", "vertex_ai/", "xai/"} {
		if strings.HasPrefix(model, prefix) {
			limit, ok := tokenAdmissionModelLimits[strings.TrimPrefix(model, prefix)]
			return limit, ok
		}
	}
	return tokenAdmissionModelLimit{}, false
}

func tokenAdmissionImageTool(v gjson.Result) bool {
	if v.IsArray() {
		for _, child := range v.Array() {
			if tokenAdmissionImageTool(child) {
				return true
			}
		}
	}
	if v.IsObject() {
		if v.Get("type").String() == "image_generation" {
			return true
		}
		found := false
		v.ForEach(func(_, child gjson.Result) bool { found = tokenAdmissionImageTool(child); return !found })
		return found
	}
	return false
}

func buildTokenAdmissionBound(input TokenAdmissionInput) (tokenAdmissionBound, error) {
	b := tokenAdmissionBound{body: input.Body}
	if input.APIKey == nil || input.APIKey.User == nil || input.Account == nil {
		return b, ErrTokenBillingInvalidQuote
	}
	dec := json.NewDecoder(bytes.NewReader(input.Body))
	dec.UseNumber()
	if !gjson.ParseBytes(input.Body).IsObject() || !uniqueTokenAdmissionJSON(dec) {
		return b, fmt.Errorf("%w: ambiguous JSON", ErrTokenAdmissionUnbounded)
	}
	if _, err := dec.Token(); err != io.EOF {
		return b, ErrTokenAdmissionUnbounded
	}
	b.model = strings.TrimSpace(input.MappedModel)
	if b.model == "" {
		b.model = strings.TrimSpace(gjson.GetBytes(input.Body, "model").String())
	}
	if b.model == "" {
		b.model = input.Model
	}
	b.model = input.Account.GetMappedModel(b.model)
	limit, known := tokenAdmissionCatalogLimit(b.model)
	n := 1
	for _, field := range []string{"n", "best_of", "generationConfig.candidateCount", "generation_config.candidate_count"} {
		value, exists, err := tokenAdmissionPositiveInt(input.Body, field, 128)
		if err != nil {
			return b, err
		}
		if exists && value > n {
			n = value
		}
	}
	output := 0
	for _, field := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens", "generationConfig.maxOutputTokens", "generation_config.max_output_tokens"} {
		value, exists, err := tokenAdmissionPositiveInt(input.Body, field, maxReportedUsageTokens)
		if err != nil {
			return b, err
		}
		if exists && value > output {
			output = value
		}
	}
	if input.Protocol != TokenAdmissionEmbeddings {
		if output == 0 {
			output = 4096
		}
		field := "max_output_tokens"
		switch input.Protocol {
		case TokenAdmissionChat:
			field = "max_completion_tokens"
			// The Chat endpoint also accepts Cursor's Responses-shaped body.
			// That branch forwards input directly to /responses, so its cap
			// must use the Responses field rather than an ignored Chat field.
			if !gjson.GetBytes(b.body, "messages").Exists() && gjson.GetBytes(b.body, "input").Exists() {
				field = "max_output_tokens"
			}
		case TokenAdmissionMessages:
			field = "max_tokens"
		case TokenAdmissionGemini:
			field = "generationConfig.maxOutputTokens"
		}
		var err error
		b.body, err = sjson.SetBytes(b.body, field, output)
		if err != nil {
			return b, err
		}
	}
	// OAuth's Codex transform removes these unsupported limits. API-key
	// requests retain a canonical bound through every HTTP retry instead.
	if input.Protocol != TokenAdmissionEmbeddings && input.Account.Platform == PlatformOpenAI && input.Account.Type == AccountTypeOAuth {
		if !known {
			return b, fmt.Errorf("%w: unknown OAuth native output limit for %s", ErrTokenAdmissionUnbounded, b.model)
		}
		if limit.Output > output {
			output = limit.Output
		}
	}
	// Existing protocol bridges floor small limits at128. Budget-error
	// retries cannot increase the output cap once a reservation exists.
	if (input.Protocol == TokenAdmissionChat || input.Protocol == TokenAdmissionMessages) && output < 128 {
		output = 128
	}
	if input.Protocol == TokenAdmissionMessages && input.Account.Platform == PlatformAntigravity && input.Account.Type != AccountTypeUpstream {
		// Reuse the actual transformer for its thinking-budget adjustment. A
		// scalar-only request keeps thinking enabled (the conservative case),
		// without transforming messages or injecting prompts twice.
		var request antigravity.ClaudeRequest
		if err := json.Unmarshal(b.body, &request); err != nil {
			return b, fmt.Errorf("%w: invalid thinking configuration", ErrTokenAdmissionUnbounded)
		}
		request.MaxTokens = output
		request.Messages = nil
		request.System = nil
		thinkingEnabled := request.Thinking != nil && (request.Thinking.Type == "enabled" || request.Thinking.Type == "adaptive")
		mappedModel := applyThinkingModelSuffix(b.model, thinkingEnabled)
		transformed, err := antigravity.TransformClaudeToGeminiWithOptions(&request, "", mappedModel, antigravity.TransformOptions{})
		if err != nil {
			return b, fmt.Errorf("%w: %v", ErrTokenAdmissionUnbounded, err)
		}
		if adjusted := int(gjson.GetBytes(transformed, "request.generationConfig.maxOutputTokens").Int()); adjusted > output {
			output = adjusted
		}
	}
	if output > maxReportedUsageTokens/n {
		return b, ErrTokenAdmissionUnbounded
	}
	b.output = output * n
	if isOpenAIImageGenerationModel(b.model) || isImageGenerationModel(b.model) {
		b.imageCount = n
	}
	if tokenAdmissionImageTool(gjson.GetBytes(b.body, "tools")) {
		calls, exists, err := tokenAdmissionPositiveInt(b.body, "max_tool_calls", 128)
		if err != nil {
			return b, err
		}
		if !exists {
			if input.Account.Platform == PlatformCustom {
				// Transparent Custom payloads cannot acquire an injected tool cap.
				// Reserve the billing engine's accepted count envelope instead.
				calls = maxBillableRequestCount / n
			} else {
				calls = 8
				b.body, err = sjson.SetBytes(b.body, "max_tool_calls", calls)
				if err != nil {
					return b, err
				}
			}
		}
		b.imageCount = calls * n
		imageConfig, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(b.body, b.model)
		if err != nil {
			return b, err
		}
		b.imageModel = imageConfig.Model
	}
	customImage := b.imageCount > 0 && input.Account.Platform == PlatformCustom
	if customImage {
		// Preserve every byte of Custom image requests. Count-based image prices
		// stay exact; token-based overrides must authorize the full accepted
		// usage envelope because these models have no audited native token cap.
		b.body = input.Body
		b.output = maxReportedUsageTokens
	}
	// Four tokens per UTF-8 JSON byte plus framing is intentionally above the
	// byte-tokenizer upper bound, and includes tool schemas, all turns, function
	// outputs, metadata, and the API key's configured system prompt.
	size := len(b.body) + len(input.APIKey.CustomSystemPrompt)
	if size > (maxReportedUsageTokens-8192)/4 {
		return b, ErrTokenAdmissionUnbounded
	}
	b.input = size*4 + 8192
	if customImage {
		b.input = maxReportedUsageTokens
	} else if input.ServerHistory || tokenAdmissionHasServerInput(gjson.ParseBytes(b.body)) {
		if !known || limit.Input <= 0 || limit.Input > maxReportedUsageTokens {
			return b, fmt.Errorf("%w: unknown context limit for %s", ErrTokenAdmissionUnbounded, b.model)
		}
		if limit.Input > b.input {
			b.input = limit.Input
		}
	}
	if tier := strings.TrimSpace(gjson.GetBytes(b.body, "service_tier").String()); tier != "" {
		b.tier = &tier
	}
	return b, nil
}

func tokenAdmissionImageSizes(b tokenAdmissionBound) []string {
	if b.imageCount == 0 {
		return []string{""}
	}
	// Configured image tiers need not be monotonic in size.
	return []string{"1K", "2K", "4K"}
}

func maxTokenAdmissionQuote(current, candidate *TokenBillingQuote) (*TokenBillingQuote, error) {
	if candidate == nil || candidate.Command.BalanceCost < 0 || math.IsNaN(candidate.Command.BalanceCost) || math.IsInf(candidate.Command.BalanceCost, 0) {
		return nil, ErrTokenBillingInvalidQuote
	}
	if candidate.Command.BalanceCost == 0 && current != nil {
		return current, nil
	}
	if current == nil || candidate.Command.BalanceCost > current.Command.BalanceCost {
		return candidate, nil
	}
	return current, nil
}

// Administrators can configure a lower rate above an interval boundary.
// Pricing only the largest token count would then be an underestimate. Every
// end point of the piecewise-linear settlement price is included in the quote.
func tokenAdmissionInputSamples(ctx context.Context, resolver *ModelPricingResolver, input TokenAdmissionInput, b tokenAdmissionBound) []int {
	counts := map[int]bool{b.input: true, 1: true}
	add := func(n int) {
		if n > 0 && n <= b.input {
			counts[n] = true
		}
		if n >= 0 && n < b.input {
			counts[n+1] = true
		}
	}
	add(input.LongContextThreshold)
	add(openAIGPT54LongContextInputThreshold)
	if resolver != nil {
		for _, model := range []string{b.model, input.Model, input.MappedModel, input.OriginalModel, input.ChannelMappedModel} {
			if model == "" {
				continue
			}
			p := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: input.APIKey.GroupID, Group: input.APIKey.Group})
			if p == nil {
				continue
			}
			if p.BasePricing != nil {
				add(p.BasePricing.LongContextInputThreshold)
			}
			for _, iv := range p.Intervals {
				add(iv.MinTokens)
				if iv.MaxTokens != nil {
					add(*iv.MaxTokens)
				}
			}
		}
	}
	out := make([]int, 0, len(counts))
	for n := range counts {
		out = append(out, n)
	}
	return out
}

func (s *OpenAIGatewayService) QuoteTokenAdmission(ctx context.Context, input TokenAdmissionInput) ([]byte, *TokenBillingQuote, error) {
	if tokenBillingExempt(s.cfg, input.APIKey, input.Subscription) {
		return input.Body, nil, nil
	}
	b, err := buildTokenAdmissionBound(input)
	if err != nil {
		return nil, nil, err
	}
	var maximum *TokenBillingQuote
	for _, inputTokens := range tokenAdmissionInputSamples(ctx, s.resolver, input, b) {
		// Cache categories have different prices. Evaluate each corner through the
		// settlement path instead of assuming uncached input is the highest price.
		for _, bucket := range []string{"input", "write", "read", "image"} {
			usage := OpenAIUsage{InputTokens: inputTokens, OutputTokens: b.output}
			switch bucket {
			case "write":
				usage.CacheCreationInputTokens = inputTokens
			case "read":
				usage.CacheReadInputTokens = inputTokens
			case "image":
				usage.ImageInputTokens = inputTokens
			}
			var quote *TokenBillingQuote
			if b.imageCount > 0 {
				// No catalog-native image token bound is available. Cover every
				// image token quantity the settlement validator can accept, rather
				// than invent a provider limit. Per-image prices ignore this bucket.
				usage.ImageOutputTokens = maxReportedUsageTokens
			}
			for _, tier := range []*string{nil, optionalTrimmedStringPtr("priority"), b.tier} {
				for _, size := range tokenAdmissionImageSizes(b) {
					result := &OpenAIForwardResult{Model: input.Model, UpstreamModel: b.model, BillingModel: b.imageModel, Usage: usage, ServiceTier: tier, ImageCount: b.imageCount, ImageSize: size}
					err = s.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: input.APIKey, User: input.APIKey.User, Account: input.Account, Subscription: input.Subscription, PricingAt: input.PricingAt, ChannelUsageFields: input.ChannelUsageFields, TokenQuote: &quote})
					if err != nil {
						return nil, nil, err
					}
					maximum, err = maxTokenAdmissionQuote(maximum, quote)
					if err != nil {
						return nil, nil, err
					}
				}
			}
		}
	}
	if maximum == nil {
		return nil, nil, ErrTokenBillingInvalidQuote
	}
	return b.body, maximum, nil
}

func (s *GatewayService) QuoteTokenAdmission(ctx context.Context, input TokenAdmissionInput) ([]byte, *TokenBillingQuote, error) {
	if tokenBillingExempt(s.cfg, input.APIKey, input.Subscription) {
		return input.Body, nil, nil
	}
	b, err := buildTokenAdmissionBound(input)
	if err != nil {
		return nil, nil, err
	}
	var maximum *TokenBillingQuote
	for _, inputTokens := range tokenAdmissionInputSamples(ctx, s.resolver, input, b) {
		for _, bucket := range []string{"input", "write5m", "write1h", "read"} {
			usage := ClaudeUsage{OutputTokens: b.output}
			switch bucket {
			case "input":
				usage.InputTokens = inputTokens
			case "write5m":
				usage.CacheCreationInputTokens = inputTokens
				usage.CacheCreation5mTokens = inputTokens
			case "write1h":
				usage.CacheCreationInputTokens = inputTokens
				usage.CacheCreation1hTokens = inputTokens
			case "read":
				usage.CacheReadInputTokens = inputTokens
			}
			var quote *TokenBillingQuote
			if b.imageCount > 0 {
				usage.ImageOutputTokens = maxReportedUsageTokens
			}
			for _, size := range tokenAdmissionImageSizes(b) {
				result := &ForwardResult{Model: input.Model, UpstreamModel: b.model, Usage: usage, ImageCount: b.imageCount, ImageSize: size}
				if input.LongContextThreshold > 0 {
					err = s.RecordUsageWithLongContext(ctx, &RecordUsageLongContextInput{Result: result, APIKey: input.APIKey, User: input.APIKey.User, Account: input.Account, Subscription: input.Subscription, PricingAt: input.PricingAt, ChannelUsageFields: input.ChannelUsageFields, LongContextThreshold: input.LongContextThreshold, LongContextMultiplier: input.LongContextMultiplier, TokenQuote: &quote})
				} else {
					err = s.RecordUsage(ctx, &RecordUsageInput{Result: result, APIKey: input.APIKey, User: input.APIKey.User, Account: input.Account, Subscription: input.Subscription, PricingAt: input.PricingAt, ChannelUsageFields: input.ChannelUsageFields, TokenQuote: &quote})
				}
				if err != nil {
					return nil, nil, err
				}
				maximum, err = maxTokenAdmissionQuote(maximum, quote)
				if err != nil {
					return nil, nil, err
				}
			}
		}
	}
	if maximum == nil {
		return nil, nil, ErrTokenBillingInvalidQuote
	}
	return b.body, maximum, nil
}

// Reported provider usage belongs to the admitted attempt even when a buffer
// prevented client delivery. Cyber receipts use their dedicated settlement path.
func tokenAdmissionRetainsOpenAIUsage(c *gin.Context, usage *OpenAIUsage) bool {
	return c != nil && c.Request != nil && TokenBillingReservationFromContext(c.Request.Context()) != nil &&
		usage != nil && usage.HasTokens() && GetOpsCyberPolicy(c) == nil
}
