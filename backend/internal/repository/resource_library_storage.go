package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// LocalResourceLibraryStorage keeps administrator-preserved files outside the
// workbench directories, so workbench retention sweeps never reach them.
type LocalResourceLibraryStorage struct {
	directory string
}

var _ service.ResourceLibraryStorage = (*LocalResourceLibraryStorage)(nil)

func NewLocalResourceLibraryStorage(directory string) *LocalResourceLibraryStorage {
	return &LocalResourceLibraryStorage{directory: directory}
}

func ProvideResourceLibraryStorage() service.ResourceLibraryStorage {
	directory := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if directory == "" {
		if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
			directory = "/app/data"
		} else {
			directory = "."
		}
	}
	return NewLocalResourceLibraryStorage(filepath.Join(directory, "resource-library"))
}

func (s *LocalResourceLibraryStorage) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.MkdirAll(s.directory, 0700)
}

func (s *LocalResourceLibraryStorage) root(ctx context.Context, id string) (*os.Root, error) {
	if !service.ValidResourceSavedID(id) {
		return nil, service.ErrResourceNotFound
	}
	if err := s.Ready(ctx); err != nil {
		return nil, err
	}
	return os.OpenRoot(s.directory)
}

// Write stores content once per id and returns its size and SHA-256 digest.
func (s *LocalResourceLibraryStorage) Write(ctx context.Context, id string, content io.Reader, maxBytes int64) (int64, string, error) {
	root, err := s.root(ctx, id)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = root.Close() }()
	if err := root.Mkdir(id, 0700); err != nil {
		return 0, "", err
	}
	success := false
	defer func() {
		if !success {
			_ = root.RemoveAll(id)
		}
	}()
	file, err := root.OpenFile(id+"/content.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, "", err
	}
	digest := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(&videoContextReader{ctx: ctx, reader: content}, maxBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		return 0, "", copyErr
	case syncErr != nil:
		return 0, "", syncErr
	case closeErr != nil:
		return 0, "", closeErr
	case count > maxBytes:
		return 0, "", fmt.Errorf("resource exceeds %d bytes", maxBytes)
	case count == 0:
		return 0, "", errors.New("resource is empty")
	}
	if err := root.Rename(id+"/content.tmp", id+"/content"); err != nil {
		return 0, "", err
	}
	success = true
	return count, hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *LocalResourceLibraryStorage) Open(ctx context.Context, id string) (io.ReadSeekCloser, error) {
	root, err := s.root(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(id + "/content")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, service.ErrResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (s *LocalResourceLibraryStorage) Delete(ctx context.Context, id string) error {
	root, err := s.root(ctx, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.RemoveAll(id)
}
