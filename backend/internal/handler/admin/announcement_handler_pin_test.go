package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type announcementPinRepoStub struct {
	service.AnnouncementRepository
	result *service.AnnouncementPinResult
	id     int64
}

func (r *announcementPinRepoStub) TogglePin(_ context.Context, id int64) (*service.AnnouncementPinResult, error) {
	r.id = id
	return r.result, nil
}

func TestAnnouncementTogglePinResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousID := int64(12)
	repo := &announcementPinRepoStub{result: &service.AnnouncementPinResult{
		Announcement:           &service.Announcement{ID: 34, Title: "Pinned", IsPinned: true},
		ReplacedAnnouncementID: &previousID,
	}}
	handler := NewAnnouncementHandler(service.NewAnnouncementService(repo, nil, nil, nil))
	router := gin.New()
	router.POST("/announcements/:id/pin", handler.TogglePin)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/announcements/34/pin", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(34), repo.id)
	var body struct {
		Data struct {
			Announcement struct {
				ID       int64 `json:"id"`
				IsPinned bool  `json:"is_pinned"`
			} `json:"announcement"`
			ReplacedAnnouncementID int64 `json:"replaced_announcement_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, int64(34), body.Data.Announcement.ID)
	require.True(t, body.Data.Announcement.IsPinned)
	require.Equal(t, previousID, body.Data.ReplacedAnnouncementID)

	repo.id = 0
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/announcements/invalid/pin", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, repo.id)
}
