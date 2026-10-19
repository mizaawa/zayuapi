package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func tokenAdmissionFixture() TokenAdmissionInput {
	groupID := int64(3)
	return TokenAdmissionInput{
		Model: "gpt-5.4", Protocol: TokenAdmissionResponses, Body: []byte(`{"model":"gpt-5.4","input":"hello"}`),
		APIKey:  &APIKey{ID: 1, UserID: 2, User: &User{ID: 2}, GroupID: &groupID, Group: &Group{ID: 3, Platform: PlatformOpenAI, RateMultiplier: 1}},
		Account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
	}
}

func TestTokenAdmissionFiniteBoundsForEveryProtocol(t *testing.T) {
	for _, tc := range []struct {
		protocol TokenAdmissionProtocol
		field    string
	}{
		{TokenAdmissionResponses, "max_output_tokens"}, {TokenAdmissionChat, "max_completion_tokens"},
		{TokenAdmissionMessages, "max_tokens"}, {TokenAdmissionGemini, "generationConfig.maxOutputTokens"},
	} {
		t.Run(string(tc.protocol), func(t *testing.T) {
			input := tokenAdmissionFixture()
			input.Protocol = tc.protocol
			if tc.protocol == TokenAdmissionChat {
				input.Body = []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`)
			}
			b, err := buildTokenAdmissionBound(input)
			require.NoError(t, err)
			require.Equal(t, int64(4096), gjson.GetBytes(b.body, tc.field).Int())
			require.Equal(t, 4096, b.output)
			require.Greater(t, b.input, len(input.Body))
		})
	}
	input := tokenAdmissionFixture()
	input.Protocol = TokenAdmissionEmbeddings
	b, err := buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Zero(t, b.output)
	require.JSONEq(t, string(input.Body), string(b.body))
}

func TestTokenAdmissionNAndFullToolPayload(t *testing.T) {
	input := tokenAdmissionFixture()
	input.Account.Platform = PlatformCustom
	input.Body = []byte(`{"model":"gpt-5.4","max_output_tokens":7,"n":3,"input":[{"type":"function_call_output","call_id":"call_1","output":"` + strings.Repeat("payload", 300) + `"}],"tools":[{"type":"function","name":"local","parameters":{"type":"object"}}]}`)
	b, err := buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Equal(t, 21, b.output)
	require.GreaterOrEqual(t, b.input, len(input.Body)*4)
	input.Body = []byte(`{"model":"vendor-unknown","max_output_tokens":7,"tools":[{"type":"function","name":"local"}],"input":"hello"}`)
	_, err = buildTokenAdmissionBound(input)
	require.NoError(t, err, "ordinary local tools have a byte-counted bound")
}

func TestTokenAdmissionRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.4","n":0}`, `{"model":"gpt-5.4","n":1.5}`, `{"model":"gpt-5.4","n":"3"}`,
		`{"model":"gpt-5.4","max_tokens":-1}`, `{"model":"gpt-5.4","max_tokens":null}`,
		`{"model":"gpt-5.4","max_output_tokens":100000001}`, `{"model":"gpt-5.4","n":129}`,
		`{"model":"gpt-5.4","max_tokens":1,"max_tokens":10000000}`,
		`{"model":"unknown","previous_response_id":"resp_hidden"}`, `{"model":"unknown","input":[{"type":"input_image","image_url":"https://example.invalid/image"}]}`,
	} {
		t.Run(fmt.Sprintf("%s", body), func(t *testing.T) {
			input := tokenAdmissionFixture()
			input.Body = []byte(body)
			_, err := buildTokenAdmissionBound(input)
			require.ErrorIs(t, err, ErrTokenAdmissionUnbounded)
		})
	}
}

func TestTokenAdmissionRetainsKnownMultimodalAndHistory(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5.4","previous_response_id":"resp_hidden","input":"next"}`,
		`{"model":"gpt-5.4","input":[{"type":"input_image","image_url":"https://example.invalid/image"}]}`,
		`{"model":"gpt-5.4","tools":[{"type":"web_search"}],"input":"search"}`,
	} {
		input := tokenAdmissionFixture()
		input.Body = []byte(body)
		b, err := buildTokenAdmissionBound(input)
		require.NoError(t, err)
		require.GreaterOrEqual(t, b.input, 1050000)
	}
}

func TestTokenAdmissionQuotesUseSettlementAndIntervalMaximum(t *testing.T) {
	input := tokenAdmissionFixture()
	input.Account.Platform = PlatformCustom
	input.Model = "vendor-bounded"
	input.APIKey.Group.Platform = PlatformCustom
	input.Body = []byte(`{"model":"vendor-bounded","max_output_tokens":10,"input":"hello","n":3}`)
	cheap, expensive, output, cache := 0.000001, 0.1, 0.00001, 0.2
	boundary := 100
	input.APIKey.Group.ModelPricing = []ChannelModelPricing{{Models: []string{"vendor-bounded"}, BillingMode: BillingModeToken, InputPrice: &cheap, OutputPrice: &output, CacheWritePrice: &cache, CacheReadPrice: &cheap, Intervals: []PricingInterval{
		{MinTokens: 0, MaxTokens: &boundary, InputPrice: &expensive, OutputPrice: &output, CacheWritePrice: &cache},
		{MinTokens: 100, InputPrice: &cheap, OutputPrice: &output, CacheWritePrice: &cheap},
	}}}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	resolver := NewModelPricingResolver(nil, billing)
	s := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: resolver}
	_, quote, err := s.QuoteTokenAdmission(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, quote)
	require.GreaterOrEqual(t, quote.Command.BalanceCost, 20.0, "100 expensive cache-write tokens outrank the much larger cheap upper bound")
	for _, tokens := range []int{1, 99, 100, 101, 300, 8000} {
		for _, bucket := range []string{"input", "write", "read"} {
			usage := OpenAIUsage{InputTokens: tokens, OutputTokens: 30}
			if bucket == "write" {
				usage.CacheCreationInputTokens = tokens
			}
			if bucket == "read" {
				usage.CacheReadInputTokens = tokens
			}
			var actual *TokenBillingQuote
			err = s.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: input.Model, UpstreamModel: input.Model, Usage: usage}, APIKey: input.APIKey, User: input.APIKey.User, Account: input.Account, TokenQuote: &actual})
			require.NoError(t, err)
			require.LessOrEqual(t, actual.Command.BalanceCost, quote.Command.BalanceCost)
		}
	}
}

