package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
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
	reads   int
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

func (s *workbenchTestStorage) Read(_ context.Context, key string, _ int64) ([]byte, string, error) {
	for _, saved := range s.keys {
		if saved == key {
			s.reads++
			data, err := base64.StdEncoding.DecodeString(localTestPNG)
			return data, "image/png", err
		}
	}
	return nil, "", fmt.Errorf("object not found")
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
	var started atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner := service.ImageTaskOwner{
				UserID: 7, APIKeyID: int64(i + 1), UserEmail: "owner@example.com",
			}
			task, err := svc.CreateWorkbench(context.Background(), owner, service.ImageWorkbenchMetadata{Prompt: "test", Model: "gpt-image-2", Count: 1})
			if err != nil {
				unexpected.Add(1)
				return
			}
			running, err := svc.StartWorkbench(context.Background(), owner, task.ID)
			if err != nil {
				unexpected.Add(1)
			} else if running {
				started.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Zero(t, unexpected.Load())
	require.EqualValues(t, 5, started.Load())
	tasks, err := svc.ListWorkbench(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, tasks, 30)
	otherTasks, err := svc.ListWorkbench(context.Background(), 8)
	require.NoError(t, err)
	require.Empty(t, otherTasks)
	var queued []*service.ImageTask
	for _, task := range tasks {
		if task.Status == service.ImageTaskStatusProcessing {
			require.NoError(t, svc.Fail(context.Background(), task.ID, http.StatusBadGateway, json.RawMessage(`{"message":"upstream failed"}`)))
		} else {
			queued = append(queued, task)
		}
	}
	require.Len(t, queued, 25)
	for _, task := range queued {
		_, err := svc.StartWorkbench(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: task.Workbench.APIKeyID}, task.ID)
		require.NoError(t, err)
	}
	tasks, err = svc.ListWorkbench(context.Background(), 7)
	require.NoError(t, err)
	active := 0
	for _, task := range tasks {
		if task.Status == service.ImageTaskStatusProcessing {
			active++
		}
	}
	require.Equal(t, 5, active, "finished tasks release execution slots")
}

func TestImageWorkbenchResultsExpireAndRemainScheduledForCleanup(t *testing.T) {
	svc, store, storage, mr := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "example prompt", Model: "gpt-image-2", Count: 1})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	completed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(900), completed.ExpiresAt-*completed.CompletedAt)
	require.Equal(t, 15*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	imageData, err := base64.StdEncoding.DecodeString(localTestPNG)
	require.NoError(t, err)
	require.Contains(t, string(completed.Result), fmt.Sprintf(`"size_bytes":%d`, len(imageData)))
	require.Equal(t, "/api/v1/image-workbench/tasks/"+task.ID+"/images/0", completed.ImageURL)
	require.NotContains(t, string(completed.Result), "images.example")
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
		owner := service.ImageTaskOwner{UserID: 7, APIKeyID: int64(i + 1)}
		task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
		require.NoError(t, err)
		started, err := svc.StartWorkbench(ctx, owner, task.ID)
		require.NoError(t, err)
		require.True(t, started)
	}
	user := service.ImageTaskOwner{UserID: 7, APIKeyID: 3}
	queued, err := svc.CreateWorkbench(ctx, user, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	started, err := svc.StartWorkbench(ctx, user, queued.ID)
	require.NoError(t, err)
	require.False(t, started)
	for i := range 12 {
		owner := service.ImageTaskOwner{UserID: 8, APIKeyID: int64(i + 1), IsAdmin: true}
		task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
		require.NoError(t, err)
		started, err := svc.StartWorkbench(ctx, owner, task.ID)
		require.NoError(t, err)
		require.True(t, started)
	}
	policy.AdminExempt = false
	admin := service.ImageTaskOwner{UserID: 8, APIKeyID: 13, IsAdmin: true}
	queued, err = svc.CreateWorkbench(ctx, admin, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	started, err = svc.StartWorkbench(ctx, admin, queued.ID)
	require.NoError(t, err)
	require.False(t, started)
	policy.AdminExempt = true
	started, err = svc.StartWorkbench(ctx, admin, queued.ID)
	require.NoError(t, err)
	require.True(t, started)
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
	require.Equal(t, service.ImageWorkbenchQueueTimeout+7*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	started, err := svc.StartWorkbench(ctx, owner, task.ID)
	require.NoError(t, err)
	require.True(t, started)
	require.Equal(t, 8*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	policy.RetentionMinutes = 25
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	completed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, int64(25*60), completed.ExpiresAt-*completed.CompletedAt)
	require.Equal(t, 25*time.Minute, mr.TTL(imageTaskKey(task.ID)))
	cleanup, ok := store.(service.ImageWorkbenchStore)
	require.True(t, ok)
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
	started, err := svc.StartWorkbench(ctx, owner, task.ID)
	require.NoError(t, err)
	require.True(t, started)
	require.Error(t, svc.DeleteWorkbench(ctx, 7, task.ID))
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
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
	storage.baseURL = "http://private-minio.example"
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "example"})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	_, _, err = svc.DownloadWorkbenchImage(ctx, 8, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	_, _, err = svc.DownloadWorkbenchImage(ctx, 7, task.ID, 1)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	require.Zero(t, storage.reads)
	data, contentType, err := svc.DownloadWorkbenchImage(ctx, 7, task.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, localTestPNG, base64.StdEncoding.EncodeToString(data))
	require.EqualValues(t, 1, storage.reads)
}
