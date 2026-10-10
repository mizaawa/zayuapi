package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type workbenchTestStorage struct {
	keys    []string
	deleted []string
	baseURL string
}

func (s *workbenchTestStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	s.keys = append(s.keys, key)
	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = "https://images.example"
	}
	return baseURL + "/" + key, nil
}

func (s *workbenchTestStorage) DeletePrefix(_ context.Context, prefix string) (int64, error) {
	s.deleted = append(s.deleted, prefix)
	return 1, nil
}

func newWorkbenchTestService(t *testing.T) (*service.ImageTaskService, service.ImageTaskStore, *workbenchTestStorage, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	storage := &workbenchTestStorage{}
	uploader := service.NewImageResultUploader(storage, "images/", 0, nil)
	svc := service.NewImageTaskServiceWithUploader(store, uploader, 24*time.Hour, time.Minute)
	return svc, store, storage, mr
}

func TestImageWorkbenchConcurrentLimitIsAtomicAcrossKeys(t *testing.T) {
	svc, _, _, _ := newWorkbenchTestService(t)
	var accepted atomic.Int32
	var rejected atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateWorkbench(context.Background(), service.ImageTaskOwner{
				UserID: 7, APIKeyID: int64(i + 1), UserEmail: "owner@example.com",
			}, service.ImageWorkbenchMetadata{Prompt: "test", Model: "gpt-image-2", Count: 1})
			switch err {
			case nil:
				accepted.Add(1)
			case service.ErrImageWorkbenchLimit:
				rejected.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, unexpected.Load())
	require.EqualValues(t, 5, accepted.Load())
	require.EqualValues(t, 25, rejected.Load())
	tasks, err := svc.ListWorkbench(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, tasks, 5)
	otherTasks, err := svc.ListWorkbench(context.Background(), 8)
	require.NoError(t, err)
	require.Empty(t, otherTasks)
	require.NoError(t, svc.Fail(context.Background(), tasks[0].ID, http.StatusBadGateway, json.RawMessage(`{"message":"upstream failed"}`)))
	_, err = svc.CreateWorkbench(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 40}, service.ImageWorkbenchMetadata{})
	require.NoError(t, err, "finishing a task releases its user slot")
}

