package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

func videoStoreFixture(t *testing.T) service.VideoWorkbenchStore {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewVideoWorkbenchStore(rdb)
}

func videoTaskFixture(index int, owner int64, now time.Time) *service.VideoWorkbenchRecord {
	return &service.VideoWorkbenchRecord{VideoWorkbenchTask: service.VideoWorkbenchTask{
		ID:     "vidtask_" + strings.Repeat("0", 30) + string("0123456789abcdef"[index/16]) + string("0123456789abcdef"[index%16]),
		Status: "queued", CreatedAt: now.Unix(), ExpiresAt: now.Add(3 * time.Hour).Unix(),
		Workbench: service.VideoWorkbenchMetadata{APIKeyID: int64(index + 1), Duration: 60},
	}, UserID: owner, LeaseUntil: now.Add(service.VideoWorkbenchLease).Unix(), Deadline: now.Add(service.VideoWorkbenchQueueTimeout).Unix()}
}

func TestVideoWorkbenchAtomicConcurrencyAndLiveLimit(t *testing.T) {
	store := videoStoreFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	var tasks []*service.VideoWorkbenchRecord
	for i := range 8 {
		task := videoTaskFixture(i, 7, now)
		require.NoError(t, store.Create(ctx, task, 3*time.Hour))
		tasks = append(tasks, task)
	}
	var starts atomic.Int32
	var wg sync.WaitGroup
	for _, task := range tasks {
		for range 2 {
			wg.Add(1)
			go func(task service.VideoWorkbenchRecord) {
				defer wg.Done()
				started, err := store.TryStart(ctx, &task, now, 30*time.Minute, 3)
				if err != nil {
					require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
				}
				if started {
					starts.Add(1)
				}
			}(*task)
		}
	}
	wg.Wait()
	require.EqualValues(t, 3, starts.Load(), "all API keys of one user share three slots")
	records, err := store.List(ctx, 7, now)
	require.NoError(t, err)
	var queued, running []*service.VideoWorkbenchRecord
	for _, r := range records {
		if r.Status == "queued" {
			queued = append(queued, r)
		} else {
			running = append(running, r)
		}
	}
	require.Len(t, queued, 5)
	for _, task := range queued {
		started, err := store.TryStart(ctx, task, now, 30*time.Minute, 1)
		require.NoError(t, err)
		require.False(t, started)
	}
	other := videoTaskFixture(9, 8, now)
	require.NoError(t, store.Create(ctx, other, 3*time.Hour))
	started, err := store.TryStart(ctx, other, now, 30*time.Minute, 1)
	require.NoError(t, err)
	require.True(t, started, "users have separate slots")
	failed := *running[0]
	failed.Status = "failed"
	failed.ExpiresAt = now.Add(30 * time.Minute).Unix()
	finished, err := store.Finish(ctx, &failed, 30*time.Minute, 0)
	require.NoError(t, err)
	require.True(t, finished)
	started, err = store.TryStart(ctx, queued[0], now, 30*time.Minute, 3)
	require.NoError(t, err)
	require.True(t, started, "a failed task releases its slot")
	for _, task := range queued[1:] {
		started, err := store.TryStart(ctx, task, now, 30*time.Minute, 5)
		require.NoError(t, err)
		if started {
			starts.Add(1)
		}
	}
	require.EqualValues(t, 5, starts.Load())
}

func TestVideoWorkbenchDeletionAndExpiryCannotResurrectTasks(t *testing.T) {
	store := videoStoreFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	task := videoTaskFixture(1, 7, now)
	require.NoError(t, store.Create(ctx, task, 3*time.Hour))
	require.NoError(t, store.Delete(ctx, task, now))
	_, err := store.TryStart(ctx, task, now, time.Minute, 3)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
	task.Status = "completed"
	_, err = store.Finish(ctx, task, time.Minute, 0)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
	ids, err := store.ExpiredFiles(ctx, now)
	require.NoError(t, err)
	require.Contains(t, ids, task.ID)
	require.NoError(t, store.AcknowledgeCleanup(ctx, task.ID))
	running := videoTaskFixture(2, 7, now)
	require.NoError(t, store.Create(ctx, running, 3*time.Hour))
	started, err := store.TryStart(ctx, running, now, time.Minute, 3)
	require.NoError(t, err)
	require.True(t, started)
	require.ErrorIs(t, store.Delete(ctx, running, now), service.ErrVideoTaskProcessing)
	stale := *running
	stale.Status = "failed"
	finished, err := store.Finish(ctx, &stale, time.Minute, now.Unix())
	require.NoError(t, err)
	require.False(t, finished, "a live lease wins against stale cleanup")
	expiredAt := now.Add(service.VideoWorkbenchLease + time.Second)
	expired, err := store.ExpiredLeases(ctx, expiredAt)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	finished, err = store.Finish(ctx, &stale, time.Minute, expiredAt.Unix())
	require.NoError(t, err)
	require.True(t, finished)
	require.ErrorIs(t, store.Heartbeat(ctx, running, expiredAt), service.ErrVideoTaskNotFound)
}

func videoMP4Fixture() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
}

func TestVideoWorkbenchLocalStreamingAndIndependentDiskCleanup(t *testing.T) {
	directory := t.TempDir()
	storage := NewLocalVideoWorkbenchStorage(directory)
	ctx := context.Background()
	id := "vidtask_" + strings.Repeat("a", 32)
	const size = 70 << 20
	input := io.MultiReader(bytes.NewReader(videoMP4Fixture()), io.LimitReader(zeroVideoReader{}, size))
	written, contentType, err := storage.Write(ctx, id, input, 30*time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, size+len(videoMP4Fixture()), written)
	require.Equal(t, "video/mp4", contentType)
	file, err := storage.Open(ctx, id)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.NoError(t, storage.Sweep(ctx, time.Now().Add(31*time.Minute)))
	_, err = os.Stat(filepath.Join(directory, id))
	require.True(t, os.IsNotExist(err), "disk expiry works without Redis")
	for _, invalid := range []string{"../outside", "/absolute", "vidtask_" + strings.Repeat("/", 32), "vidtask_" + strings.Repeat("a", 32) + ":stream"} {
		_, _, err := storage.Write(ctx, invalid, bytes.NewReader(videoMP4Fixture()), time.Minute)
		require.Error(t, err)
		require.Error(t, storage.Delete(ctx, invalid))
	}
	_, _, err = storage.Write(ctx, id, strings.NewReader("<html>secret error</html>"), time.Minute)
	require.Error(t, err)
	require.NoError(t, storage.Ready(ctx))
	require.NoError(t, os.Mkdir(filepath.Join(directory, id), 0700))
	marker, _ := json.Marshal(time.Now().Add(-time.Minute).Unix())
	require.NoError(t, os.WriteFile(filepath.Join(directory, id, "expires.json"), marker, 0600))
	_, err = storage.Open(ctx, id)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
}

type zeroVideoReader struct{}

func (zeroVideoReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
