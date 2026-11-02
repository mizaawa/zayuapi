//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

const modelPricingGateTestGroupID = int64(100)

// newModelPricingGateTestResolver prices models with the built-in fallback table
// (grok-4.7 is priced, grok-4.7-fast is not) plus optional channel pricing for
// group 100 on the given platform.
func newModelPricingGateTestResolver(t *testing.T, platform string, channelPricing []ChannelModelPricing) *ModelPricingResolver {
	t.Helper()
	repo := &mockChannelRepository{
		listAllFn: func(_ context.Context) ([]Channel, error) {
			if len(channelPricing) == 0 {
				return nil, nil
			}
			return []Channel{{
				ID:           1,
				Name:         "gate-test-channel",
				Status:       StatusActive,
				GroupIDs:     []int64{modelPricingGateTestGroupID},
				ModelPricing: channelPricing,
			}}, nil
		},
		getGroupPlatformsFn: func(_ context.Context, _ []int64) (map[int64]string, error) {
			return map[int64]string{modelPricingGateTestGroupID: platform}, nil
		},
	}
	return NewModelPricingResolver(NewChannelService(repo, nil, nil, nil), NewBillingService(&config.Config{}, nil))
}

func modelPricingGateTestAPIKey(platform string, groupPricing ...ChannelModelPricing) *APIKey {
	groupID := modelPricingGateTestGroupID
	return &APIKey{
		ID:      7,
		GroupID: &groupID,
		Group:   &Group{ID: groupID, Platform: platform, RateMultiplier: 1, ModelPricing: groupPricing},
	}
}

func modelPricingGateTestAccount(id int64, platform string, mapping map[string]any) *Account {
	credentials := map[string]any{}
	if mapping != nil {
		credentials["model_mapping"] = mapping
	}
	return &Account{ID: id, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: credentials}
}

func TestModelPricingGate_PricedModelInstallsNoGate(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformGrok, nil)
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver,
		modelPricingGateTestAPIKey(PlatformGrok), "grok-4.7", ChannelMappingResult{MappedModel: "grok-4.7"})

	vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
	require.False(t, vetoed)
	_, blocked := ModelPricingGateBlocked(ctx)
	require.False(t, blocked)
}

// grok-4.7-fast has no price anywhere: every account that would forward it
// unchanged is vetoed, so the request never reaches upstream to be billed $0.
func TestModelPricingGate_UnpricedModelVetoesPassthroughAccounts(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformOpenAI, nil)
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver,
		modelPricingGateTestAPIKey(PlatformOpenAI), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})

	vetoed, reason := accountAdmissionVetoReason(ctx, modelPricingGateTestAccount(1, PlatformOpenAI, nil))
	require.True(t, vetoed)
	require.Equal(t, modelPricingFilterReason, reason)

	model, blocked := ModelPricingGateBlocked(ctx)
	require.True(t, blocked)
	require.Equal(t, "grok-4.7-fast", model)
	require.Contains(t, ModelPricingUnavailableMessage(model), `"grok-4.7-fast"`)
}

// An account that maps the unpriced alias to a priced upstream model is billed
// by that upstream model (same candidate list as RecordUsage), so it stays eligible.
func TestModelPricingGate_AccountMappingToPricedModelIsAdmitted(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformOpenAI, nil)
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver,
		modelPricingGateTestAPIKey(PlatformOpenAI), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})

	passthrough := modelPricingGateTestAccount(1, PlatformOpenAI, nil)
	mapped := modelPricingGateTestAccount(2, PlatformOpenAI, map[string]any{"grok-4.7-fast": "grok-4.7"})

	vetoed, _ := modelPricingGateVetoReason(ctx, passthrough)
	require.True(t, vetoed)
	vetoed, _ = modelPricingGateVetoReason(ctx, mapped)
	require.False(t, vetoed)

	_, blocked := ModelPricingGateBlocked(ctx)
	require.False(t, blocked, "an admitted account means the failure is not a pricing problem")
}

