package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lostResponseStore commits writes but reports a failure, as when a Redis
// reply is lost after the command executed.
type lostResponseStore struct {
	imageTaskMemoryStore
	loseWrite bool
	failRead  bool
}

func (s *lostResponseStore) Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if err := s.imageTaskMemoryStore.Save(ctx, task, ttl); err != nil {
		return err
	}
	if s.loseWrite && task.Status == ImageTaskStatusCompleted {
		s.getErr = nil
		if s.failRead {
			s.getErr = errors.New("redis unavailable")
		}
		return errors.New("connection reset after write")
	}
	return nil
}

type failingImageStorage struct{}

func (failingImageStorage) Save(context.Context, string, string, []byte) (string, error) {
	return "", errors.New("object storage unavailable")
}

func TestImageTaskDeliverReportsWhetherOwnerCanRetrieveResult(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	png := json.RawMessage(`{"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="}]}`)
	for _, tc := range []struct {
		name      string
		storage   ImageStorage
		result    json.RawMessage
		loseWrite bool
		failRead  bool
		delivered bool
		wantErr   bool
		status    string
	}{
		{name: "stored", result: png, delivered: true, status: ImageTaskStatusCompleted},
		{name: "upload failed", storage: failingImageStorage{}, result: png, status: ImageTaskStatusFailed},
		{name: "invalid provider response", result: json.RawMessage(`not-json`), status: ImageTaskStatusFailed},
		{name: "completed write reply lost", result: png, loseWrite: true, delivered: true, status: ImageTaskStatusCompleted},
		{name: "completed state unknown", result: png, loseWrite: true, failRead: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &lostResponseStore{loseWrite: tc.loseWrite, failRead: tc.failRead}
			var uploader *ImageResultUploader
			if tc.storage != nil {
				uploader = NewImageResultUploader(tc.storage, "images/", 0, nil)
			}
			svc := NewImageTaskServiceWithUploader(store, uploader, time.Hour, time.Minute)
			created, err := svc.Create(context.Background(), owner)
			require.NoError(t, err)
			delivered, err := svc.Deliver(context.Background(), created.ID, http.StatusOK, tc.result)
			if tc.wantErr {
				require.Error(t, err)
				require.False(t, delivered)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.delivered, delivered)
			require.Equal(t, tc.status, store.task.Status)
		})
	}
}
