package handler

import (
	"context"
	"encoding/json"
	"io"
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

type queueHandlerStore struct {
	localWorkbenchHandlerStore
	order []string
}

func (s *queueHandlerStore) CreateWorkbench(_ context.Context, task *service.ImageTaskRecord, _ time.Duration, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	s.tasks[task.ID] = &copy
	s.order = append(s.order, task.ID)
	return nil
}

func (s *queueHandlerStore) TryStartWorkbench(_ context.Context, owner service.ImageTaskOwner, id string, now time.Time, ttl time.Duration, limit int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[id]
	if task == nil || task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID || task.Status != service.ImageTaskStatusQueued {
		return false, service.ErrImageTaskNotFound
	}
	running, position := 0, 0
	for _, task := range s.tasks {
		if task.UserID == owner.UserID && task.Status == service.ImageTaskStatusProcessing {
			running++
		}
	}
	for _, queuedID := range s.order {
		if queuedID == id {
			break
		}
		if pending := s.tasks[queuedID]; pending != nil && pending.UserID == owner.UserID && pending.Status == service.ImageTaskStatusQueued {
			position++
		}
	}
	if limit > 0 && position >= limit-running {
		return false, nil
	}
	started := now.Unix()
	task.Status = service.ImageTaskStatusProcessing
	task.StartedAt = &started
	task.ExpiresAt = now.Add(ttl).Unix()
	return true, nil
}

func (s *queueHandlerStore) ListWorkbench(_ context.Context, userID int64, _ time.Time) ([]*service.ImageTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var tasks []*service.ImageTaskRecord
	for _, task := range s.tasks {
		if task.UserID == userID {
			copy := *task
			tasks = append(tasks, &copy)
		}
	}
	return tasks, nil
}

func (s *queueHandlerStore) DeleteWorkbench(_ context.Context, task *service.ImageTaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.tasks[task.ID]
	if current == nil {
		return service.ErrImageTaskNotFound
	}
	if current.Status == service.ImageTaskStatusProcessing {
		return service.ErrImageTaskProcessing
	}
	delete(s.tasks, task.ID)
	return nil
}

func queueHandlerFixture(t *testing.T, limit int, timeout time.Duration) (*AsyncImageHandler, *gin.Engine, *queueHandlerStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := &queueHandlerStore{localWorkbenchHandlerStore: localWorkbenchHandlerStore{asyncImageMemoryStore: asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}}}
	svc := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, timeout)
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: limit, MaxImageSizeMB: 50, MaxReferenceImages: 6, RetentionMinutes: 15}
	})
	h := NewAsyncImageHandler(svc, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9, UserID: 7, Key: "sk-private-fixture",
			User:  &service.User{ID: 7, Email: "owner@example.com"},
			Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
		})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/api/v1/image-workbench/tasks", h.ListWorkbench)
	router.DELETE("/api/v1/image-workbench/tasks/:task_id", h.DeleteWorkbench)
	return h, router, store
}

func submitQueueFixture(t *testing.T, router *gin.Engine) service.ImageTask {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-2","prompt":"snapshot","n":1,"images":[{"image_url":"data:image/png;base64,aW1hZ2U="}]}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(imageWorkbenchHeader, "true")
	request.Header.Set(imageOwnerEmailHeader, "owner@example.com")
	request.Header.Set("X-Request-ID", "same-untrusted-client-id")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var task service.ImageTask
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &task))
	require.Equal(t, service.ImageTaskStatusQueued, task.Status)
	return task
}

