//go:build live

package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	imageWorkbenchLiveKey     = "sk-image-workbench-live"
	imageWorkbenchLiveSession = "image-workbench-live-session"
	imageWorkbenchLiveEmail   = "workbench-live@example.test"
)

// TestImageWorkbenchLiveServer exposes the production async workbench routes
// for a real browser. It makes paid upstream calls only when a request is
// submitted. Credentials come from IMAGE_TEST_* environment variables.
//
// Set IMAGE_WORKBENCH_LIVE_ADDR=127.0.0.1:3020, then run:
// go test -tags live ./internal/handler -run '^TestImageWorkbenchLiveServer$' -v -count=1 -timeout 30m
// POST /__live/shutdown with the fixture session token to stop cleanly.
func TestImageWorkbenchLiveServer(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("IMAGE_WORKBENCH_LIVE_ADDR"))
	if addr == "" {
		t.Skip("IMAGE_WORKBENCH_LIVE_ADDR is required to start the opt-in live server")
	}
	host, _, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	ip := net.ParseIP(host)
	require.True(t, ip != nil && ip.IsLoopback(), "live server must bind a loopback IP")
	gateway, account, err := service.NewImageWorkbenchLiveGatewayFromEnvironment()
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	store := &queueHandlerStore{localWorkbenchHandlerStore: localWorkbenchHandlerStore{
		asyncImageMemoryStore: asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)},
	}}
	tasks := service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) {
		return nil, false
	}, time.Hour, 6*time.Minute)
	directory := strings.TrimSpace(os.Getenv("IMAGE_WORKBENCH_LIVE_STORAGE"))
	if directory == "" {
		directory = t.TempDir()
	}
	tasks.SetWorkbenchLocalStorage(repository.NewLocalImageStorage(directory))
	tasks.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{
			Enabled: true, MaxConcurrent: 1, MaxImageSizeMB: 50,
			MaxReferenceImages: 6, RetentionMinutes: 30,
		}
	})

	groupID := int64(3)
	key := &service.APIKey{
		ID: 9, UserID: 7, Key: imageWorkbenchLiveKey, GroupID: &groupID,
		User: &service.User{ID: 7, Email: imageWorkbenchLiveEmail},
		Group: &service.Group{ID: groupID, Platform: account.Platform,
			AllowImageGeneration: true, ImageWorkbenchEnabled: true},
	}
	setIdentity := func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.UserID})
		c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)
		c.Set(string(middleware2.ContextKeyAPIKey), key)
	}
	handler := NewAsyncImageHandler(tasks, &OpenAIGatewayHandler{gatewayService: gateway})
	// The fixture has no balance database. Parsing, queueing and forwarding
	// still run through the production services and their real HTTP transport.
	handler.precharge = nil
	handler.SetWorkbenchAuthRefresh(func(c *gin.Context) error { setIdentity(c); return nil })
	handler.execute = func(_ string, c *gin.Context) {
		body, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "failed to read live request")
			return
		}
		parsed, parseErr := gateway.ParseOpenAIImagesRequest(c, body)
		if parseErr != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", parseErr.Error())
			return
		}
		result, forwardErr := gateway.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
		if forwardErr != nil {
			message := strings.ReplaceAll(forwardErr.Error(), os.Getenv("IMAGE_TEST_API_KEY"), "[redacted]")
			t.Logf("live forward failed: %s", message)
			if !c.Writer.Written() {
				imageTaskJSONError(c, http.StatusBadGateway, "api_error", "live upstream image generation failed")
			}
			return
		}
		t.Logf("live forwarded endpoint=%s model=%s images=%d request_bytes=%d", c.Request.URL.Path, result.UpstreamModel, result.ImageCount, len(body))
	}

	user := gin.H{"id": key.UserID, "email": imageWorkbenchLiveEmail, "username": "live-workbench",
		"role": service.RoleUser, "status": "active", "balance": 100, "concurrency": 1, "run_mode": "standard"}
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			if token != imageWorkbenchLiveKey {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			setIdentity(c)
		} else if strings.HasPrefix(c.Request.URL.Path, "/api/v1/") || c.Request.URL.Path == "/__live/shutdown" {
			if c.Request.URL.Path != "/api/v1/settings/public" && token != imageWorkbenchLiveSession {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			setIdentity(c)
		}
		c.Next()
	})
	router.POST("/v1/images/generations/async", handler.Submit)
	router.POST("/v1/images/edits/async", handler.Submit)
	router.GET("/v1/images/tasks/:task_id", handler.Get)
	router.GET("/api/v1/image-workbench/tasks", handler.ListWorkbench)
	router.DELETE("/api/v1/image-workbench/tasks/:task_id", handler.DeleteWorkbench)
	router.GET("/api/v1/image-workbench/tasks/:task_id/images/:index", handler.DownloadWorkbenchImage)
	router.GET("/v1/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []gin.H{{"id": "gpt-image-2", "object": "model", "media_type": "image"}}})
	})
	router.GET("/api/v1/auth/me", func(c *gin.Context) { response.Success(c, user) })
	router.GET("/api/v1/keys", func(c *gin.Context) {
		response.Success(c, gin.H{"items": []gin.H{{
			"id": key.ID, "user_id": key.UserID, "key": imageWorkbenchLiveKey,
			"name": "Live image provider", "status": "active", "group_id": groupID,
			"group": gin.H{"id": groupID, "name": "Live provider", "platform": account.Platform,
				"allow_image_generation": true, "image_workbench_enabled": true},
		}}, "total": 1, "page": 1, "page_size": 100, "pages": 1})
	})
	router.GET("/api/v1/settings/public", func(c *gin.Context) {
		response.Success(c, gin.H{"site_name": "Sub2API", "image_workbench_enabled": true,
			"registration_enabled": false, "backend_mode_enabled": false, "custom_menu_items": []any{}})
	})
	router.GET("/__live/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ready": true, "platform": account.Platform})
	})
	stop := make(chan struct{})
	var once sync.Once
	router.POST("/__live/shutdown", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
		once.Do(func() { close(stop) })
	})
	assets := strings.TrimSpace(os.Getenv("IMAGE_WORKBENCH_LIVE_FRONTEND"))
	if assets == "" {
		assets = filepath.Join("..", "web", "dist")
	}
	assets, err = filepath.Abs(assets)
	require.NoError(t, err)
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet || strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			c.Status(http.StatusNotFound)
			return
		}
		path := strings.TrimPrefix(c.Request.URL.Path, "/")
		if filepath.IsLocal(filepath.FromSlash(path)) {
			file := filepath.Join(assets, filepath.FromSlash(path))
			if info, statErr := os.Stat(file); statErr == nil && info.Mode().IsRegular() {
				c.File(file)
				return
			}
		}
		c.File(filepath.Join(assets, "index.html"))
	})

	listener, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	server := &http.Server{Handler: router, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	t.Logf("live workbench ready at http://%s/image-workbench; storage=%s", listener.Addr(), directory)
	select {
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, server.Shutdown(ctx))
		require.ErrorIs(t, <-served, http.ErrServerClosed)
	case serveErr := <-served:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			t.Fatal(serveErr)
		}
	}
}
