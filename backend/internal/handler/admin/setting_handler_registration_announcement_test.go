//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsRegistrationAnnouncementPreservesOmittedFields(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyRegistrationAnnouncementEnabled: "true",
		service.SettingKeyRegistrationAnnouncementContent: "Existing notice",
	})

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyRegistrationAnnouncementEnabled])
	require.Equal(t, "Existing notice", repo.values[service.SettingKeyRegistrationAnnouncementContent])

	rec = doUpdateSettings(t, h, map[string]any{"registration_announcement_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyRegistrationAnnouncementEnabled])
	require.Equal(t, "Existing notice", repo.values[service.SettingKeyRegistrationAnnouncementContent])

	rec = doUpdateSettings(t, h, map[string]any{"registration_announcement_content": "  New notice\nSecond line  "}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyRegistrationAnnouncementEnabled])
	require.Equal(t, "New notice\nSecond line", repo.values[service.SettingKeyRegistrationAnnouncementContent])

	var resp struct {
		Data dto.SystemSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.False(t, resp.Data.RegistrationAnnouncementEnabled)
	require.Equal(t, "New notice\nSecond line", resp.Data.RegistrationAnnouncementContent)

	rec = doUpdateSettings(t, h, map[string]any{"registration_announcement_content": " \r\n "}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, repo.values[service.SettingKeyRegistrationAnnouncementContent])
}

func TestGetSettingsRegistrationAnnouncement(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyRegistrationAnnouncementEnabled: "true",
		service.SettingKeyRegistrationAnnouncementContent: "Welcome",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data dto.SystemSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Data.RegistrationAnnouncementEnabled)
	require.Equal(t, "Welcome", resp.Data.RegistrationAnnouncementContent)
}

func TestDiffSettingsRegistrationAnnouncement(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{
		RegistrationAnnouncementEnabled: true,
		RegistrationAnnouncementContent: "Welcome",
	}, nil, nil, UpdateSettingsRequest{})
	require.ElementsMatch(t, []string{"registration_announcement_enabled", "registration_announcement_content"}, changed)
}
