package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImageWorkbenchPrivateS3ReadUsesConfiguredEndpointAndOwnership(t *testing.T) {
	var mu sync.Mutex
	objects := make(map[string][]byte)
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPut:
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[r.URL.Path] = data
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			reads++
			data, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	storage, err := NewS3ImageStorage(ctx, &config.ImageStorageConfig{
		Endpoint: server.URL, Bucket: "private-images", Region: "us-east-1", ForcePathStyle: true,
		AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key",
		PublicBaseURL: "https://cdn.example.invalid",
	})
	require.NoError(t, err)
	_, store, _, _ := newWorkbenchTestService(t)
	svc := service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "test", Count: 1})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, task.ID, http.StatusOK, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	completed, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, completed.Status)
	encoded, err := json.Marshal(completed)
	require.NoError(t, err)
	for _, hidden := range []string{server.URL, "private-images", "cdn.example", "test-access-key", "test-secret-key", "owner@example.com"} {
		require.NotContains(t, string(encoded), hidden)
	}
	for _, foreign := range []service.ImageTaskOwner{
		{UserID: 8, APIKeyID: 9, UserEmail: owner.UserEmail},
		{UserID: 7, APIKeyID: 10, UserEmail: owner.UserEmail},
		{UserID: 7, APIKeyID: 9, UserEmail: "other@example.com"},
	} {
		_, err = svc.Get(ctx, foreign, task.ID)
		require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	}
	_, _, err = svc.DownloadWorkbenchImage(ctx, 8, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	mu.Lock()
	require.Zero(t, reads)
	mu.Unlock()
	data, contentType, err := svc.DownloadWorkbenchImage(ctx, 7, task.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, localTestPNG, base64.StdEncoding.EncodeToString(data))
	mu.Lock()
	require.Equal(t, 1, reads)
	var objectKey string
	for key := range objects {
		objectKey = strings.TrimPrefix(key, "/private-images/")
	}
	mu.Unlock()
	_, _, err = storage.Read(ctx, objectKey, 1)
	require.ErrorContains(t, err, "exceeds read limit")
	record, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	record.ExpiresAt = time.Now().Add(-time.Second).Unix()
	require.NoError(t, store.Save(ctx, record, time.Minute))
	_, _, err = svc.DownloadWorkbenchImage(ctx, 7, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}