func TestImageWorkbenchResultsExpireAndRemainScheduledForCleanup(t *testing.T) {
	svc, store, storage, mr := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "example prompt", Model: "gpt-image-2", Count: 1})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"aW1hZ2U="}]}`)))
	completed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(900), completed.ExpiresAt-*completed.CompletedAt)
	require.Equal(t, 15*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	require.Contains(t, string(completed.Result), `"size_bytes":5`)
	require.NotContains(t, string(completed.Result), "b64_json")
	require.Len(t, storage.keys, 1)
	require.True(t, strings.HasPrefix(storage.keys[0], "images/workbench/users/"))
	require.NotContains(t, storage.keys[0], "owner@example.com")
	require.ErrorIs(t, svc.DeleteWorkbench(ctx, 8, task.ID), service.ErrImageTaskNotFound)

	record, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	record.ExpiresAt = time.Now().Add(-time.Second).Unix()
	require.NoError(t, store.Save(ctx, record, time.Second))
	mr.FastForward(2 * time.Second)
	_, err = store.Get(ctx, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.NoError(t, svc.RunWorkbenchCleanup(ctx))
	require.Len(t, storage.deleted, 1)
	require.True(t, strings.HasPrefix(storage.keys[0], storage.deleted[0]))
	require.NoError(t, svc.RunWorkbenchCleanup(ctx))
	require.Len(t, storage.deleted, 1, "acknowledged cleanup is not repeated")
}

func TestImageWorkbenchSettingsApplyAcrossKeysAndToAdminExemption(t *testing.T) {
	svc, _, _, _ := newWorkbenchTestService(t)
	policy := service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 2, AdminExempt: true, RetentionMinutes: 15}
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime { return policy })
	ctx := context.Background()
	for i := range 2 {
		_, err := svc.CreateWorkbench(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: int64(i + 1)}, service.ImageWorkbenchMetadata{})
		require.NoError(t, err)
	}
	_, err := svc.CreateWorkbench(ctx, service.ImageTaskOwner{UserID: 7, APIKeyID: 3}, service.ImageWorkbenchMetadata{})
	require.ErrorIs(t, err, service.ErrImageWorkbenchLimit)
	for i := range 12 {
		_, err := svc.CreateWorkbench(ctx, service.ImageTaskOwner{UserID: 8, APIKeyID: int64(i + 1), IsAdmin: true}, service.ImageWorkbenchMetadata{})
		require.NoError(t, err)
	}
	policy.AdminExempt = false
	_, err = svc.CreateWorkbench(ctx, service.ImageTaskOwner{UserID: 8, APIKeyID: 13, IsAdmin: true}, service.ImageWorkbenchMetadata{})
	require.ErrorIs(t, err, service.ErrImageWorkbenchLimit)
}

func TestImageWorkbenchDisabledBlocksExistingTasksWithoutBlockingRegularAsyncTasks(t *testing.T) {
	svc, _, _, _ := newWorkbenchTestService(t)
	policy := service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 5, RetentionMinutes: 15}
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime { return policy })
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	workbench, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	regular, err := svc.Create(ctx, owner)
	require.NoError(t, err)
	policy.Enabled = false
	_, err = svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.ErrorIs(t, err, service.ErrImageWorkbenchDisabled)
	_, err = svc.ListWorkbench(ctx, owner.UserID)
	require.ErrorIs(t, err, service.ErrImageWorkbenchDisabled)
	_, err = svc.Get(ctx, owner, workbench.ID)
	require.ErrorIs(t, err, service.ErrImageWorkbenchDisabled)
	require.ErrorIs(t, svc.DeleteWorkbench(ctx, owner.UserID, workbench.ID), service.ErrImageWorkbenchDisabled)
	_, _, err = svc.DownloadWorkbenchImage(ctx, owner.UserID, workbench.ID, 0)
	require.ErrorIs(t, err, service.ErrImageWorkbenchDisabled)
	_, err = svc.Get(ctx, owner, regular.ID)
	require.NoError(t, err)
}

func TestImageWorkbenchConfiguredRetentionControlsRedisAndCleanupDeadline(t *testing.T) {
	svc, store, _, mr := newWorkbenchTestService(t)
	policy := service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: 5, RetentionMinutes: 7}
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime { return policy })
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	require.Equal(t, 8*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	policy.RetentionMinutes = 25
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"aW1hZ2U="}]}`)))
	completed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(25*60), completed.ExpiresAt-*completed.CompletedAt)
	require.Equal(t, 25*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	cleanup := store.(service.ImageWorkbenchStore)
	expired, err := cleanup.ExpiredWorkbench(ctx, time.Unix(completed.ExpiresAt-1, 0))
	require.NoError(t, err)
	require.Empty(t, expired)
	expired, err = cleanup.ExpiredWorkbench(ctx, time.Unix(completed.ExpiresAt+1, 0))
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, task.ID, expired[0].ID)
}

func TestImageWorkbenchDeletionOnlyAllowsOwnedFinishedTasks(t *testing.T) {
	svc, _, storage, _ := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "example"})
	require.NoError(t, err)
	require.Error(t, svc.DeleteWorkbench(ctx, 7, task.ID))
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"aW1hZ2U="}]}`)))
	require.NoError(t, svc.DeleteWorkbench(ctx, 7, task.ID))
	_, err = svc.Get(ctx, owner, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	tasks, err := svc.ListWorkbench(ctx, 7)
	require.NoError(t, err)
	require.Empty(t, tasks)
	require.Len(t, storage.deleted, 1)
	require.Contains(t, storage.deleted[0], fmt.Sprintf("/%s-", task.ID))
}

func TestImageWorkbenchDownloadChecksOwnerAndImageIndex(t *testing.T) {
	svc, _, storage, _ := newWorkbenchTestService(t)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	storage.baseURL = server.URL
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "example"})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"aW1hZ2U="}]}`)))
	_, _, err = svc.DownloadWorkbenchImage(ctx, 8, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	_, _, err = svc.DownloadWorkbenchImage(ctx, 7, task.ID, 1)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.Zero(t, downloads.Load())
	data, contentType, err := svc.DownloadWorkbenchImage(ctx, 7, task.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, []byte("image"), data)
	require.EqualValues(t, 1, downloads.Load())
}
