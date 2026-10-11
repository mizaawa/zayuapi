package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWorkbenchQueueAtomicAcrossInstancesAndLiveLimits(t *testing.T) {
	first, _, _, mr := newWorkbenchTestService(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	second := service.NewImageTaskServiceWithUploader(NewImageTaskStore(client), nil, time.Hour, time.Minute)
	var limit atomic.Int32
	limit.Store(3)
	policy := func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: true, MaxConcurrent: int(limit.Load()), RetentionMinutes: 15}
	}
	first.SetWorkbenchSettingsResolver(policy)
	second.SetWorkbenchSettingsResolver(policy)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	var tasks []*service.ImageTask
	for range 8 {
		task, err := first.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Count: 1})
		require.NoError(t, err)
		require.Equal(t, service.ImageTaskStatusQueued, task.Status)
		tasks = append(tasks, task)
	}
	var running atomic.Int32
	var attempts sync.WaitGroup
	for _, svc := range []*service.ImageTaskService{first, second} {
		for _, task := range tasks {
			attempts.Add(1)
			go func() {
				defer attempts.Done()
				started, err := svc.StartWorkbench(ctx, owner, task.ID)
				if err != nil {
					require.ErrorIs(t, err, service.ErrImageTaskNotFound)
				}
				if started {
					running.Add(1)
				}
			}()
		}
	}
	attempts.Wait()
	require.EqualValues(t, 3, running.Load())
	limit.Store(1)
	listed, err := first.ListWorkbench(ctx, owner.UserID)
	require.NoError(t, err)
	var queued []string
	var active []string
	for _, task := range listed {
		if task.Status == service.ImageTaskStatusQueued {
			queued = append(queued, task.ID)
		} else {
			active = append(active, task.ID)
		}
	}
	for _, id := range queued {
		started, err := second.StartWorkbench(ctx, owner, id)
		require.NoError(t, err)
		require.False(t, started, "lowering the limit pauses starts without terminating running tasks")
	}
	require.NoError(t, first.Fail(ctx, active[0], http.StatusBadGateway, json.RawMessage(`{"message":"failure"}`)))
	for _, id := range queued {
		started, err := second.StartWorkbench(ctx, owner, id)
		require.NoError(t, err)
		require.False(t, started, "two running tasks still exceed the new limit of one")
	}
	limit.Store(5)
	running.Store(2)
	for _, id := range queued {
		started, err := second.StartWorkbench(ctx, owner, id)
		require.NoError(t, err)
		if started {
			running.Add(1)
		}
	}
	require.EqualValues(t, 5, running.Load())
	other := service.ImageTaskOwner{UserID: 8, APIKeyID: 10}
	task, err := second.CreateWorkbench(ctx, other, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	started, err := second.StartWorkbench(ctx, other, task.ID)
	require.NoError(t, err)
	require.True(t, started, "users have independent execution slots")
}

func TestWorkbenchQueueOrphanLeaseAndCancellation(t *testing.T) {
	svc, store, _, mr := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	record, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	record.QueueLeaseUntil = time.Now().Add(-time.Second).Unix()
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, mr.Set(imageTaskKey(task.ID), string(encoded)))
	_, err = mr.ZAdd(workbenchQueueLeases, float64(record.QueueLeaseUntil), task.ID)
	require.NoError(t, err)
	require.NoError(t, svc.RunWorkbenchCleanup(ctx))
	failed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, failed.Status)
	require.Nil(t, failed.StartedAt)
	require.Equal(t, int64(900), failed.ExpiresAt-*failed.CompletedAt)
	_, err = svc.StartWorkbench(ctx, owner, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)

	task, err = svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	record, err = store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.ErrorIs(t, svc.DeleteWorkbench(ctx, 8, task.ID), service.ErrImageTaskNotFound)
	require.NoError(t, svc.DeleteWorkbench(ctx, owner.UserID, task.ID))
	_, err = svc.StartWorkbench(ctx, owner, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	record.Status = service.ImageTaskStatusFailed
	require.ErrorIs(t, store.Save(ctx, record, time.Minute), service.ErrImageTaskNotFound, "late failure must not recreate a deleted task")
	require.False(t, mr.Exists(imageTaskKey(task.ID)))
}

func TestWorkbenchQueueDeleteClaimRaceAndLeaseSweepCAS(t *testing.T) {
	svc, store, _, _ := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	stale, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	started, err := svc.StartWorkbench(ctx, owner, task.ID)
	require.NoError(t, err)
	require.True(t, started)
	workbenchStore, ok := store.(service.ImageWorkbenchStore)
	require.True(t, ok)
	queueStore, ok := store.(service.ImageWorkbenchQueueStore)
	require.True(t, ok)
	require.ErrorIs(t, workbenchStore.DeleteWorkbench(ctx, stale), service.ErrImageTaskProcessing)
	stale.Status = service.ImageTaskStatusFailed
	require.NoError(t, queueStore.FailExpiredWorkbenchQueue(ctx, stale, time.Now().Add(time.Hour), time.Minute))
	current, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusProcessing, current.Status, "an expired queue snapshot cannot overwrite a claimed task")
	_, err = svc.StartWorkbench(ctx, owner, task.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound, "a task can only start once")
}

func TestWorkbenchQueueCapacityAndExpiredIndexRecovery(t *testing.T) {
	svc, _, _, mr := newWorkbenchTestService(t)
	ctx := context.Background()
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	for range service.ImageWorkbenchQueueLimit {
		_, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
		require.NoError(t, err)
	}
	_, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.ErrorIs(t, err, service.ErrImageWorkbenchQueueFull)
	mr.FastForward(time.Hour)
	_, err = svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err, "expired task records must not leave the pending capacity full")
}
