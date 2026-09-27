//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type plazaModelAvailabilityStub struct {
	models    map[string][]string
	platforms map[string]struct{}
}

func (s *plazaModelAvailabilityStub) GetAvailableModels(_ context.Context, _ *int64, platform string) []string {
	return s.models[platform]
}

func (s *plazaModelAvailabilityStub) GetSchedulablePlatforms(_ context.Context, _ *int64) map[string]struct{} {
	return s.platforms
}

func plazaGroups() []service.PlazaGroup {
	return []service.PlazaGroup{
		{ID: 1, Name: "public-standard", Platform: "anthropic", SubscriptionType: "standard", RateMultiplier: 1},
		{ID: 2, Name: "exclusive-a", Platform: "anthropic", IsExclusive: true, RateMultiplier: 0.5},
		{ID: 3, Name: "public-subscription", Platform: "openai", SubscriptionType: "subscription", RateMultiplier: 1},
		{ID: 4, Name: "exclusive-b", Platform: "openai", IsExclusive: true, RateMultiplier: 0.8},
	}
}

func TestFilterPlazaVisibleGroups_AnonymousSeesOnlyNonExclusive(t *testing.T) {
	// 匿名(allowedExclusive == nil):仅非专属分组;订阅型公开分组照常可见(橱窗语义)。
	visible := filterPlazaVisibleGroups(plazaGroups(), nil)
	require.Len(t, visible, 2)
	ids := []int64{visible[0].ID, visible[1].ID}
	require.ElementsMatch(t, []int64{1, 3}, ids)
}

func TestFilterPlazaVisibleGroups_AuthedSeesGrantedExclusive(t *testing.T) {
	// 登录:非专属 + 授权的专属;未授权的专属仍不可见。
	allowed := map[int64]struct{}{2: {}}
	visible := filterPlazaVisibleGroups(plazaGroups(), allowed)
	require.Len(t, visible, 3)
	ids := make([]int64, 0, len(visible))
	for _, g := range visible {
		ids = append(ids, g.ID)
	}
	require.ElementsMatch(t, []int64{1, 2, 3}, ids)
}

func TestFilterPlazaVisibleGroups_AuthedEmptySetSeesNoExclusive(t *testing.T) {
	// 登录但无任何专属授权(空集合,非 nil):与匿名同样只见非专属,
	// 但语义区分要保持——空集合不能被当作 nil 匿名分支。
	visible := filterPlazaVisibleGroups(plazaGroups(), map[int64]struct{}{})
	require.Len(t, visible, 2)
}

func TestFilterPlazaVisibleGroups_BlockedIDsOnlyHidePublicStandardGroups(t *testing.T) {
	blocked := map[int64]struct{}{1: {}, 2: {}, 3: {}, 4: {}}
	visible := filterPlazaVisibleGroups(plazaGroups(), map[int64]struct{}{2: {}, 4: {}}, blocked)
	// A stale deny row must not hide an exclusive or subscription group after
	// its type changes; only the public standard group is removed.
	require.ElementsMatch(t, []int64{2, 3, 4}, []int64{visible[0].ID, visible[1].ID, visible[2].ID})
}

func TestFilterGroupsByAccountModels_UsesBoundAccountMappings(t *testing.T) {
	h := &ModelPlazaHandler{modelAvailability: &plazaModelAvailabilityStub{
		models:    map[string][]string{service.PlatformOpenAI: {"gpt-5.6-sol"}},
		platforms: map[string]struct{}{service.PlatformOpenAI: {}},
	}}
	groups := []service.PlazaGroup{{
		ID: 1, Platform: service.PlatformOpenAI,
		Models: []service.PlazaModel{
			{Name: "gpt-5.6-sol", Platform: service.PlatformOpenAI},
			{Name: "gpt-5.6-terra", Platform: service.PlatformOpenAI},
			{Name: "gpt-image-2", Platform: service.PlatformOpenAI},
		},
	}}

	got := h.filterGroupsByAccountModels(context.Background(), groups)

	require.Len(t, got, 1)
	require.Equal(t, []service.PlazaModel{{Name: "gpt-5.6-sol", Platform: service.PlatformOpenAI}}, got[0].Models)
}

