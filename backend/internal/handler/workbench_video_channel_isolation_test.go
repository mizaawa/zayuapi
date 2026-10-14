package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchVideoKeysRepo struct {
	service.APIKeyRepository
	keys map[string]*service.APIKey
}

func (r *workbenchVideoKeysRepo) GetByKeyForAuth(_ context.Context, key string) (*service.APIKey, error) {
	value := r.keys[key]
	if value == nil {
		return nil, service.ErrAPIKeyNotFound
	}
	copy := *value
	return &copy, nil
}

func (r *workbenchVideoKeysRepo) UpdateLastUsed(context.Context, int64, time.Time) error {
	return nil
}

func TestGatewayModels_VideoWorkbenchBearerKeysKeepSeparateCatalogues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channels := service.NewChannelService(&gatewayModelsChannelRepoStub{channels: []service.Channel{
		{ID: 1, Status: service.StatusActive, GroupIDs: []int64{81}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformCustom, BillingMode: service.BillingModeVideo, Models: []string{"a1"}},
		}},
		{ID: 2, Status: service.StatusActive, GroupIDs: []int64{82}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformCustom, BillingMode: service.BillingModeVideo, Models: []string{"b1"}},
		}},
	}}, nil, nil, nil)
	shared := service.Account{ID: 1, GroupIDs: []int64{81, 82}, Platform: service.PlatformCustom, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test", "base_url": "https://example.invalid", "model_mapping": map[string]string{"a1": "a1", "b1": "b1"}}}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		81: {shared}, 82: {shared},
	}}, channels)
	keys := &workbenchVideoKeysRepo{keys: map[string]*service.APIKey{}}
	for key, groupID := range map[string]int64{"test-key-a": 81, "test-key-b": 82} {
		group := &service.Group{ID: groupID, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true, VideoWorkbenchEnabled: true}
		keys.keys[key] = &service.APIKey{ID: groupID, Key: key, Status: service.StatusActive, GroupID: &group.ID, Group: group,
			UserID: 7, User: &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}}
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	keyService := service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, cfg)
	router := gin.New()
	router.Use(gin.HandlerFunc(middleware2.NewAPIKeyAuthMiddleware(keyService, nil, cfg)))
	router.GET("/v1/models", h.Models)
	for _, tc := range []struct{ key, want string }{{"test-key-a", "a1"}, {"test-key-b", "b1"}, {"test-key-a", "a1"}} {
		req := httptest.NewRequest(http.MethodGet, "/v1/models?workbench=video", nil)
		req.Header.Set("Authorization", "Bearer "+tc.key)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var response gatewayModelsResponseForTest
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, []string{tc.want}, modelIDsForTest(response.Data))
	}
}

func TestGatewayModels_VideoWorkbenchKeysIsolateSeparateChannelsWithSharedAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	videoPricing := func(models ...string) []service.ChannelModelPricing {
		return []service.ChannelModelPricing{{
			Platform: service.PlatformCustom, BillingMode: service.BillingModeVideo, Models: models,
		}}
	}
	channels := service.NewChannelService(&gatewayModelsChannelRepoStub{channels: []service.Channel{
		{ID: 1, Status: service.StatusActive, GroupIDs: []int64{81}, ModelPricing: videoPricing("a1")},
		{ID: 2, Status: service.StatusActive, GroupIDs: []int64{82}, ModelPricing: videoPricing("b1")},
	}}, nil, nil, nil)
	shared := service.Account{
		ID: 1, GroupIDs: []int64{81, 82}, Platform: service.PlatformCustom, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test", "base_url": "https://example.invalid",
			"model_mapping": map[string]string{"a1": "a1", "b1": "b1"},
		},
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		81: {shared}, 82: {shared},
	}}, channels)

	for _, tc := range []struct {
		name    string
		groupID int64
		pricing []service.ChannelModelPricing
		list    service.GroupModelsListConfig
		want    []string
	}{
		{name: "key A only sees channel A", groupID: 81, want: []string{"a1"}},
		{name: "key B only sees channel B", groupID: 82, want: []string{"b1"}},
		{name: "switch back to key A", groupID: 81, want: []string{"a1"}},
		{
			name: "explicit group pricing overrides channel declaration", groupID: 81,
			pricing: videoPricing("group-video"), want: []string{"group-video"},
		},
		{
			name: "model list intersects channel declaration", groupID: 81,
			list: service.GroupModelsListConfig{Enabled: true, Models: []string{"a1", "b1"}},
			want: []string{"a1"},
		},
		{
			name: "model list cannot add another channels model", groupID: 81,
			list: service.GroupModelsListConfig{Enabled: true, Models: []string{"b1"}},
			want: []string{},
		},
		{
			name: "model list intersects explicit group pricing", groupID: 81,
			pricing: videoPricing("a1", "group-video"),
			list:    service.GroupModelsListConfig{Enabled: true, Models: []string{"group-video", "b1"}},
			want:    []string{"group-video"},
		},
		{
			name: "enabled empty model list denies all models", groupID: 81,
			pricing: videoPricing("a1"), list: service.GroupModelsListConfig{Enabled: true},
			want: []string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{
				ID: tc.groupID, Platform: service.PlatformComposite,
				ModelPricing: tc.pricing, ModelsListConfig: tc.list,
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?workbench=video", nil)
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: tc.groupID, GroupID: &group.ID, Group: group})
			h.Models(c)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, tc.want, modelIDsForTest(got.Data))
		})
	}
}

func TestGatewayModels_VideoWorkbenchChannelVideoWildcardUsesConcreteAccountModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channels := service.NewChannelService(&gatewayModelsChannelRepoStub{channels: []service.Channel{
		{ID: 1, Status: service.StatusActive, GroupIDs: []int64{81}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformCustom, BillingMode: service.BillingModeVideo, Models: []string{"a*"}},
			{Platform: service.PlatformCustom, BillingMode: service.BillingModeImage, Models: []string{"a2"}},
		}},
		{ID: 2, Status: service.StatusActive, GroupIDs: []int64{82}, ModelPricing: []service.ChannelModelPricing{
			{Platform: service.PlatformCustom, BillingMode: service.BillingModeVideo, Models: []string{"b*"}},
		}},
	}}, nil, nil, nil)
	shared := service.Account{
		ID: 1, GroupIDs: []int64{81, 82}, Platform: service.PlatformCustom, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test", "base_url": "https://example.invalid",
			"model_mapping": map[string]string{"a1": "a1", "a2": "a2", "b1": "b1"},
		},
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		81: {shared}, 82: {shared},
	}}, channels)
	for _, tc := range []struct {
		groupID int64
		want    string
	}{{81, "a1"}, {82, "b1"}} {
		group := &service.Group{ID: tc.groupID, Platform: service.PlatformComposite}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/models?workbench=video", nil)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: tc.groupID, GroupID: &group.ID, Group: group})
		h.Models(c)
		require.Equal(t, http.StatusOK, rec.Code)
		var got gatewayModelsResponseForTest
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, []string{tc.want}, modelIDsForTest(got.Data))
	}
}
