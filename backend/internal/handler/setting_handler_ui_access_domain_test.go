//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUIAccessDomainPublicSettings(t *testing.T) {
	for _, domain := range []string{"", "example.com"} {
		repo := &settingHandlerPublicRepoStub{values: map[string]string{service.SettingKeyUIAccessDomain: domain}}
		handler := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
		handler.GetPublicSettings(ctx)
		require.Equal(t, http.StatusOK, response.Code)
		var payload struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
		require.Equal(t, domain, payload.Data["ui_access_domain"])
		require.Equal(t, domain != "", payload.Data["ui_access_domain_enabled"])
		require.Equal(t, false, payload.Data["ui_country_restriction_enabled"])
		require.Equal(t, "", payload.Data["ui_blocked_countries"])
	}
}

func TestUIAccessControlsPublicSettingsExplicitSwitches(t *testing.T) {
	repo := &settingHandlerPublicRepoStub{values: map[string]string{
		service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "false",
		service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "US,CN",
	}}
	handler := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
	handler.GetPublicSettings(ctx)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Equal(t, "example.com", payload.Data["ui_access_domain"])
	require.Equal(t, false, payload.Data["ui_access_domain_enabled"])
	require.Equal(t, true, payload.Data["ui_country_restriction_enabled"])
	require.Equal(t, "US,CN", payload.Data["ui_blocked_countries"])
}
