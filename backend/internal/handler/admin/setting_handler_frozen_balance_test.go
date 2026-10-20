//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsFrozenBalanceReleaseHoursPersistsAndPreservesOmitted(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	updated := doUpdateSettings(t, h, map[string]any{"frozen_balance_release_hours": 6}, nil)
	require.Equal(t, http.StatusOK, updated.Code)
	require.Equal(t, "6", repo.values[service.SettingKeyFrozenBalanceReleaseHours])
	require.Contains(t, updated.Body.String(), `"frozen_balance_release_hours":6`)

	omitted := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, omitted.Code)
	require.Equal(t, "6", repo.values[service.SettingKeyFrozenBalanceReleaseHours])
	require.Contains(t, omitted.Body.String(), `"frozen_balance_release_hours":6`)
}

func TestUpdateSettingsFrozenBalanceReleaseHoursRejectsInvalid(t *testing.T) {
	for _, hours := range []int{-1, 0, 721} {
		t.Run(strconv.Itoa(hours), func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				service.SettingKeyFrozenBalanceReleaseHours: "6",
			})
			rec := doUpdateSettings(t, h, map[string]any{"frozen_balance_release_hours": hours}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "6", repo.values[service.SettingKeyFrozenBalanceReleaseHours])
		})
	}
}

func TestGetSettingsFrozenBalanceReleaseHoursDefaults(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"frozen_balance_release_hours":1`)
}

func TestDiffSettingsFrozenBalanceReleaseHours(t *testing.T) {
	before := &service.SystemSettings{FrozenBalanceReleaseHours: 1}
	after := &service.SystemSettings{FrozenBalanceReleaseHours: 6}
	require.Contains(t, diffSettings(before, after, nil, nil, UpdateSettingsRequest{}), "frozen_balance_release_hours")
	require.NotContains(t, diffSettings(after, after, nil, nil, UpdateSettingsRequest{}), "frozen_balance_release_hours")
}