func TestFilterGroupsByAccountModels_CompositeIncludesCustomPricing(t *testing.T) {
	h := &ModelPlazaHandler{modelAvailability: &plazaModelAvailabilityStub{
		models:    map[string][]string{service.PlatformCustom: {"vendor-model"}},
		platforms: map[string]struct{}{service.PlatformCustom: {}},
	}}
	groups := []service.PlazaGroup{{
		ID:       1,
		Platform: service.PlatformComposite,
		Models: []service.PlazaModel{{
			Name:     "vendor-model",
			Platform: service.PlatformCustom,
			Pricing:  &service.ChannelModelPricing{BillingMode: service.BillingModeToken, InputPrice: testPtr(1e-6)},
		}},
	}}

	got := h.filterGroupsByAccountModels(context.Background(), groups)

	require.Len(t, got, 1)
	require.Equal(t, groups[0].Models, got[0].Models)
	dto := toModelPlazaGroupDTO(&got[0], nil)
	require.Len(t, dto.Models, 1)
	require.NotNil(t, dto.Models[0].Pricing)
	require.Equal(t, testPtr(1e-6), dto.Models[0].Pricing.InputPrice)
}

func TestFilterGroupsByAccountModels_CompositeCustomRespectsModelList(t *testing.T) {
	h := &ModelPlazaHandler{modelAvailability: &plazaModelAvailabilityStub{
		models:    map[string][]string{service.PlatformCustom: {"vendor-*"}},
		platforms: map[string]struct{}{service.PlatformCustom: {}},
	}}
	groups := []service.PlazaGroup{{
		ID:       1,
		Platform: service.PlatformComposite,
		ModelsListConfig: service.GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"vendor-selected", "unavailable-model"},
		},
		Models: []service.PlazaModel{
			{Name: "vendor-selected", Platform: service.PlatformCustom},
			{Name: "vendor-hidden", Platform: service.PlatformCustom},
			{Name: "unavailable-model", Platform: service.PlatformCustom},
		},
	}}

	got := h.filterGroupsByAccountModels(context.Background(), groups)

	require.Len(t, got, 1)
	require.Equal(t, []service.PlazaModel{groups[0].Models[0]}, got[0].Models)
}

func TestFilterGroupsByAccountModels_CompositeCustomRequiresExposedModels(t *testing.T) {
	for _, tt := range []struct {
		name         string
		availability plazaModelAvailabilityStub
	}{
		{
			name: "no custom account",
			availability: plazaModelAvailabilityStub{
				models:    map[string][]string{service.PlatformOpenAI: {"vendor-model"}},
				platforms: map[string]struct{}{service.PlatformOpenAI: {}},
			},
		},
		{
			name: "no explicit custom model mapping",
			availability: plazaModelAvailabilityStub{
				platforms: map[string]struct{}{service.PlatformCustom: {}},
			},
		},
		{
			name: "model absent from account mapping",
			availability: plazaModelAvailabilityStub{
				models:    map[string][]string{service.PlatformCustom: {"other-model"}},
				platforms: map[string]struct{}{service.PlatformCustom: {}},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &ModelPlazaHandler{modelAvailability: &tt.availability}
			groups := []service.PlazaGroup{{
				ID:       1,
				Platform: service.PlatformComposite,
				Models:   []service.PlazaModel{{Name: "vendor-model", Platform: service.PlatformCustom}},
			}}
			require.Empty(t, h.filterGroupsByAccountModels(context.Background(), groups))
		})
	}
}

func TestFilterGroupsByAccountModels_UsesAPIKeyFallbackAndCustomList(t *testing.T) {
	stub := &plazaModelAvailabilityStub{
		models:    map[string][]string{}, // no account mapping: /v1/models uses platform defaults
		platforms: map[string]struct{}{service.PlatformOpenAI: {}},
	}
	h := &ModelPlazaHandler{modelAvailability: stub}
	groups := []service.PlazaGroup{
		{
			ID: 1, Platform: service.PlatformOpenAI,
			Models: []service.PlazaModel{
				{Name: "gpt-5.6", Platform: service.PlatformOpenAI},
				{Name: "private-upstream-model", Platform: service.PlatformOpenAI},
			},
		},
		{
			ID: 2, Platform: service.PlatformOpenAI,
			ModelsListConfig: service.GroupModelsListConfig{
				Enabled: true,
				Models:  []string{"gpt-5.6-terra"},
			},
			Models: []service.PlazaModel{
				{Name: "gpt-5.6", Platform: service.PlatformOpenAI},
				{Name: "gpt-5.6-terra", Platform: service.PlatformOpenAI},
			},
		},
	}

	got := h.filterGroupsByAccountModels(context.Background(), groups)

	require.Len(t, got, 2)
	require.Equal(t, []string{"gpt-5.6"}, []string{got[0].Models[0].Name})
	require.Equal(t, []string{"gpt-5.6-terra"}, []string{got[1].Models[0].Name})
}