func TestModelPricingGate_GroupAndChannelPricingAdmitModel(t *testing.T) {
	price := 2e-6
	t.Run("group per-model pricing", func(t *testing.T) {
		resolver := newModelPricingGateTestResolver(t, PlatformGrok, nil)
		apiKey := modelPricingGateTestAPIKey(PlatformGrok, ChannelModelPricing{
			Platform: PlatformGrok, Models: []string{"grok-4.7-fast"}, BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price,
		})
		ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, apiKey, "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})
		vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
		require.False(t, vetoed)
	})
	t.Run("channel pricing", func(t *testing.T) {
		resolver := newModelPricingGateTestResolver(t, PlatformGrok, []ChannelModelPricing{{
			Platform: PlatformGrok, Models: []string{"grok-4.7-fast"}, BillingMode: BillingModeToken, InputPrice: &price, OutputPrice: &price,
		}})
		ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformGrok), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})
		vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
		require.False(t, vetoed)
	})
	t.Run("channel mapping to a priced model", func(t *testing.T) {
		resolver := newModelPricingGateTestResolver(t, PlatformGrok, nil)
		ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformGrok), "grok-4.7-fast",
			ChannelMappingResult{MappedModel: "grok-4.7", Mapped: true})
		vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
		require.False(t, vetoed)
	})
}

// Listing a model in channel pricing without filling any price only falls back
// to the official price list; for a model without one that is not a price.
func TestModelPricingGate_EmptyChannelEntryIsNotAPrice(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformGrok, []ChannelModelPricing{{
		Platform: PlatformGrok, Models: []string{"grok-4.7-fast"}, BillingMode: BillingModeToken,
	}})
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformGrok), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})

	vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
	require.True(t, vetoed)
}

func TestModelPricingGate_Exemptions(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformOpenAI, nil)
	account := modelPricingGateTestAccount(1, PlatformOpenAI, nil)
	mapping := ChannelMappingResult{MappedModel: "grok-4.7-fast"}

	t.Run("simple mode does not bill", func(t *testing.T) {
		ctx := withModelPricingGate(context.Background(), &config.Config{RunMode: config.RunModeSimple}, resolver, modelPricingGateTestAPIKey(PlatformOpenAI), "grok-4.7-fast", mapping)
		vetoed, _ := modelPricingGateVetoReason(ctx, account)
		require.False(t, vetoed)
	})
	t.Run("channel monitor keys are free", func(t *testing.T) {
		apiKey := modelPricingGateTestAPIKey(PlatformOpenAI)
		apiKey.Purpose = APIKeyPurposeChannelMonitor
		ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, apiKey, "grok-4.7-fast", mapping)
		vetoed, _ := modelPricingGateVetoReason(ctx, account)
		require.False(t, vetoed)
	})
	t.Run("media models carry built-in per-image prices", func(t *testing.T) {
		ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformGrok), "grok-imagine-image", ChannelMappingResult{MappedModel: "grok-imagine-image"})
		vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformGrok, nil))
		require.False(t, vetoed)
	})
}

// Re-installing the gate (another key, a fallback group, a new WS turn) must not
// keep a gate decided for a different model.
func TestModelPricingGate_ReinstallClearsPreviousGate(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformOpenAI, nil)
	apiKey := modelPricingGateTestAPIKey(PlatformOpenAI)
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, apiKey, "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})
	ctx = withModelPricingGate(ctx, &config.Config{}, resolver, apiKey, "grok-4.7", ChannelMappingResult{MappedModel: "grok-4.7"})

	vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformOpenAI, nil))
	require.False(t, vetoed)
}

// The async billing worker has no ForcePlatform, so admission must price with
// the group platform exactly like RecordUsage does.
func TestModelPricingGate_IgnoresForcePlatformLikeBilling(t *testing.T) {
	price := 1e-6
	resolver := newModelPricingGateTestResolver(t, PlatformAnthropic, []ChannelModelPricing{{
		Platform: PlatformAnthropic, Models: []string{"house-model"}, BillingMode: BillingModeToken, InputPrice: &price,
	}})
	ctx := context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity)
	ctx = withModelPricingGate(ctx, &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformAnthropic), "house-model", ChannelMappingResult{MappedModel: "house-model"})

	vetoed, _ := modelPricingGateVetoReason(ctx, modelPricingGateTestAccount(1, PlatformAntigravity, nil))
	require.False(t, vetoed)
}

func TestOpenAIGatewayService_ModelPricingBillableForAccount(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}, resolver: newModelPricingGateTestResolver(t, PlatformOpenAI, nil)}
	apiKey := modelPricingGateTestAPIKey(PlatformOpenAI)
	account := modelPricingGateTestAccount(1, PlatformOpenAI, map[string]any{"cheap-alias": "grok-4.7"})

	require.True(t, svc.ModelPricingBillableForAccount(context.Background(), apiKey, "grok-4.7", ChannelMappingResult{MappedModel: "grok-4.7"}, account))
	require.True(t, svc.ModelPricingBillableForAccount(context.Background(), apiKey, "cheap-alias", ChannelMappingResult{MappedModel: "cheap-alias"}, account))
	require.False(t, svc.ModelPricingBillableForAccount(context.Background(), apiKey, "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"}, account))
}

