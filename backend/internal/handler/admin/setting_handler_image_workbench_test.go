//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsImageWorkbenchPersistsAndPreservesOmitted(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	values := map[string]any{
		"image_workbench_enabled":                  false,
		"image_workbench_max_concurrent":           8,
		"image_workbench_admin_exempt":             true,
		"image_workbench_custom_retention_enabled": true,
		"image_workbench_retention_minutes":        30,
		"image_workbench_tutorial_url":             " https://docs.example.com/images ",
	}
	rec := doUpdateSettings(t, h, values, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	want := map[string]string{
		service.SettingKeyImageWorkbenchEnabled:                "false",
		service.SettingKeyImageWorkbenchMaxConcurrent:          "8",
		service.SettingKeyImageWorkbenchAdminExempt:            "true",
		service.SettingKeyImageWorkbenchCustomRetentionEnabled: "true",
		service.SettingKeyImageWorkbenchRetentionMinutes:       "30",
		service.SettingKeyImageWorkbenchTutorialURL:            "https://docs.example.com/images",
	}
	for key, value := range want {
		require.Equal(t, value, repo.values[key])
	}
	rec = doUpdateSettings(t, h, map[string]any{"disable_redeem_code_creation_limit": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for key, value := range want {
		require.Equal(t, value, repo.values[key], "omitting a field preserves it")
	}
	require.Contains(t, rec.Body.String(), `"image_workbench_max_concurrent":8`)
	require.Contains(t, rec.Body.String(), `"image_workbench_retention_minutes":30`)

	rec = doUpdateSettings(t, h, map[string]any{
		"image_workbench_enabled":                  true,
		"image_workbench_admin_exempt":             false,
		"image_workbench_custom_retention_enabled": false,
		"image_workbench_tutorial_url":             "",
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyImageWorkbenchEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeyImageWorkbenchAdminExempt])
	require.Equal(t, "false", repo.values[service.SettingKeyImageWorkbenchCustomRetentionEnabled])
	require.Empty(t, repo.values[service.SettingKeyImageWorkbenchTutorialURL])
	require.Equal(t, "true", repo.values[service.SettingKeyDisableRedeemCodeCreationLimit])
}

func TestUpdateSettingsImageWorkbenchDefaultsAndValidation(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5", repo.values[service.SettingKeyImageWorkbenchMaxConcurrent])
	require.Equal(t, "15", repo.values[service.SettingKeyImageWorkbenchRetentionMinutes])
	for _, values := range []map[string]any{
		{"image_workbench_max_concurrent": 0},
		{"image_workbench_max_concurrent": 101},
		{"image_workbench_retention_minutes": 0},
		{"image_workbench_retention_minutes": 1441},
		{"image_workbench_tutorial_url": "javascript:alert(1)"},
	} {
		rec = doUpdateSettings(t, h, values, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, "5", repo.values[service.SettingKeyImageWorkbenchMaxConcurrent])
		require.Equal(t, "15", repo.values[service.SettingKeyImageWorkbenchRetentionMinutes])
	}
}
