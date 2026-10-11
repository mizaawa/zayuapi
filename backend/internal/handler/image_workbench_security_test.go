package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchSecurityStore struct{ localWorkbenchHandlerStore }

func (s *workbenchSecurityStore) ListWorkbench(context.Context, int64, time.Time) ([]*service.ImageTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var tasks []*service.ImageTaskRecord
	for _, task := range s.tasks {
		copy := *task
		tasks = append(tasks, &copy)
	}
	return tasks, nil
}

func TestImageWorkbenchHTTPRejectsForeignSessionsAndHidesProviderData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &workbenchSecurityStore{localWorkbenchHandlerStore{asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}}
	svc := service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) { return nil, false }, time.Hour, time.Minute)
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	data, err := base64.StdEncoding.DecodeString(png)
	require.NoError(t, err)
	svc.SetWorkbenchLocalStorage(localWorkbenchHandlerStorage{data: data})
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(context.Background(), owner, service.ImageWorkbenchMetadata{Prompt: "private prompt", Count: 1})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), task.ID, http.StatusOK, json.RawMessage(`{"debug":"provider-secret","data":[{"b64_json":"`+png+`","revised_prompt":"provider-secret"}]}`)))
	h := NewAsyncImageHandler(svc, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		if id > 0 {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: id})
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: owner.APIKeyID, UserID: id, User: &service.User{ID: id, Email: owner.UserEmail}})
		}
		c.Next()
	})
	listURL := "/api/v1/image-workbench/tasks"
	imageURL := listURL + "/" + task.ID + "/images/0"
	router.GET(listURL, h.ListWorkbench)
	router.DELETE(listURL+"/:task_id", h.DeleteWorkbench)
	router.GET(listURL+"/:task_id/images/:index", h.DownloadWorkbenchImage)
	router.GET("/v1/images/tasks/:task_id", h.Get)
	request := func(method, path string, userID int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
		req.Header.Set(imageOwnerEmailHeader, owner.UserEmail)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct{ method, path string }{{http.MethodGet, listURL}, {http.MethodGet, imageURL}, {http.MethodDelete, listURL + "/" + task.ID}} {
		rec := request(tc.method, tc.path, 0)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.NotContains(t, rec.Body.String(), "private prompt")
		require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
	for _, path := range []string{imageURL, "/v1/images/tasks/" + task.ID} {
		rec := request(http.MethodGet, path, 8)
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.NotContains(t, rec.Body.String(), "private prompt")
	}
	require.Equal(t, http.StatusNotFound, request(http.MethodDelete, listURL+"/"+task.ID, 8).Code)
	rec := request(http.MethodGet, listURL, 8)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"tasks":[]`)
	rec = request(http.MethodGet, listURL, 7)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "private prompt")
	for _, hidden := range []string{"provider-secret", "local-image:", "owner@example.com", "b64_json"} {
		require.NotContains(t, rec.Body.String(), hidden)
	}
	rec = request(http.MethodGet, imageURL, 7)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "attachment;")
	require.Equal(t, data, rec.Body.Bytes())
}

func TestImageWorkbenchRejectsAmbiguousAndHiddenGenerationInputs(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-image-2","prompt":"test","n":20,"n":1}`,
		`{"model":"gpt-image-2","prompt":"test","n":1,"N":20}`,
		`{"model":"gpt-image-2","prompt":"test","n":20,"N":1}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{"image_url":"https://private.invalid/image","IMAGE_URL":"data:image/png;base64,aW1hZ2U="}]}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{"image_url":"https://private.invalid/image","image_url":"data:image/png;base64,aW1hZ2U="}]}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{"image_url":"data:image/png;base64,aW1hZ2U=","file_id":"unchecked-input"}]}`,
		`{"model":"gpt-image-2","prompt":"test","mask":{"image_url":"https://private.invalid/image"}}`,
		`{"model":"gpt-image-2","prompt":"test","image":"https://private.invalid/image"}`,
	} {
		_, err := parseImageWorkbenchMetadata("application/json", []byte(body), 50, 6)
		require.Error(t, err, body)
	}
}
