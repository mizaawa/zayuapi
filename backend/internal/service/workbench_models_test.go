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
