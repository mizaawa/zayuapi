package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageTaskStoreRoundTripAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:        "imgtask_123",
		UserID:    7,
		APIKeyID:  9,
		Status:    service.ImageTaskStatusProcessing,
		CreatedAt: 100,
		ExpiresAt: 200,
	}

	require.NoError(t, store.Save(context.Background(), task, 24*time.Hour))
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskKey(task.ID)))
}

func TestImageTaskStoreMissing(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)

	_, err := store.Get(context.Background(), "imgtask_missing")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}

func TestImageTaskFinishCannotOverwriteTerminalResult(t *testing.T) {
	for _, workbench := range []bool{false, true} {
		t.Run(map[bool]string{false: "api", true: "workbench"}[workbench], func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			store, ok := NewImageTaskStore(rdb).(*imageTaskStore)
			require.True(t, ok)
			ctx := context.Background()
			task := &service.ImageTaskRecord{ID: "imgtask_terminal", UserID: 7, APIKeyID: 9, Status: service.ImageTaskStatusProcessing, ExpiresAt: time.Now().Add(time.Hour).Unix()}
			if workbench {
				task.Workbench = &service.ImageWorkbenchMetadata{}
				require.NoError(t, store.CreateWorkbench(ctx, task, time.Hour, 10))
			} else {
				require.NoError(t, store.Save(ctx, task, time.Hour))
			}
			stale := *task
			task.Status = service.ImageTaskStatusCompleted
			task.Result = json.RawMessage(`{"data":[{"url":"saved-image"}]}`)
			require.NoError(t, store.Finish(ctx, task, time.Hour))
			stale.Status = service.ImageTaskStatusFailed
			require.NoError(t, store.Finish(ctx, &stale, time.Minute))
			stored, err := store.Get(ctx, task.ID)
			require.NoError(t, err)
			require.Equal(t, service.ImageTaskStatusCompleted, stored.Status)
			require.JSONEq(t, string(task.Result), string(stored.Result))
			require.Equal(t, time.Hour, mr.TTL(imageTaskKey(task.ID)))
			require.NoError(t, rdb.Del(ctx, imageTaskKey(task.ID)).Err())
			require.ErrorIs(t, store.Finish(ctx, &stale, time.Hour), service.ErrImageTaskNotFound)
		})
	}
}
