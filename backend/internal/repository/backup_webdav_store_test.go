package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestWebDAVBackupStoreUploadDownloadDeleteAndConnectionTest(t *testing.T) {
	const username = "backup-user"
	const password = "backup-password"
	const expectedRoot = "/remote.php/dav/files/backup-user"
	const expectedPrefix = expectedRoot + "/database/backups/"
	var mu sync.Mutex
	objects := make(map[string][]byte)
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, ok := r.BasicAuth()
		if !ok || gotUser != username || gotPassword != password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, expectedRoot+"/") {
			http.Error(w, "wrong path", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodPut:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			if got := r.Header.Get("Content-Type"); got != "application/gzip" && got != "application/octet-stream" {
				http.Error(w, "wrong content type", http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(r.Body)
			mu.Lock()
			objects[strings.TrimPrefix(r.URL.Path, expectedPrefix)] = data
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			key := strings.TrimPrefix(r.URL.Path, expectedPrefix)
			mu.Lock()
			data, found := objects[key]
			mu.Unlock()
			if !found {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		case http.MethodDelete:
			if !strings.HasPrefix(r.URL.Path, expectedPrefix) {
				http.Error(w, "wrong object path", http.StatusBadRequest)
				return
			}
			key := strings.TrimPrefix(r.URL.Path, expectedPrefix)
			mu.Lock()
			delete(objects, key)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	factory := NewS3BackupStoreFactory()
	store, err := factory(context.Background(), &service.BackupS3Config{
		StorageType:    "webdav",
		WebDAVURL:      server.URL + "/remote.php/dav/files/backup-user",
		WebDAVUsername: username,
		WebDAVPassword: password,
		WebDAVPath:     "database/backups",
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "2026/10/01/dump.sql.gz"
	content := []byte("compressed database bytes")
	size, err := store.Upload(ctx, key, strings.NewReader(string(content)), "application/gzip")
	require.NoError(t, err)
	require.Equal(t, int64(len(content)), size)
	body, err := store.Download(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, content, got)
	require.NoError(t, store.Delete(ctx, key))

	// The same authenticated adapter is used by the configuration test endpoint.
	require.NoError(t, store.HeadBucket(ctx))
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, methods)
	require.Contains(t, methods, "MKCOL")
	require.Contains(t, methods, http.MethodPut)
	require.Contains(t, methods, http.MethodGet)
	require.Contains(t, methods, http.MethodDelete)
	require.Empty(t, objects)
}

func TestNewWebDAVBackupStoreRejectsUnsafeURLAndPath(t *testing.T) {
	tests := []service.BackupS3Config{
		{WebDAVURL: "https://user:password@dav.example.test/dav", WebDAVUsername: "u", WebDAVPassword: "p"},
		{WebDAVURL: "https://dav.example.test/dav?token=x", WebDAVUsername: "u", WebDAVPassword: "p"},
		{WebDAVURL: "https://dav.example.test/dav", WebDAVUsername: "u", WebDAVPassword: "p", WebDAVPath: "backups/../private"},
	}
	for _, cfg := range tests {
		_, err := newWebDAVBackupStore(&cfg)
		require.Error(t, err)
	}
}
