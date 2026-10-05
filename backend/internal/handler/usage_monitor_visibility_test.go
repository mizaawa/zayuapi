package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userUsageVisibilitySettingsRepo struct {
	service.SettingRepository
	hidden bool
}

func (r *userUsageVisibilitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyChannelMonitorHideUsageLogs && r.hidden {
		return "true", nil
	}
	return "false", nil
}

func TestChannelMonitorUsageVisibility_PersonalListAndStats(t *testing.T) {
	settings := &userUsageVisibilitySettingsRepo{hidden: true}
	repo := &userUsageRepoCapture{}
	svc := service.ProvideUsageService(repo, nil, nil, nil, service.NewSettingService(settings, nil))
	h := NewUsageHandler(svc, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage", h.List)
	router.GET("/stats", h.Stats)
	for _, hidden := range []bool{true, false} {
		settings.hidden = hidden
		for _, path := range []string{"/usage?user_id=99&exact_total=true", "/stats?user_id=99"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
		require.Equal(t, int64(42), repo.listFilters.UserID)
		require.Equal(t, int64(42), repo.statsFilters.UserID)
		require.Equal(t, hidden, repo.listFilters.HideChannelMonitorLogs)
		require.Equal(t, hidden, repo.statsFilters.HideChannelMonitorLogs)
	}
}
