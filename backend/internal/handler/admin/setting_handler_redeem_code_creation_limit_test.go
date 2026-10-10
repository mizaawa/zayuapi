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

func TestUpdateSettingsRedeemCodeCreationLimitPersistsAndPreservesOmitted(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	enabled := doUpdateSettings(t, h, map[string]any{"disable_redeem_code_creation_limit": true}, nil)
	require.Equal(t, http.StatusOK, enabled.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyDisableRedeemCodeCreationLimit])
	require.Contains(t, enabled.Body.String(), `"disable_redeem_code_creation_limit":true`)

	omitted := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, omitted.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyDisableRedeemCodeCreationLimit])
	require.Contains(t, omitted.Body.String(), `"disable_redeem_code_creation_limit":true`)

	disabled := doUpdateSettings(t, h, map[string]any{"disable_redeem_code_creation_limit": false}, nil)
	require.Equal(t, http.StatusOK, disabled.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyDisableRedeemCodeCreationLimit])
	require.Contains(t, disabled.Body.String(), `"disable_redeem_code_creation_limit":false`)

	omittedAgain := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": false}, nil)
	require.Equal(t, http.StatusOK, omittedAgain.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyDisableRedeemCodeCreationLimit])
}

func TestGetSettingsRedeemCodeCreationLimit(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyDisableRedeemCodeCreationLimit: "true",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"disable_redeem_code_creation_limit":true`)
}

func TestDiffSettingsRedeemCodeCreationLimit(t *testing.T) {
	before := &service.SystemSettings{}
	after := &service.SystemSettings{DisableRedeemCodeCreationLimit: true}
	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), "disable_redeem_code_creation_limit")
	require.Contains(t, diffSettings(after, before, nil, nil, UpdateSettingsRequest{}), "disable_redeem_code_creation_limit")
	require.NotContains(t, diffSettings(after, after, nil, nil, UpdateSettingsRequest{}), "disable_redeem_code_creation_limit")
}