func TestWorkbenchQueueRunsThreeGeneratorsInParallelAndRefillsAfterFailure(t *testing.T) {
	h, router, store := queueHandlerFixture(t, 3, 10*time.Second)
	started := make(chan string, 10)
	finish := make(chan struct{}, 10)
	failedID := make(chan string, 1)
	fail := make(chan struct{})
	stop := make(chan struct{})
	defer close(stop)
	var executions atomic.Int32
	h.execute = func(_ string, c *gin.Context) {
		index := executions.Add(1)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || !strings.Contains(string(body), `"n":1`) || !strings.Contains(string(body), "data:image/png;base64,aW1hZ2U=") {
			t.Error("queued execution lost the accepted request snapshot")
		}
		started <- imageWorkbenchBillingRequestID(c)
		release := (<-chan struct{})(finish)
		if index == 1 {
			failedID <- imageWorkbenchBillingRequestID(c)
			release = fail
		}
		select {
		case <-release:
		case <-stop:
		case <-c.Request.Context().Done():
		}
		if index == 1 {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "fixture generation failure"}})
		} else {
			c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://fixture.invalid/image.png"}}})
		}
	}
	for range 5 {
		submitQueueFixture(t, router)
	}
	active := make(map[string]bool)
	for range 3 {
		select {
		case id := <-started:
			require.NotEmpty(t, id)
			require.False(t, active[id])
			active[id] = true
		case <-time.After(3 * time.Second):
			t.Fatal("three generators did not start before any generator was released")
		}
	}
	require.EqualValues(t, 3, executions.Load())
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	listed, err := h.tasks.ListWorkbench(context.Background(), owner.UserID)
	require.NoError(t, err)
	require.Len(t, listed, 5)
	var queued []string
	for _, task := range listed {
		record, err := store.Get(context.Background(), task.ID)
		require.NoError(t, err)
		raw, err := json.Marshal(record)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "sk-private-fixture")
		require.NotContains(t, string(raw), "base64")
		if active[task.ID] {
			require.Equal(t, service.ImageTaskStatusProcessing, task.Status)
			require.NotNil(t, task.StartedAt)
		} else {
			require.Equal(t, service.ImageTaskStatusQueued, task.Status)
			queued = append(queued, task.ID)
		}
	}
	require.Len(t, queued, 2)
	deleted := httptest.NewRecorder()
	router.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/v1/image-workbench/tasks/"+queued[0], nil))
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	close(fail)
	failedTaskID := <-failedID
	select {
	case id := <-started:
		require.Equal(t, queued[1], id)
	case <-time.After(3 * time.Second):
		t.Fatal("releasing a generator did not start the pending task")
	}
	failedTask, err := h.tasks.Get(context.Background(), owner, failedTaskID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, failedTask.Status, "a failed task frees its execution slot")
	for range 3 {
		finish <- struct{}{}
	}
	require.Eventually(t, func() bool {
		listed, err := h.tasks.ListWorkbench(context.Background(), owner.UserID)
		if err != nil || len(listed) != 4 {
			return false
		}
		for _, task := range listed {
			if task.Status == service.ImageTaskStatusProcessing || task.Status == service.ImageTaskStatusQueued {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 4, executions.Load(), "deleted tasks never execute and completed tasks are not replayed")
	_, err = h.tasks.Get(context.Background(), owner, queued[0])
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}

func TestWorkbenchQueueStartsTimeoutOnlyWhenGenerationStarts(t *testing.T) {
	h, router, _ := queueHandlerFixture(t, 1, 150*time.Millisecond)
	started := make(chan time.Duration, 2)
	var executions atomic.Int32
	h.execute = func(_ string, c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		if !ok {
			t.Error("execution context has no timeout")
		}
		started <- time.Until(deadline)
		if executions.Add(1) == 1 {
			<-c.Request.Context().Done()
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://fixture.invalid/image.png"}}})
	}
	submitQueueFixture(t, router)
	submitQueueFixture(t, router)
	for range 2 {
		select {
		case remaining := <-started:
			require.Greater(t, remaining, 100*time.Millisecond)
		case <-time.After(3 * time.Second):
			t.Fatal("queued generation never started")
		}
	}
	require.Eventually(t, func() bool {
		tasks, err := h.tasks.ListWorkbench(context.Background(), 7)
		if err != nil || len(tasks) != 2 {
			return false
		}
		statuses := map[string]int{}
		for _, task := range tasks {
			statuses[task.Status]++
		}
		return statuses[service.ImageTaskStatusFailed] == 1 && statuses[service.ImageTaskStatusCompleted] == 1
	}, time.Second, 10*time.Millisecond)
}

func TestWorkbenchQueueWorkersFollowSavedLimitWithoutStoppingActiveTasks(t *testing.T) {
	h, router, _ := queueHandlerFixture(t, 1, 10*time.Second)
	var limit atomic.Int32
	limit.Store(1)
	h.tasks.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: int(limit.Load()), RetentionMinutes: 15}
	})
	started := make(chan string, 3)
	release := make(chan struct{}, 3)
	stop := make(chan struct{})
	defer close(stop)
	h.execute = func(_ string, c *gin.Context) {
		started <- imageWorkbenchBillingRequestID(c)
		select {
		case <-release:
		case <-stop:
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "fixture failure"}})
	}
	for range 3 {
		submitQueueFixture(t, router)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("initial execution did not start")
	}
	limit.Store(2)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("raising the saved limit did not start a waiting generator")
	}
	limit.Store(1)
	release <- struct{}{}
	select {
	case <-started:
		t.Fatal("lowering the limit started another task before an execution slot was free")
	case <-time.After(1200 * time.Millisecond):
	}
	tasks, err := h.tasks.ListWorkbench(context.Background(), 7)
	require.NoError(t, err)
	statuses := map[string]int{}
	for _, task := range tasks {
		statuses[task.Status]++
	}
	require.Equal(t, 1, statuses[service.ImageTaskStatusProcessing])
	require.Equal(t, 1, statuses[service.ImageTaskStatusQueued])
	limit.Store(3)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("raising the saved limit again did not refill")
	}
	release <- struct{}{}
	release <- struct{}{}
	require.Eventually(t, func() bool {
		tasks, err := h.tasks.ListWorkbench(context.Background(), 7)
		if err != nil || len(tasks) != 3 {
			return false
		}
		for _, task := range tasks {
			if task.Status != service.ImageTaskStatusFailed {
				return false
			}
		}
		return true
	}, time.Second, 10*time.Millisecond)
}

