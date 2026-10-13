//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const asyncDeliveryPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="

type asyncImageDeliveryStorage struct {
	mu    sync.Mutex
	err   error
	saved int
}

func (s *asyncImageDeliveryStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.saved++
	return "https://cdn.example/" + key, nil
}

// runAsyncImageTaskForTest performs Submit's admission steps and then runs the
// accepted task synchronously so ledger assertions cannot race the worker.
func runAsyncImageTaskForTest(t *testing.T, h *AsyncImageHandler, key *service.APIKey, body []byte) string {
	t.Helper()
	c, _ := workbenchBillingContext(key, service.PlatformOpenAI, "")
	c.Request.URL.Path = "/v1/images/generations/async"
	require.NoError(t, h.precharge(c, body, service.PlatformOpenAI, "image"))
	require.NotNil(t, mediaPrecharge(c))
	c.Request = c.Request.WithContext(service.WithMediaWorkbenchPrepayment(c.Request.Context(), mediaPrecharge(c)))
	task, err := h.tasks.Create(context.Background(), service.ImageTaskOwner{UserID: key.UserID, APIKeyID: key.ID, UserEmail: key.User.Email})
	require.NoError(t, err)
	taskCtx, recorder, cancel := newAsyncImageTaskContext(c, body, time.Minute)
	h.run(task.ID, service.PlatformOpenAI, taskCtx, recorder, cancel)
	return task.ID
}

func TestAsyncImageSubmitDefersUsageToDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	h := &AsyncImageHandler{tasks: service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)}
	gated := make(chan bool, 1)
	h.execute = func(_ string, c *gin.Context) {
		held := holdAsyncImageUsage(c, func(context.Context) {})
		gated <- held
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://example.test/image.png"}}})
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID,
			User:  &service.User{ID: 7, Email: "owner@example.com"},
			Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(imageOwnerEmailHeader, "owner@example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	select {
	case held := <-gated:
		require.True(t, held, "accepted tasks must hold usage until their result is stored")
	case <-time.After(3 * time.Second):
		t.Fatal("async task did not execute")
	}
	// A synchronous Images request has no task context and bills immediately.
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	require.False(t, holdAsyncImageUsage(c, func(context.Context) {}))
}

func TestAsyncImageTasksBillOnlyStoredResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"stored", "storage-failed"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := &workbenchBillingUpstream{statuses: []int{http.StatusOK}, body: `{"data":[{"b64_json":"` + asyncDeliveryPNG + `"}]}`}
			openAI, ledger, key := newWorkbenchBillingHandler(t, service.PlatformOpenAI, upstream)
			key.User.Email = "owner@example.com"
			storage := &asyncImageDeliveryStorage{}
			if scenario == "storage-failed" {
				storage.err = errors.New("object storage unavailable")
			}
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			tasks := service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
			h := NewAsyncImageHandler(tasks, openAI)

			taskID := runAsyncImageTaskForTest(t, h, key, []byte(`{"model":"gpt-image-2","prompt":"test","size":"1024x1024","n":1}`))

			require.Equal(t, 1, upstream.calls)
			stored, err := store.Get(context.Background(), taskID)
			require.NoError(t, err)
			if scenario == "stored" {
				require.Equal(t, service.ImageTaskStatusCompleted, stored.Status, string(stored.Error))
				require.Len(t, ledger.commands, 1)
				require.InDelta(t, 0.5, ledger.commands[0].BalanceCost, 1e-9)
				require.Zero(t, ledger.refunded)
			} else {
				require.Equal(t, service.ImageTaskStatusFailed, stored.Status)
				require.Empty(t, ledger.commands, "an image its owner never received must not be billed")
				require.Equal(t, 1, ledger.refunded)
			}
			require.Empty(t, ledger.reservations)
		})
	}
}