func TestTokenAdmissionExemptModesPreservePayload(t *testing.T) {
	for _, mode := range []string{"simple", "managed", "subscription"} {
		input := tokenAdmissionFixture()
		input.Body = []byte(`{"provider_extension":"opaque"}`)
		cfg := &config.Config{}
		switch mode {
		case "simple":
			cfg.RunMode = config.RunModeSimple
		case "managed":
			input.APIKey.Purpose = APIKeyPurposeChannelMonitor
		case "subscription":
			input.Subscription = &UserSubscription{ID: 1}
			input.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
		}
		s := &OpenAIGatewayService{cfg: cfg}
		body, quote, err := s.QuoteTokenAdmission(context.Background(), input)
		require.NoError(t, err)
		require.Nil(t, quote)
		require.Equal(t, input.Body, body)
	}
}

func TestTokenAdmissionEnforceableAPIKeyAndOAuthBounds(t *testing.T) {
	input := tokenAdmissionFixture()
	input.Body = []byte(`{"model":"gpt-6-astra","input":"hello","max_completion_tokens":7,"n":3}`)
	b, err := buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Equal(t, 21, b.output)
	require.Equal(t, int64(7), gjson.GetBytes(b.body, "max_output_tokens").Int())
	input.Account.Type = AccountTypeOAuth
	_, err = buildTokenAdmissionBound(input)
	require.ErrorIs(t, err, ErrTokenAdmissionUnbounded)
	input.Body = []byte(`{"model":"gpt-5.4","max_output_tokens":7}`)
	b, err = buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Equal(t, 128000, b.output)
	input.Account.Type = AccountTypeAPIKey
	input.Protocol = TokenAdmissionMessages
	b, err = buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Equal(t, 128, b.output)
	input.Account.Platform = PlatformAnthropic
	b, err = buildTokenAdmissionBound(input)
	require.NoError(t, err)
	require.Equal(t, 128, b.output, "budget retries cannot raise a reserved cap")
}

