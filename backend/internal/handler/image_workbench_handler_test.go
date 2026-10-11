package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageWorkbenchSubmissionRequiresGroupDeclaration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		declared  bool
		workbench bool
		status    int
	}{
		{name: "undeclared group", workbench: true, status: http.StatusForbidden},
		{name: "declared group reaches body validation", declared: true, workbench: true, status: http.StatusBadRequest},
		{name: "ordinary image API remains available", status: http.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			executed := make(chan struct{}, 1)
			handler := &AsyncImageHandler{
				tasks: service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute),
				execute: func(_ string, c *gin.Context) {
					executed <- struct{}{}
					c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://example.test/image.png"}}})
				},
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
					ID: 1, UserID: 2, User: &service.User{ID: 2, Email: "owner@example.com"},
					Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: tc.declared},
				})
				c.Next()
			})
			router.POST("/v1/images/generations/async", handler.Submit)
			request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader("{"))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(imageOwnerEmailHeader, "owner@example.com")
			if tc.workbench {
				request.Header.Set(imageWorkbenchHeader, "true")
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.status == http.StatusForbidden {
				require.Contains(t, recorder.Body.String(), "not enabled for the image workbench")
			}
			if tc.status == http.StatusAccepted {
				select {
				case <-executed:
				case <-time.After(time.Second):
					t.Fatal("ordinary image request was not executed")
				}
			} else {
				require.Empty(t, store.tasks)
			}
		})
	}
}

func TestImageWorkbenchMetadataValidation(t *testing.T) {
	_, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"`+strings.Repeat("\u753b", 32000)+`"}`), 50, 6)
	require.NoError(t, err)
	for _, count := range []string{"1"} {
		metadata, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"test","n":`+count+`}`), 50, 6)
		require.NoError(t, err)
		require.Equal(t, "auto", metadata.Quality)
	}
	for _, payload := range []string{
		`{}`, `{"model":"gpt-image-2","prompt":" "}`,
		`{"model":"gpt-image-2","prompt":"test","n":2}`,
		`{"model":"gpt-image-2","prompt":"test","n":3}`,
		`{"model":"gpt-image-2","prompt":"test","n":20}`,
		`{"model":"gpt-image-2","prompt":"test","quality":"invalid"}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{},{},{}]}`,
		`{"model":"gpt-image-2","prompt":"` + strings.Repeat("a", 32001) + `"}`,
	} {
		_, err := parseImageWorkbenchMetadata("application/json", []byte(payload), 50, 6)
		require.Error(t, err)
	}
	_, err = parseImageWorkbenchMetadata("multipart/form-data; boundary=test", []byte(`{}`), 50, 6)
	require.Error(t, err)
}

func TestImageWorkbenchReferenceSizeBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		limit int
		valid bool
	}{
		{name: "exact limit", size: 1024 * 1024, limit: 1, valid: true},
		{name: "one byte above", size: 1024*1024 + 1, limit: 1},
		{name: "unlimited", size: 2 * 1024 * 1024, limit: 0, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, tc.size))
			payload, err := json.Marshal(map[string]any{
				"model": "gpt-image-2", "prompt": "test",
				"images": []map[string]string{{"image_url": imageURL}, {"image_url": imageURL}},
			})
			require.NoError(t, err)
			_, err = parseImageWorkbenchMetadata("application/json", payload, tc.limit, 6)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "1 MB")
			}
		})
	}
	for _, imageURL := range []string{"", "https://example.test/ref.png", "data:image/png;base64,invalid!", "data:image/png;base64,", "data:text/plain;base64,aGk="} {
		payload, err := json.Marshal(map[string]any{
			"model": "gpt-image-2", "prompt": "test", "images": []map[string]string{{"image_url": imageURL}},
		})
		require.NoError(t, err)
		_, err = parseImageWorkbenchMetadata("application/json", payload, 0, 6)
		require.Error(t, err, imageURL)
	}
}

