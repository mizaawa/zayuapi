package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const localTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="

func TestLocalImageStorageConfinementAndCleanup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	storage := NewLocalImageStorage(directory)
	data, err := base64.StdEncoding.DecodeString(localTestPNG)
	require.NoError(t, err)
	key := "images/workbench/users/owner/imgtask_first-0.png"
	url, err := storage.Save(ctx, key, "image/png", data)
	require.NoError(t, err)
	require.Equal(t, service.LocalImageURLPrefix+key, url)
	got, contentType, err := storage.Read(ctx, key, 1024)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.Equal(t, "image/png", contentType)
	_, _, err = storage.Read(ctx, key, 1)
	require.Error(t, err)
	_, err = storage.Save(ctx, "html.png", "image/png", []byte("<!DOCTYPE html><script>alert(1)</script>"))
	require.Error(t, err)
	for _, key := range []string{"../outside.png", "/absolute.png", "a/../../outside.png", `a\..\outside.png`, `C:/outside.png`, ".", ""} {
		_, err := storage.Save(ctx, key, "image/png", data)
		require.Error(t, err, key)
		_, _, err = storage.Read(ctx, key, 1024)
		require.Error(t, err, key)
		_, err = storage.DeletePrefix(ctx, key)
		require.Error(t, err, key)
	}
	_, err = storage.Save(ctx, "images/workbench/users/owner/imgtask_other-0.png", "image/png", data)
	require.NoError(t, err)
	deleted, err := storage.DeletePrefix(ctx, "images/workbench/users/owner/imgtask_first-")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.NoFileExists(t, filepath.Join(directory, filepath.FromSlash(key)))
	require.FileExists(t, filepath.Join(directory, "images/workbench/users/owner/imgtask_other-0.png"))
	deleted, err = storage.DeletePrefix(ctx, "images/workbench/users/owner/imgtask_first-")
	require.NoError(t, err)
	require.Zero(t, deleted)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err == nil {
		_, err = storage.Save(ctx, "escape/outside.png", "image/png", data)
		require.Error(t, err)
		require.NoFileExists(t, filepath.Join(outside, "outside.png"))
	}
}

func TestWorkbenchLocalStorageLifecycleWithoutObjectStorage(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	var remote *service.ImageResultUploader
	newService := func() *service.ImageTaskService {
		svc := service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) {
			return remote, remote != nil
		}, time.Hour, time.Minute)
		svc.SetWorkbenchLocalStorage(NewLocalImageStorage(directory))
		return svc
	}
	svc := newService()
	require.False(t, svc.Enabled(), "the standard async API keeps its existing opt-in")
	require.True(t, svc.WorkbenchEnabled(ctx), "the workbench needs no S3 credentials")
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9, UserEmail: "owner@example.com"}
	task, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{Prompt: "local image", Count: 1})
	require.NoError(t, err)
	// Changing S3 settings while a task runs must not move its storage/cleanup target.
	remote = service.NewImageResultUploader(&workbenchTestStorage{}, "images/", 0, nil)
	require.NoError(t, svc.Complete(ctx, task.ID, 200, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	record, err := store.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageWorkbenchStorageLocal, record.WorkbenchStorage)
	require.Contains(t, string(record.Result), service.LocalImageURLPrefix)
	require.NotContains(t, string(record.Result), "b64_json")
	require.Equal(t, int64(900), record.ExpiresAt-*record.CompletedAt)
	var privateResult struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(record.Result, &privateResult))
	file := filepath.Join(directory, filepath.FromSlash(strings.TrimPrefix(privateResult.Data[0].URL, service.LocalImageURLPrefix)))
	require.FileExists(t, file)
	svc = newService()
	public, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, "/api/v1/image-workbench/tasks/"+task.ID+"/images/0", public.ImageURL)
	require.NotContains(t, string(public.Result), directory)
	require.NotContains(t, string(public.Result), service.LocalImageURLPrefix)
	require.Contains(t, string(public.Result), `"content_type":"image/png"`)
	_, _, err = svc.DownloadWorkbenchImage(ctx, 8, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	data, contentType, err := svc.DownloadWorkbenchImage(ctx, owner.UserID, task.ID, 0)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, localTestPNG, base64.StdEncoding.EncodeToString(data))
	_, _, err = svc.DownloadWorkbenchImage(ctx, owner.UserID, task.ID, 1)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	record.ExpiresAt = time.Now().Add(-time.Second).Unix()
	require.NoError(t, store.Save(ctx, record, time.Second))
	_, _, err = svc.DownloadWorkbenchImage(ctx, owner.UserID, task.ID, 0)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	mr.FastForward(2 * time.Second)
	// Expired files still get removed after restart and after disabling the UI.
	svc = newService()
	svc.SetWorkbenchSettingsResolver(func(context.Context) service.ImageWorkbenchRuntime {
		return service.ImageWorkbenchRuntime{Enabled: false}
	})
	require.NoError(t, svc.RunWorkbenchCleanup(ctx))
	require.NoFileExists(t, file)
	require.NoError(t, svc.RunWorkbenchCleanup(ctx))
	svc = newService()
	remote = nil
	deletedTask, err := svc.CreateWorkbench(ctx, owner, service.ImageWorkbenchMetadata{})
	require.NoError(t, err)
	require.NoError(t, svc.Complete(ctx, deletedTask.ID, 200, json.RawMessage(`{"data":[{"b64_json":"`+localTestPNG+`"}]}`)))
	require.NoError(t, svc.DeleteWorkbench(ctx, owner.UserID, deletedTask.ID))
	_, err = store.Get(ctx, deletedTask.ID)
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
	files, err := filepath.Glob(filepath.Join(directory, "images/workbench/users/*/*"))
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestWorkbenchLocalStorageUsesDataDirAndRejectsUnwritableLocation(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	t.Setenv("DATA_DIR", directory)
	storage := ProvideWorkbenchLocalStorage()
	require.NoError(t, storage.Ready(ctx))
	require.DirExists(t, filepath.Join(directory, "image-workbench"))
	file := filepath.Join(directory, "not-a-directory")
	require.NoError(t, os.WriteFile(file, []byte("test"), 0600))
	_, store, _, _ := newWorkbenchTestService(t)
	blocked := NewLocalImageStorage(filepath.Join(file, "images"))
	svc := service.NewImageTaskServiceWithResolver(store, func() (*service.ImageResultUploader, bool) { return nil, false }, 0, 0)
	svc.SetWorkbenchLocalStorage(blocked)
	require.False(t, svc.WorkbenchEnabled(ctx))
}
