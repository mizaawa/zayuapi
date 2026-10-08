//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsGitHubTokenPreservesReplacesAndClears(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyUpdateGitHubToken: "original-secret",
	})
	for _, test := range []struct {
		body map[string]any
		want string
	}{
		{body: map[string]any{"risk_control_enabled": true}, want: "original-secret"},
		{body: map[string]any{"update_github_token": "  replacement-secret  "}, want: "replacement-secret"},
		{body: map[string]any{"risk_control_enabled": false}, want: "replacement-secret"},
		{body: map[string]any{"update_github_token": ""}},
		{body: map[string]any{"risk_control_enabled": true}},
	} {
		rec := doUpdateSettings(t, h, test.body, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, test.want, repo.values[service.SettingKeyUpdateGitHubToken])
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, test.want != "", response.Data["update_github_token_configured"])
		require.NotContains(t, response.Data, "update_github_token")
		require.NotContains(t, rec.Body.String(), "original-secret")
		require.NotContains(t, rec.Body.String(), "replacement-secret")
	}
}

func TestGetSettingsGitHubTokenDoesNotExposeSecret(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyUpdateGitHubToken: "stored-secret",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, true, response.Data["update_github_token_configured"])
	require.NotContains(t, response.Data, "update_github_token")
	require.NotContains(t, rec.Body.String(), "stored-secret")
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{
		UpdateGitHubToken: "stored-secret",
	}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "update_github_token")
	require.NotContains(t, changed, "stored-secret")
}