// The terminal post-slot check must apply the pricing gate even when the group
// has no profit control.
func TestProfitControlVetoLatest_AppliesPricingGateWithoutProfitGate(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformOpenAI, nil)
	ctx := withModelPricingGate(context.Background(), &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformOpenAI), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})

	_, vetoed, reason := profitControlVetoLatest(ctx, modelPricingGateTestAccount(1, PlatformOpenAI, nil), nil)
	require.True(t, vetoed)
	require.Equal(t, modelPricingFilterReason, reason)
}

// Custom accounts skip local model checks, but billing is still local.
func TestIsOpenAICompatibleAccountEligibleForRequest_CustomAccountHonorsPricingGate(t *testing.T) {
	resolver := newModelPricingGateTestResolver(t, PlatformCustom, nil)
	account := &Account{
		ID: 3, Platform: PlatformCustom, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://upstream.example.com"},
	}
	plain := context.Background()
	require.True(t, isOpenAICompatibleAccountEligibleForRequest(plain, account, PlatformCustom, "grok-4.7-fast", false, OpenAIEndpointCapabilityResponses))

	gated := withModelPricingGate(plain, &config.Config{}, resolver, modelPricingGateTestAPIKey(PlatformCustom), "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})
	require.False(t, isOpenAICompatibleAccountEligibleForRequest(gated, account, PlatformCustom, "grok-4.7-fast", false, OpenAIEndpointCapabilityResponses))
}

// Gateway scheduling (Anthropic / Gemini / Antigravity groups): with the gate
// installed only an account that maps the unpriced alias to a priced model is
// selectable; without one the request fails before reaching any upstream.
func TestGatewaySelectAccount_ModelPricingGate(t *testing.T) {
	groupID := int64(10)
	group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, RateMultiplier: 1}
	passthrough := Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true}
	mapped := Account{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Priority: 5, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"house-alias": "claude-sonnet-4-6"}}}

	newService := func(accounts ...Account) *GatewayService {
		repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{}}
		for i := range repo.accounts {
			repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
		}
		cfg := testConfig()
		return &GatewayService{
			accountRepo: repo,
			groupRepo:   &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}},
			cfg:         cfg,
			resolver:    NewModelPricingResolver(nil, NewBillingService(cfg, nil)),
		}
	}
	apiKey := &APIKey{ID: 5, GroupID: &groupID, Group: group}
	mapping := ChannelMappingResult{MappedModel: "house-alias"}

	t.Run("prefers the account that bills by a priced model", func(t *testing.T) {
		svc := newService(passthrough, mapped)
		ctx := svc.WithModelPricingGate(context.WithValue(context.Background(), ctxkey.Group, group), apiKey, "house-alias", mapping)
		account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "house-alias", nil)
		require.NoError(t, err)
		require.Equal(t, mapped.ID, account.ID)
	})

	t.Run("rejects when no account can be billed", func(t *testing.T) {
		svc := newService(passthrough)
		ctx := svc.WithModelPricingGate(context.WithValue(context.Background(), ctxkey.Group, group), apiKey, "house-alias", mapping)
		_, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "house-alias", nil)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		model, blocked := ModelPricingGateBlocked(ctx)
		require.True(t, blocked)
		require.Equal(t, "house-alias", model)
	})

	t.Run("priced models schedule exactly as before", func(t *testing.T) {
		svc := newService(passthrough, mapped)
		ctx := svc.WithModelPricingGate(context.WithValue(context.Background(), ctxkey.Group, group), apiKey, "claude-sonnet-4-6",
			ChannelMappingResult{MappedModel: "claude-sonnet-4-6"})
		account, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "claude-sonnet-4-6", nil)
		require.NoError(t, err)
		require.Equal(t, passthrough.ID, account.ID)
	})
}

