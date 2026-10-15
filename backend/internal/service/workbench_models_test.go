//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type workbenchModelsAccountRepo struct {
	AccountRepository
	accounts map[int64][]Account
}

func (r *workbenchModelsAccountRepo) ListSchedulableByGroupID(_ context.Context, id int64) ([]Account, error) {
	return r.accounts[id], nil
}

func workbenchCustomAccount(id int64, groups []int64, mapping map[string]string) Account {
	credentials := map[string]any{"api_key": "test", "base_url": "https://example.invalid"}
	if len(mapping) > 0 {
		credentials["model_mapping"] = mapping
	}
	return Account{ID: id, GroupIDs: groups, Platform: PlatformCustom, Type: AccountTypeAPIKey, Credentials: credentials}
}

func TestWorkbenchImageAccountModelsIgnoreUnschedulableAccounts(t *testing.T) {
	// ListSchedulableByGroupID is the only source: switching scheduling off for
	// an account must remove its models on the next panel refresh.
	s := &GatewayService{accountRepo: &workbenchModelsAccountRepo{accounts: map[int64][]Account{
		// Disabled accounts are absent from the schedulable listing.
		42: {workbenchCustomAccount(2, []int64{42}, map[string]string{"gpt-image-2-live": "gpt-image-2-live", "gpt-image-*": "gpt-image-2-live"})},
	}}}
	// Patterns are mapping rules; the handler drops them from the panel payload
	// while the service still reports what the account maps.
	require.Equal(t, []string{"gpt-image-*", "gpt-image-2-live"},
		s.GetWorkbenchImageAccountModels(context.Background(), &Group{ID: 42, Platform: PlatformCustom}, PlatformCustom))
	require.Empty(t, s.GetWorkbenchImageAccountModels(context.Background(), &Group{ID: 43, Platform: PlatformCustom}, PlatformCustom))
	require.Empty(t, s.GetWorkbenchImageAccountModels(context.Background(), nil, PlatformCustom))
}

func TestWorkbenchPricedImageModelsRespectCustomAccountMappings(t *testing.T) {
	ctx := context.Background()
	price := .04
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformCustom, BillingMode: BillingModeImage, Models: []string{"gpt-image-2", "gpt-image-2-live"}, PerRequestPrice: &price},
		},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformCustom}, nil
		},
	}, nil, nil, nil)
	// Only "gpt-image-2-live" survives on the schedulable account; the declared
	// "gpt-image-2" belongs to an account whose scheduling is switched off.
	repo := &workbenchModelsAccountRepo{accounts: map[int64][]Account{
		42: {workbenchCustomAccount(1, []int64{42}, map[string]string{"gpt-image-2-live": "gpt-image-2-live"})},
	}}
	s := &GatewayService{channelService: channelService, accountRepo: repo}
	group := &Group{ID: 42, Platform: PlatformCustom}
	require.Equal(t, []string{"gpt-image-2-live"}, s.GetWorkbenchPricedModels(ctx, group, PlatformCustom, "image"))
	// A mapping-free Custom account accepts arbitrary names, so the declared
	// model stays publishable when nothing else claims it.
	repo.accounts[42] = []Account{workbenchCustomAccount(1, []int64{42}, nil)}
	require.Equal(t, []string{"gpt-image-2", "gpt-image-2-live"}, s.GetWorkbenchPricedModels(ctx, group, PlatformCustom, "image"))
	// A channel mapping still counts as ownership: the account maps the routed
	// upstream name rather than the public one.
	channels[0].ModelMapping = map[string]map[string]string{PlatformCustom: {"gpt-image-2": "gpt-image-2-live"}}
	channelService.invalidateCache()
	repo.accounts[42] = []Account{workbenchCustomAccount(1, []int64{42}, map[string]string{"gpt-image-2-live": "gpt-image-2-live"})}
	require.Equal(t, []string{"gpt-image-2", "gpt-image-2-live"}, s.GetWorkbenchPricedModels(ctx, group, PlatformCustom, "image"))
	// Video pricing keeps its own ownership rules.
	repo.accounts[42] = []Account{workbenchCustomAccount(1, []int64{42}, map[string]string{"other-video": "other-video"})}
	channels[0].ModelPricing = []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"declared-video"}, PerRequestPrice: &price}}
	channelService.invalidateCache()
	require.Equal(t, []string{"declared-video"}, s.GetWorkbenchPricedModels(ctx, group, PlatformCustom, "video"))
}

func TestWorkbenchPricedModelsGroupAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	price := .01
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42},
		ModelMapping: map[string]map[string]string{PlatformCustom: {"public-video": "provider-video"}},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"public-video", "tier-only", "outside-mapping", "video-*"}, Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: &price}}},
			{Platform: PlatformCustom, BillingMode: BillingModeImage, Models: []string{"image-only"}, PerRequestPrice: &price},
			{Platform: PlatformOpenAI, BillingMode: BillingModeVideo, Models: []string{"foreign-platform"}, PerRequestPrice: &price},
		},
	}}
	channelRepo := &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite}, nil
		},
	}
	channelService := NewChannelService(channelRepo, nil, nil, nil)
	s := &GatewayService{channelService: channelService, accountRepo: &workbenchModelsAccountRepo{accounts: map[int64][]Account{
		42: {{Platform: PlatformCustom, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"provider-video": "provider-video", "tier-only": "tier-only", "image-only": "image-only"}}}},
		43: {{Platform: PlatformCustom, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid"}}},
	}}}
	group := &Group{ID: 42, Platform: PlatformComposite}
	require.Equal(t, []string{"outside-mapping", "public-video", "tier-only"}, s.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "video"))
	require.Equal(t, []string{"image-only"}, s.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "image"))
	require.Empty(t, s.GetWorkbenchPricedModels(ctx, &Group{ID: 43, Platform: PlatformComposite}, PlatformComposite, "video"))
	group.ModelPricing = []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeImage, Models: []string{"tier-only"}, PerRequestPrice: &price}}
	require.Equal(t, []string{"outside-mapping", "public-video"}, s.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "video"))
	// A pricing edit must be visible without waiting for the account model cache.
	channels[0].ModelPricing[0].Models = []string{"tier-only"}
	channelService.invalidateCache()
	group.ModelPricing = nil
	require.Equal(t, []string{"tier-only"}, s.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "video"))
}

func TestWorkbenchPricedModelsSharedChannelUsesGroupVideoDeclaration(t *testing.T) {
	ctx := context.Background()
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42, 43},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a", "video-b"}}},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite, 43: PlatformComposite}, nil
		},
	}, nil, nil, nil)
	shared := Account{Platform: PlatformCustom, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid"}}
	s := &GatewayService{channelService: channelService, accountRepo: &workbenchModelsAccountRepo{
		accounts: map[int64][]Account{42: {shared}, 43: {shared}},
	}}
	a := &Group{ID: 42, Platform: PlatformComposite, ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a"}}}}
	b := &Group{ID: 43, Platform: PlatformComposite, ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-b"}}}}
	require.True(t, GroupHasWorkbenchModelPricing(a, PlatformComposite, "video"))
	require.Equal(t, []string{"video-a"}, s.GetWorkbenchPricedModels(ctx, a, PlatformComposite, "video"))
	require.Equal(t, []string{"video-b"}, s.GetWorkbenchPricedModels(ctx, b, PlatformComposite, "video"))
	a.ModelPricing[0].Models = []string{"video-*"}
	require.Equal(t, []string{"video-a", "video-b"}, s.GetWorkbenchPricedModels(ctx, a, PlatformComposite, "video"))
	require.True(t, GroupAllowsWorkbenchVideoModel(a, "video-a"))
	require.False(t, GroupAllowsWorkbenchVideoModel(a, "other-video"))
}

func TestWorkbenchPricedModelsSharedChannelRespectsGroupAccounts(t *testing.T) {
	ctx := context.Background()
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42, 43},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a", "video-b"}}},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite, 43: PlatformComposite}, nil
		},
	}, nil, nil, nil)
	account := func(id, groupID int64, model string) Account {
		return Account{ID: id, GroupIDs: []int64{groupID}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{model: model}}}
	}
	s := &GatewayService{channelService: channelService, accountRepo: &workbenchModelsAccountRepo{
		accounts: map[int64][]Account{42: {account(1, 42, "video-a")}, 43: {account(2, 43, "video-b")}},
	}}
	for _, tc := range []struct {
		groupID int64
		want    string
	}{{42, "video-a"}, {43, "video-b"}} {
		group := &Group{ID: tc.groupID, Platform: PlatformComposite}
		require.Equal(t, []string{tc.want}, s.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "video"))
	}
}

func TestWorkbenchPricedModelsSharedChannelWithoutGroupSourceIsEmpty(t *testing.T) {
	ctx := context.Background()
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42, 43},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a", "video-b"}}},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite, 43: PlatformComposite}, nil
		},
	}, nil, nil, nil)
	shared := Account{ID: 1, GroupIDs: []int64{42, 43}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid"}}
	repo := &workbenchModelsAccountRepo{
		accounts: map[int64][]Account{42: {shared}, 43: {shared}},
	}
	s := &GatewayService{channelService: channelService, accountRepo: repo}
	require.Empty(t, s.GetWorkbenchPricedModels(ctx, &Group{ID: 42, Platform: PlatformComposite}, PlatformComposite, "video"))
	require.Empty(t, s.GetWorkbenchPricedModels(ctx, &Group{ID: 43, Platform: PlatformComposite}, PlatformComposite, "video"))
	shared.Credentials["model_mapping"] = map[string]string{"*": "*"}
	repo.accounts[42] = []Account{shared}
	require.Empty(t, s.GetWorkbenchPricedModels(ctx, &Group{ID: 42, Platform: PlatformComposite}, PlatformComposite, "video"))
	require.Empty(t, s.GetWorkbenchVideoAccountModels(ctx, &Group{ID: 42, Platform: PlatformComposite}, PlatformComposite))
	selected := &Group{ID: 42, Platform: PlatformComposite,
		ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{"video-a"}}}
	require.Equal(t, []string{"video-a"}, s.GetWorkbenchVideoAccountModels(ctx, selected, PlatformComposite))
}