func TestTokenAdmissionCatalogMatchesBundledSource(t *testing.T) {
	body, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	var catalog map[string]struct {
		Input  int `json:"max_input_tokens"`
		Output int `json:"max_output_tokens"`
	}
	require.NoError(t, json.Unmarshal(body, &catalog))
	want := map[string]tokenAdmissionModelLimit{}
	for model, limit := range catalog {
		if limit.Input > 0 && limit.Output > 0 {
			want[model] = tokenAdmissionModelLimit{Input: limit.Input, Output: limit.Output}
		}
	}
	require.Equal(t, want, tokenAdmissionModelLimits, "regenerate the embedded limits when catalog metadata changes")
}

func TestTokenAdmissionKnownFreePriceIsAQuote(t *testing.T) {
	input := tokenAdmissionFixture()
	input.APIKey.Group.RateMultiplier = 0
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	s := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	_, quote, err := s.QuoteTokenAdmission(t.Context(), input)
	require.NoError(t, err)
	require.NotNil(t, quote)
	require.Zero(t, quote.Command.BalanceCost)
	require.Positive(t, quote.Usage.InputTokens)
}

func TestTokenAdmissionRejectedOutputFieldCannotRemoveReservedBound(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","max_output_tokens":4096,"input":"hello"}`)
	failure := []byte(`{"error":{"code":"unsupported_parameter","param":"max_output_tokens","message":"unsupported parameter"}}`)
	_, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(400, body, failure, true)
	require.NoError(t, err)
	require.False(t, changed)
	_, _, changed, err = normalizeOpenAIResponsesRejectedFieldRetryBody(400, body, failure)
	require.NoError(t, err)
	require.True(t, changed, "exempt subscription/managed/simple behavior is preserved")
}

func TestTokenAdmissionCustomImagesPreserveBytesAndHighestConfiguredTier(t *testing.T) {
	input := tokenAdmissionFixture()
	input.Account.Platform = PlatformCustom
	input.APIKey.Group.Platform = PlatformCustom
	input.Model = "gpt-image-2"
	input.Body = []byte("{ \"model\":\"gpt-image-2\", \"input\":\"draw a tree\", \"n\":2, \"provider_extension\":true }")
	price1, price2, price4 := 0.4, 0.1, 0.2
	input.APIKey.Group.ImagePrice1K = &price1
	input.APIKey.Group.ImagePrice2K = &price2
	input.APIKey.Group.ImagePrice4K = &price4
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	billing := NewBillingService(cfg, nil)
	s := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	body, quote, err := s.QuoteTokenAdmission(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, input.Body, body)
	require.InDelta(t, 0.8, quote.Command.BalanceCost, 1e-8)
	require.Equal(t, 2, quote.Usage.ImageCount)
}

func TestTokenAdmissionAntigravityQuotesOnlyActualThinkingAdjustment(t *testing.T) {
	for _, tc := range []struct {
		name, model, extra string
		want               int
	}{
		{"ordinary", "claude-sonnet-4-5", "", 128},
		{"explicit thinking", "claude-sonnet-4-5", `,"thinking":{"type":"enabled","budget_tokens":1024}`, 2024},
		{"adaptive high tier", "claude-opus-4-6", `,"thinking":{"type":"adaptive"}`, 25576},
		{"native effort", "claude-opus-4-6", `,"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}`, 128},
		{"flash budget clamp", "gemini-2.5-flash", `,"thinking":{"type":"enabled","budget_tokens":50000}`, 25576},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tokenAdmissionFixture()
			input.Protocol = TokenAdmissionMessages
			input.Account.Platform = PlatformAntigravity
			input.Body = []byte(fmt.Sprintf(`{"model":%q,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]%s}`, tc.model, tc.extra))
			b, err := buildTokenAdmissionBound(input)
			require.NoError(t, err)
			require.Equal(t, tc.want, b.output)
			require.Equal(t, int64(16), gjson.GetBytes(b.body, "max_tokens").Int(), "the client cap is preserved; only the existing transform adjusts it")
		})
	}
}