func TestWorkbenchQueueRevokedCredentialsNeverExecute(t *testing.T) {
	h, router, _ := queueHandlerFixture(t, 1, time.Minute)
	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	var revoked atomic.Bool
	h.SetWorkbenchAuthRefresh(func(*gin.Context) error {
		if revoked.Load() {
			return service.ErrImageTaskForbidden
		}
		return nil
	})
	h.execute = func(_ string, c *gin.Context) {
		started <- imageWorkbenchBillingRequestID(c)
		<-release
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "fixture failure"}})
	}
	first := submitQueueFixture(t, router)
	select {
	case id := <-started:
		require.Equal(t, first.ID, id)
	case <-time.After(2 * time.Second):
		t.Fatal("initial execution did not start")
	}
	waiting := submitQueueFixture(t, router)
	revoked.Store(true)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	require.Eventually(t, func() bool {
		task, err := h.tasks.Get(context.Background(), owner, waiting.ID)
		return err == nil && task.Status == service.ImageTaskStatusFailed && task.HTTPStatus == http.StatusForbidden
	}, 3*time.Second, 10*time.Millisecond)
	select {
	case <-started:
		t.Fatal("revoked queued credentials reached the upstream executor")
	default:
	}
}

func TestWorkbenchQueueRefreshesAdminRoleBeforeClaiming(t *testing.T) {
	h, _, _ := queueHandlerFixture(t, 1, time.Minute)
	h.tasks.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 1, AdminExempt: true, RetentionMinutes: 15}
	})
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	running, err := h.tasks.CreateWorkbench(context.Background(), owner, service.ImageWorkbenchMetadata{Count: 1})
	require.NoError(t, err)
	ok, err := h.tasks.StartWorkbench(context.Background(), owner, running.ID)
	require.NoError(t, err)
	require.True(t, ok)
	var checks atomic.Int32
	h.SetWorkbenchAuthRefresh(func(c *gin.Context) error {
		key, _ := middleware2.GetAPIKeyFromContext(c)
		copy := *key
		user := *key.User
		user.Role = service.RoleUser
		copy.User = &user
		c.Set(string(middleware2.ContextKeyAPIKey), &copy)
		checks.Add(1)
		return nil
	})
	started := make(chan string, 1)
	h.execute = func(_ string, c *gin.Context) {
		started <- imageWorkbenchBillingRequestID(c)
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "fixture failure"}})
	}
	// Initial authentication sees an administrator, but the fresh lookup sees
	// a normal user and must keep the task queued behind the occupied slot.
	roleRouter := gin.New()
	roleRouter.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: owner.APIKeyID, UserID: owner.UserID,
			User:  &service.User{ID: owner.UserID, Email: owner.UserEmail, Role: service.RoleAdmin},
			Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true},
		})
		c.Next()
	})
	roleRouter.POST("/v1/images/generations/async", h.Submit)
	waiting := submitQueueFixture(t, roleRouter)
	require.Eventually(t, func() bool { return checks.Load() >= 2 }, 3*time.Second, 10*time.Millisecond)
	task, err := h.tasks.Get(context.Background(), owner, waiting.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusQueued, task.Status)
	require.NoError(t, h.tasks.Fail(context.Background(), running.ID, http.StatusBadGateway, json.RawMessage(`{"message":"fixture failure"}`)))
	select {
	case id := <-started:
		require.Equal(t, waiting.ID, id)
	case <-time.After(2 * time.Second):
		t.Fatal("normal user task did not start after the slot was freed")
	}
}