// OpenAI / Grok / Custom scheduling, including the previous_response sticky
// path: a sticky account that cannot bill the model is not reused.
func TestOpenAISelectAccountWithScheduler_ModelPricingGate(t *testing.T) {
	groupID := int64(9)
	passthrough := Account{ID: 1001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 1}
	mapped := Account{ID: 1002, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Priority: 5,
		Credentials: map[string]any{"model_mapping": map[string]any{"grok-4.7-fast": "gpt-5.4"}}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.StickyResponseIDTTLSeconds = 3600
	apiKey := &APIKey{ID: 5, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}}
	resolver := NewModelPricingResolver(nil, NewBillingService(cfg, nil))

	newService := func(accounts ...Account) *OpenAIGatewayService {
		return &OpenAIGatewayService{
			accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
			cache:              &schedulerTestGatewayCache{},
			cfg:                cfg,
			rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
			concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			resolver:           resolver,
		}
	}
	gated := func(svc *OpenAIGatewayService) context.Context {
		return svc.WithModelPricingGate(context.Background(), apiKey, "grok-4.7-fast", ChannelMappingResult{MappedModel: "grok-4.7-fast"})
	}

	t.Run("selects the priced mapping account", func(t *testing.T) {
		svc := newService(passthrough, mapped)
		selection, _, err := svc.SelectAccountWithScheduler(gated(svc), &groupID, "", "session-a", "grok-4.7-fast", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.Equal(t, mapped.ID, selection.Account.ID)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	})

	t.Run("previous response sticky account is not reused", func(t *testing.T) {
		svc := newService(passthrough, mapped)
		ctx := gated(svc)
		require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_prev_gate", passthrough.ID, time.Hour))
		// A strict previous_response chain cannot move to another account, so
		// the request is refused instead of being forwarded unbilled.
		selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "resp_prev_gate", "session-b", "grok-4.7-fast", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		require.Nil(t, selection)
	})

	t.Run("no billable account rejects the request", func(t *testing.T) {
		svc := newService(passthrough)
		ctx := gated(svc)
		_, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "session-c", "grok-4.7-fast", nil, OpenAIUpstreamTransportAny, false)
		require.ErrorIs(t, err, ErrNoAvailableAccounts)
		_, blocked := ModelPricingGateBlocked(ctx)
		require.True(t, blocked)
	})
}

// The gate admits a request when the client's model is priced even if the
// channel maps it to an unpriced upstream name; billing must then fall back to
// that same requested model instead of recording $0.
func TestGatewayServiceRecordUsage_FallsBackToRequestedModelPrice(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID:     "gateway_requested_model_fallback",
			Usage:         ClaudeUsage{InputTokens: 1000, OutputTokens: 100},
			Model:         "house-upstream-name",
			UpstreamModel: "house-upstream-name",
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 502},
		User:    &User{ID: 602},
		Account: &Account{ID: 702},
		ChannelUsageFields: ChannelUsageFields{
			OriginalModel:      "claude-sonnet-4",
			ChannelMappedModel: "house-upstream-name",
			BillingModelSource: BillingModelSourceChannelMapped,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	// claude-sonnet-4 fallback card: $3 / $15 per MTok, default multiplier 1.1.
	require.InDelta(t, 1000*3e-6+100*15e-6, usageRepo.lastLog.TotalCost, 1e-12)
	require.Greater(t, usageRepo.lastLog.ActualCost, 0.0)
	require.Equal(t, 1, userRepo.deductCalls)
}

// A blank channel entry on the first billing candidate must not stop the OpenAI
// candidate loop at a $0 card: billing continues with the priced upstream model.
func TestOpenAIRecordUsage_BlankChannelEntrySkipsToPricedCandidate(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newModelPricingGateTestResolver(t, PlatformGrok, []ChannelModelPricing{{
		Platform: PlatformGrok, Models: []string{"grok-4.7-fast"}, BillingMode: BillingModeToken,
	}})
	groupID := modelPricingGateTestGroupID

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:     "blank-channel-entry",
			Model:         "grok-4.7-fast",
			UpstreamModel: "grok-4.7",
			Usage:         OpenAIUsage{InputTokens: 1000, OutputTokens: 100},
			Duration:      time.Second,
		},
		APIKey:  &APIKey{ID: 72, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformGrok, RateMultiplier: 1}},
		User:    &User{ID: 73},
		Account: &Account{ID: 74, Platform: PlatformGrok, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	// grok-4.7 fallback card: $2 / $6 per MTok.
	require.InDelta(t, 1000*2e-6+100*6e-6, usageRepo.lastLog.TotalCost, 1e-12)
	require.Equal(t, 1, userRepo.deductCalls)
}