func TestImageWorkbenchRejectsOversizeBeforeCreatingTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &localWorkbenchHandlerStore{asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}
	svc := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 5, MaxImageSizeMB: 1, RetentionMinutes: 15}
	})
	h := NewAsyncImageHandler(svc, nil)
	h.execute = func(_ string, _ *gin.Context) { t.Error("oversized upload reached execution") }
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 1, UserID: 2, User: &service.User{ID: 2, Email: "owner@example.com"},
			Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
		})
		c.Next()
	})
	router.POST("/v1/images/edits/async", h.Submit)
	imageURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 1024*1024+1))
	payload, err := json.Marshal(map[string]any{
		"model": "gpt-image-2", "prompt": "test", "images": []map[string]string{{"image_url": imageURL}},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(imageOwnerEmailHeader, "owner@example.com")
	req.Header.Set(imageWorkbenchHeader, "true")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "1 MB")
	require.Empty(t, store.tasks)
}

func workbenchReferencePayload(t *testing.T, count int) []byte {
	t.Helper()
	images := make([]map[string]string, count)
	for i := range images {
		images[i] = map[string]string{"image_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="}
	}
	payload, err := json.Marshal(map[string]any{"model": "gpt-image-2", "prompt": "test", "images": images})
	require.NoError(t, err)
	return payload
}

func TestImageWorkbenchReferenceCountBoundary(t *testing.T) {
	for _, limit := range []int{1, 6, 8} {
		for _, count := range []int{0, limit, limit + 1} {
			_, err := parseImageWorkbenchMetadata("application/json", workbenchReferencePayload(t, count), 50, limit)
			if count <= limit {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "reference images are allowed")
			}
		}
	}
}

func TestImageWorkbenchReferenceLimitChangesBeforeSubmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &localWorkbenchHandlerStore{asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}
	svc := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	var limit atomic.Int64
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 5, MaxImageSizeMB: 50, MaxReferenceImages: int(limit.Load()), RetentionMinutes: 15}
	})
	h := NewAsyncImageHandler(svc, nil)
	executed := make(chan struct{}, 2)
	h.execute = func(_ string, c *gin.Context) {
		executed <- struct{}{}
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "local fixture"}})
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 2})
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 1, UserID: 2, User: &service.User{ID: 2, Email: "owner@example.com"},
			Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
		})
		c.Next()
	})
	router.POST("/v1/images/edits/async", h.Submit)
	router.GET("/api/v1/image-workbench/tasks", h.ListWorkbench)
	for _, tc := range []struct {
		limit, count, status int
	}{
		{6, 7, http.StatusBadRequest},
		{6, 6, http.StatusAccepted},
		{2, 3, http.StatusBadRequest},
		{8, 8, http.StatusAccepted},
	} {
		limit.Store(int64(tc.limit))
		list := httptest.NewRecorder()
		router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/image-workbench/tasks", nil))
		require.Equal(t, http.StatusOK, list.Code, list.Body.String())
		var response struct {
			Data struct {
				MaxReferenceImages int `json:"max_reference_images"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(list.Body.Bytes(), &response))
		require.Equal(t, tc.limit, response.Data.MaxReferenceImages)
		req := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", strings.NewReader(string(workbenchReferencePayload(t, tc.count))))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(imageOwnerEmailHeader, "owner@example.com")
		req.Header.Set(imageWorkbenchHeader, "true")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tc.status, rec.Code, rec.Body.String())
		if tc.status == http.StatusAccepted {
			select {
			case <-executed:
			case <-time.After(time.Second):
				t.Fatal("reference images did not reach local execution")
			}
			var task service.ImageTask
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &task))
			require.Eventually(t, func() bool {
				result, err := svc.Get(context.Background(), service.ImageTaskOwner{UserID: 2, APIKeyID: 1, UserEmail: "owner@example.com"}, task.ID)
				return err == nil && result.Status == service.ImageTaskStatusFailed
			}, time.Second, time.Millisecond)
		} else {
			require.Contains(t, rec.Body.String(), "reference images are allowed")
			select {
			case <-executed:
				t.Fatal("excess references reached execution")
			default:
			}
		}
	}
}
