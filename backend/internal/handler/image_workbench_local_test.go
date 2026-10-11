package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type localWorkbenchHandlerStore struct {
	asyncImageMemoryStore
}

func (s *localWorkbenchHandlerStore) CreateWorkbench(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration, _ int) error {
	return s.Save(ctx, task, ttl)
}

func (*localWorkbenchHandlerStore) ListWorkbench(context.Context, int64, time.Time) ([]*service.ImageTaskRecord, error) {
	return nil, nil
}

func (*localWorkbenchHandlerStore) ExpiredWorkbench(context.Context, time.Time) ([]service.ImageWorkbenchCleanup, error) {
	return nil, nil
}

func (*localWorkbenchHandlerStore) RemoveWorkbenchCleanup(context.Context, service.ImageWorkbenchCleanup) error {
	return nil
}

func (*localWorkbenchHandlerStore) DeleteWorkbench(context.Context, *service.ImageTaskRecord) error {
	return nil
}

func (s *localWorkbenchHandlerStore) TryStartWorkbench(_ context.Context, owner service.ImageTaskOwner, id string, now time.Time, ttl time.Duration, _ int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[id]
	if task == nil || task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID || task.Status != service.ImageTaskStatusQueued {
		return false, service.ErrImageTaskNotFound
	}
	started := now.Unix()
	task.Status = service.ImageTaskStatusProcessing
	task.StartedAt = &started
	task.ExpiresAt = now.Add(ttl).Unix()
	return true, nil
}

func (*localWorkbenchHandlerStore) ExpiredWorkbenchQueue(context.Context, time.Time) ([]*service.ImageTaskRecord, error) {
	return nil, nil
}

func (*localWorkbenchHandlerStore) FailExpiredWorkbenchQueue(context.Context, *service.ImageTaskRecord, time.Time, time.Duration) error {
	return nil
}

type localWorkbenchHandlerStorage struct{ data []byte }

func (localWorkbenchHandlerStorage) Ready(context.Context) error { return nil }
func (localWorkbenchHandlerStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	return service.LocalImageURLPrefix + key, nil
}
func (s localWorkbenchHandlerStorage) Read(context.Context, string, int64) ([]byte, string, error) {
	return s.data, "image/png", nil
}
func (localWorkbenchHandlerStorage) DeletePrefix(context.Context, string) (int64, error) {
	return 0, nil
}

func TestWorkbenchLocalStorageHTTPWithoutS3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &localWorkbenchHandlerStore{asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}
	svc := service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) { return nil, false }, time.Hour, time.Minute)
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	data, err := base64.StdEncoding.DecodeString(pngBase64)
	require.NoError(t, err)
	svc.SetWorkbenchLocalStorage(localWorkbenchHandlerStorage{data: data})
	h := NewAsyncImageHandler(svc, nil)
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": pngBase64}}})
	}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "Bearer owner" {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: owner.UserID})
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
				ID: owner.APIKeyID, UserID: owner.UserID,
				User:  &service.User{ID: owner.UserID, Email: owner.UserEmail},
				Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
			})
		}
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/api/v1/image-workbench/tasks", h.ListWorkbench)
	router.GET("/api/v1/image-workbench/tasks/:task_id/images/:index", h.DownloadWorkbenchImage)
	request := func(method, url, payload string, workbench, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(imageOwnerEmailHeader, owner.UserEmail)
		if authenticated {
			req.Header.Set("Authorization", "Bearer owner")
		}
		if workbench {
			req.Header.Set(imageWorkbenchHeader, "true")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	list := request(http.MethodGet, "/api/v1/image-workbench/tasks", "", false, true)
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"enabled":true`)
	require.Contains(t, list.Body.String(), `"max_image_size_mb":50`)
	require.Contains(t, list.Body.String(), `"max_reference_images":6`)
	require.Equal(t, http.StatusNotFound, request(http.MethodPost, "/v1/images/generations/async", `{"model":"gpt-image-2","prompt":"test"}`, false, true).Code)
	submitted := request(http.MethodPost, "/v1/images/generations/async", `{"model":"gpt-image-2","prompt":"test"}`, true, true)
	require.Equal(t, http.StatusAccepted, submitted.Code, submitted.Body.String())
	var task service.ImageTask
	require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &task))
	var completed *service.ImageTask
	require.Eventually(t, func() bool {
		var err error
		completed, err = svc.Get(context.Background(), owner, task.ID)
		return err == nil && completed.Status == service.ImageTaskStatusCompleted
	}, 3*time.Second, 10*time.Millisecond)
	require.NotContains(t, string(completed.Result), service.LocalImageURLPrefix)
	image := request(http.MethodGet, completed.ImageURL, "", false, true)
	require.Equal(t, http.StatusOK, image.Code, image.Body.String())
	require.Equal(t, "image/png", image.Header().Get("Content-Type"))
	require.Equal(t, "no-store", image.Header().Get("Cache-Control"))
	require.NotEmpty(t, image.Body.Bytes())
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, completed.ImageURL, "", false, false).Code)
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: false}
	})
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, completed.ImageURL, "", false, true).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodPost, "/v1/images/generations/async", `{"model":"gpt-image-2","prompt":"test"}`, true, true).Code)
}