func TestFilterGroupsByAccountModels_HidesGroupsWithoutBoundAccounts(t *testing.T) {
	h := &ModelPlazaHandler{modelAvailability: &plazaModelAvailabilityStub{
		models:    map[string][]string{service.PlatformOpenAI: {"gpt-5.6-sol"}},
		platforms: map[string]struct{}{},
	}}
	groups := []service.PlazaGroup{{
		ID:       1,
		Platform: service.PlatformOpenAI,
		Models:   []service.PlazaModel{{Name: "gpt-5.6-sol", Platform: service.PlatformOpenAI}},
	}}

	require.Empty(t, h.filterGroupsByAccountModels(context.Background(), groups))
}

func TestModelPlazaHandler_NilSettingServiceFailsClosed404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ModelPlazaHandler{} // settingService == nil → fail-closed
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/model-plaza", nil)

	h.Get(c)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
}

func TestToModelPlazaGroupDTO_UserRateAndFieldWhitelist(t *testing.T) {
	g := service.PlazaGroup{
		ID: 2, Name: "vip", Description: "d", Platform: "anthropic",
		SubscriptionType: "standard", RateMultiplier: 1, IsExclusive: true,
		Models: []service.PlazaModel{{
			Name:     "claude-sonnet",
			Platform: "anthropic",
			Pricing: &service.ChannelModelPricing{
				BillingMode: service.BillingModeToken,
				InputPrice:  testPtr(3e-6),
			},
			OfficialPricing: &service.PlazaOfficialPricing{
				InputPrice:     testPtr(3e-6),
				CacheReadPrice: testPtr(3e-7),
			},
		}},
	}

	// 有专属倍率:user_rate_multiplier 序列化输出
	dto := toModelPlazaGroupDTO(&g, map[int64]float64{2: 0.5})
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, key := range []string{
		"id", "name", "description", "platform", "subscription_type",
		"rate_multiplier", "user_rate_multiplier", "is_exclusive", "models",
		"peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier",
		"image_rate_independent", "image_rate_multiplier",
	} {
		_, exists := decoded[key]
		require.Truef(t, exists, "plaza group DTO must expose %q", key)
	}
	require.InDelta(t, 0.5, decoded["user_rate_multiplier"].(float64), 1e-9)

	// 模型条目:pricing + official_pricing 并存;official 缺失字段输出 null 而非省略
	models := decoded["models"].([]any)
	require.Len(t, models, 1)
	model := models[0].(map[string]any)
	require.Contains(t, model, "pricing")
	require.Contains(t, model, "official_pricing")
	official := model["official_pricing"].(map[string]any)
	require.Contains(t, official, "input_price")
	require.Contains(t, official, "cache_read_price")
	_, has1h := official["cache_write_1h_price"]
	require.False(t, has1h, "1h 缓存写价为 nil 时应 omitempty")

	// 无专属倍率:user_rate_multiplier 整个字段省略
	dtoNoRate := toModelPlazaGroupDTO(&g, nil)
	rawNoRate, err := json.Marshal(dtoNoRate)
	require.NoError(t, err)
	var decodedNoRate map[string]any
	require.NoError(t, json.Unmarshal(rawNoRate, &decodedNoRate))
	_, hasRate := decodedNoRate["user_rate_multiplier"]
	require.False(t, hasRate, "无专属倍率时 user_rate_multiplier 应 omitempty")
}

func TestToModelPlazaOfficialPricing_NilPassthrough(t *testing.T) {
	require.Nil(t, toModelPlazaOfficialPricing(nil))
}

func testPtr(v float64) *float64 { return &v }
