package handler

import (
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
	_, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"`+strings.Repeat("\u753b", 32000)+`"}`))
	require.NoError(t, err)
	for _, count := range []string{"1", "3", "5", "10", "20"} {
		metadata, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"test","n":`+count+`}`))
		require.NoError(t, err)
		require.Equal(t, "auto", metadata.Quality)
	}
	for _, payload := range []string{
		`{}`, `{"model":"gpt-image-2","prompt":" "}`,
		`{"model":"gpt-image-2","prompt":"test","n":2}`,
		`{"model":"gpt-image-2","prompt":"test","quality":"invalid"}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{},{},{}]}`,
		`{"model":"gpt-image-2","prompt":"` + strings.Repeat("a", 32001) + `"}`,
	} {
		_, err := parseImageWorkbenchMetadata("application/json", []byte(payload))
		require.Error(t, err)
	}
	_, err = parseImageWorkbenchMetadata("multipart/form-data; boundary=test", []byte(`{}`))
	require.Error(t, err)
}