func TestWorkbenchVideoSubmissionRejectsOtherGroupModelOnSharedChannel(t *testing.T) {
	ctx := WithVideoWorkbenchContext(context.Background())
	price := 0.2
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42, 43},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a", "video-b"}, PerRequestPrice: &price}},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite, 43: PlatformComposite}, nil
		},
	}, nil, nil, nil)
	account := func(id, groupID int64, model string) Account {
		return Account{ID: id, GroupIDs: []int64{groupID}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{model: model}}}
	}
	s := &OpenAIGatewayService{channelService: channelService, accountRepo: &workbenchModelsAccountRepo{
		accounts: map[int64][]Account{42: {account(1, 42, "video-a")}, 43: {account(2, 43, "video-b")}},
	}}
	for _, tc := range []struct {
		groupID int64
		allowed string
		denied  string
	}{{42, "video-a", "video-b"}, {43, "video-b", "video-a"}} {
		group := &Group{ID: tc.groupID, Platform: PlatformComposite, VideoPrice720P: &price}
		key := &APIKey{GroupID: &group.ID, Group: group}
		require.NoError(t, s.ValidateCustomVideoPricing(ctx, key, tc.allowed, VideoBillingResolution720P))
		require.ErrorIs(t, s.ValidateCustomVideoPricing(ctx, key, tc.denied, VideoBillingResolution720P), ErrCustomVideoPricingUnavailable)
	}
	group := &Group{ID: 42, Platform: PlatformComposite, VideoPrice720P: &price,
		ModelsListConfig: GroupModelsListConfig{Enabled: true}}
	require.ErrorIs(t, s.ValidateCustomVideoPricing(ctx, &APIKey{GroupID: &group.ID, Group: group}, "video-a", VideoBillingResolution720P), ErrCustomVideoPricingUnavailable)
}

func TestWorkbenchVideoSharedOrWildcardAccountsDoNotDeclareGroupModels(t *testing.T) {
	ctx := context.Background()
	price := 0.2
	channels := []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{42, 43},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a", "video-b"}, PerRequestPrice: &price}},
	}}
	channelService := NewChannelService(&mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) { return channels, nil },
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{42: PlatformComposite, 43: PlatformComposite}, nil
		},
	}, nil, nil, nil)
	for _, tc := range []struct {
		name     string
		accounts map[int64][]Account
	}{
		{name: "same account bound to both groups", accounts: map[int64][]Account{
			42: {{ID: 1, GroupIDs: []int64{42, 43}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"video-a": "video-a", "video-b": "video-b"}}}},
			43: {{ID: 1, GroupIDs: []int64{42, 43}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"video-a": "video-a", "video-b": "video-b"}}}},
		}},
		{name: "prefix wildcard on group-specific accounts", accounts: map[int64][]Account{
			42: {{ID: 1, GroupIDs: []int64{42}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"video-*": "video-*"}}}},
			43: {{ID: 2, GroupIDs: []int64{43}, Platform: PlatformCustom, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"video-*": "video-*"}}}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &workbenchModelsAccountRepo{accounts: tc.accounts}
			catalog := &GatewayService{channelService: channelService, accountRepo: repo}
			gateway := &OpenAIGatewayService{channelService: channelService, accountRepo: repo}
			for _, groupID := range []int64{42, 43} {
				group := &Group{ID: groupID, Platform: PlatformComposite, VideoPrice720P: &price}
				require.Empty(t, catalog.GetWorkbenchVideoAccountModels(ctx, group, PlatformComposite))
				require.Empty(t, catalog.GetWorkbenchPricedModels(ctx, group, PlatformComposite, "video"))
				key := &APIKey{GroupID: &group.ID, Group: group}
				for _, model := range []string{"video-a", "video-b"} {
					require.ErrorIs(t, gateway.ValidateCustomVideoPricing(WithVideoWorkbenchContext(ctx), key, model, VideoBillingResolution720P), ErrCustomVideoPricingUnavailable)
				}
			}
		})
	}
}

func TestWorkbenchVideoChannelDeclarationUsesPricingMatchPrecedence(t *testing.T) {
	group := &Group{ID: 42, Platform: PlatformComposite}
	channel := &Channel{GroupIDs: []int64{42}, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-*"}},
		{Platform: PlatformCustom, BillingMode: BillingModeImage, Models: []string{"video-image"}},
		{Platform: PlatformOpenAI, BillingMode: BillingModeVideo, Models: []string{"foreign-video"}},
	}}
	require.True(t, workbenchVideoSourcesAllowModel(group, channel, nil, "video-a"))
	require.False(t, workbenchVideoSourcesAllowModel(group, channel, nil, "video-image"))
	require.False(t, workbenchVideoSourcesAllowModel(group, channel, nil, "foreign-video"))
	require.False(t, workbenchVideoSourcesAllowModel(group, channel, nil, "other-video"))
	require.False(t, workbenchVideoSourcesAllowModel(group, channel, nil, "video-*"))
}
