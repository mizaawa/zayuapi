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

func TestSettingHandler_GetPublicSettings_CustomMenuForceNewTab(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		stored  string
		enabled bool
	}{
		{name: "missing"},
		{name: "enabled", stored: "true", enabled: true},
		{name: "disabled", stored: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerPublicRepoStub{values: map[string]string{}}
			if tc.stored != "" {
				repo.values[service.SettingKeyCustomMenuForceNewTab] = tc.stored
			}
			h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

			h.GetPublicSettings(c)

			require.Equal(t, http.StatusOK, rec.Code)
			var resp struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Equal(t, tc.enabled, resp.Data["custom_menu_force_new_tab"])
		})
	}
}
