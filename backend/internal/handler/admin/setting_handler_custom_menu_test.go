//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsCustomMenuForceNewTabPersistsAndPreservesOmitted(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	enabled := doUpdateSettings(t, h, map[string]any{"custom_menu_force_new_tab": true}, nil)
	require.Equal(t, http.StatusOK, enabled.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyCustomMenuForceNewTab])
	require.Contains(t, enabled.Body.String(), `"custom_menu_force_new_tab":true`)

	omitted := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, omitted.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyCustomMenuForceNewTab])
	require.Contains(t, omitted.Body.String(), `"custom_menu_force_new_tab":true`)

	disabled := doUpdateSettings(t, h, map[string]any{"custom_menu_force_new_tab": false}, nil)
	require.Equal(t, http.StatusOK, disabled.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyCustomMenuForceNewTab])
	require.Contains(t, disabled.Body.String(), `"custom_menu_force_new_tab":false`)

	omittedAgain := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": false}, nil)
	require.Equal(t, http.StatusOK, omittedAgain.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyCustomMenuForceNewTab])
}

func TestGetSettingsCustomMenuForceNewTab(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyCustomMenuForceNewTab: "true",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)

	h.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"custom_menu_force_new_tab":true`)
}

func TestDiffSettingsCustomMenuForceNewTab(t *testing.T) {
	before := &service.SystemSettings{}
	after := &service.SystemSettings{CustomMenuForceNewTab: true}
	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), "custom_menu_force_new_tab")
	require.NotContains(t, diffSettings(after, after, nil, nil, UpdateSettingsRequest{}), "custom_menu_force_new_tab")
}
