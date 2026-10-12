package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type videoFinishResponseLostStore struct {
	service.VideoWorkbenchStore
}

func (s videoFinishResponseLostStore) Finish(ctx context.Context, task *service.VideoWorkbenchRecord, ttl time.Duration, expiredBefore int64) (bool, error) {
	ok, err := s.VideoWorkbenchStore.Finish(ctx, task, ttl, expiredBefore)
	if err == nil && ok {
		return false, errors.New("finish response lost")
	}
	return ok, err
}

func TestVideoWorkbenchCompletionPreservesPublishedFile(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate", true: "lost-response"}[lostResponse], func(t *testing.T) {
			store := videoStoreFixture(t)
			if lostResponse {
				store = videoFinishResponseLostStore{store}
			}
			storage := NewLocalVideoWorkbenchStorage(t.TempDir())
			svc := service.NewVideoWorkbenchServiceWithSettings(store, storage, func(context.Context) service.VideoWorkbenchRuntime {
				return service.VideoWorkbenchRuntime{Enabled: true, MaxConcurrent: 3, RetentionMinutes: 30}
			})
			ctx := context.Background()
			task, err := svc.Create(ctx, 7, service.VideoWorkbenchMetadata{APIKeyID: 9})
			require.NoError(t, err)
			require.NoError(t, svc.AwaitSlot(ctx, task))
			require.NoError(t, svc.Complete(ctx, task, bytes.NewReader(videoMP4Fixture())))
			require.NoError(t, svc.Complete(ctx, task, bytes.NewReader([]byte("invalid duplicate"))))
			file, _, err := svc.Download(ctx, 7, task.ID)
			require.NoError(t, err)
			defer func() { _ = file.Close() }()
			content, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, videoMP4Fixture(), content)
		})
	}
}

func TestVideoWorkbenchStoreRejectsForgedOwner(t *testing.T) {
	store := videoStoreFixture(t)
	ctx, now := context.Background(), time.Now().UTC()
	task := videoTaskFixture(1, 7, now)
	require.NoError(t, store.Create(ctx, task, time.Hour))
	forged := *task
	forged.UserID = 8
	_, err := store.TryStart(ctx, &forged, now, time.Minute, 3)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
	require.ErrorIs(t, store.Delete(ctx, &forged, now), service.ErrVideoTaskNotFound)
	forged.Status = "failed"
	_, err = store.Finish(ctx, &forged, time.Minute, 0)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
	started, err := store.TryStart(ctx, task, now, time.Minute, 3)
	require.NoError(t, err)
	require.True(t, started)
	require.ErrorIs(t, store.Heartbeat(ctx, &forged, now), service.ErrVideoTaskNotFound)
}
