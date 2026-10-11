package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

// LocalImageStorage keeps private workbench images beneath one data directory.
// os.Root confines reads, writes and cleanup even if a path contains symlinks.
type LocalImageStorage struct {
	directory string
	mu        sync.Mutex
	ready     bool
}

var _ service.ImageWorkbenchLocalStorage = (*LocalImageStorage)(nil)

func NewLocalImageStorage(directory string) *LocalImageStorage {
	return &LocalImageStorage{directory: directory}
}

func ProvideWorkbenchLocalStorage() service.ImageWorkbenchLocalStorage {
	directory := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if directory == "" {
		if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
			directory = "/app/data"
		} else {
			directory = "."
		}
	}
	return NewLocalImageStorage(filepath.Join(directory, "image-workbench"))
}

func (s *LocalImageStorage) Ready(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.ready {
		return nil
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	probe := ".write-check-" + uuid.NewString()
	if err := root.WriteFile(probe, nil, 0600); err != nil {
		return err
	}
	if err := root.Remove(probe); err != nil {
		return err
	}
	s.ready = true
	return nil
}

func validLocalImageKey(key string) bool {
	return key != "" && key != "." && path.Clean(key) == key &&
		!strings.ContainsAny(key, "\\:\x00") && filepath.IsLocal(filepath.FromSlash(key))
}

func (s *LocalImageStorage) Save(ctx context.Context, key, _ string, data []byte) (string, error) {
	if !validLocalImageKey(key) {
		return "", errors.New("invalid local image key")
	}
	if !isLocalRasterImage(data) {
		return "", errors.New("unsupported local image content type")
	}
	if err := s.Ready(ctx); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(path.Dir(key), 0700); err != nil {
		return "", err
	}
	temporary := key + ".tmp-" + uuid.NewString()
	defer func() { _ = root.Remove(temporary) }()
	if err := root.WriteFile(temporary, data, 0600); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := root.Rename(temporary, key); err != nil {
		return "", err
	}
	return service.LocalImageURLPrefix + key, nil
}

func (s *LocalImageStorage) Read(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	if !validLocalImageKey(key) {
		return nil, "", errors.New("invalid local image key")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(key)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, "", errors.New("invalid local image file size or type")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("local image exceeds %d bytes", maxBytes)
	}
	if !isLocalRasterImage(data) {
		return nil, "", errors.New("unsupported local image content type")
	}
	return data, http.DetectContentType(data), ctx.Err()
}

func isLocalRasterImage(data []byte) bool {
	switch http.DetectContentType(data) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func (s *LocalImageStorage) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if !validLocalImageKey(prefix) {
		return 0, errors.New("invalid local image cleanup prefix")
	}
	root, err := os.OpenRoot(s.directory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	directory, base := path.Split(prefix)
	directory = strings.TrimSuffix(directory, "/")
	entries, err := fs.ReadDir(root.FS(), directory)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var deleted int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), base) {
			continue
		}
		if err := root.Remove(path.Join(directory, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
