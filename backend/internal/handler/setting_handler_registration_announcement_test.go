//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingHandler_GetPublicSettings_ExposesRegistrationAnnouncement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerPublicRepoStub{values: map[string]string{
		service.SettingKeyRegistrationAnnouncementEnabled: "true",
		service.SettingKeyRegistrationAnnouncementContent: "Welcome\nPlease use a valid email.",
	}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
	h.GetPublicSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data dto.PublicSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Data.RegistrationAnnouncementEnabled)
	require.Equal(t, "Welcome\nPlease use a valid email.", resp.Data.RegistrationAnnouncementContent)
}
