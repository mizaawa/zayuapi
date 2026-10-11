package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type LocalVideoWorkbenchStorage struct{ directory string }

func NewLocalVideoWorkbenchStorage(directory string) *LocalVideoWorkbenchStorage {
	return &LocalVideoWorkbenchStorage{directory: directory}
}

func ProvideVideoWorkbenchLocalStorage() service.VideoWorkbenchLocalStorage {
	directory := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if directory == "" {
		if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
			directory = "/app/data"
		} else {
			directory = "."
		}
	}
	return NewLocalVideoWorkbenchStorage(filepath.Join(directory, "video-workbench"))
}

func (s *LocalVideoWorkbenchStorage) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.MkdirAll(s.directory, 0700)
}

func (s *LocalVideoWorkbenchStorage) root(ctx context.Context, id string) (*os.Root, error) {
	if !service.ValidVideoWorkbenchTaskID(id) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err := s.Ready(ctx); err != nil {
		return nil, err
	}
	return os.OpenRoot(s.directory)
}

func (s *LocalVideoWorkbenchStorage) Write(ctx context.Context, id string, input io.Reader, retention time.Duration) (int64, string, error) {
	root, err := s.root(ctx, id)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = root.Close() }()
	reader := bufio.NewReader(input)
	head, err := reader.Peek(512)
	if err != nil && err != io.EOF {
		return 0, "", err
	}
	contentType := http.DetectContentType(head)
	if contentType != "video/mp4" && contentType != "video/webm" && contentType != "video/quicktime" {
		return 0, "", errors.New("upstream content is not a supported video")
	}
	if err := root.Mkdir(id, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return 0, "", err
	}
	success := false
	defer func() {
		if !success {
			_ = root.RemoveAll(id)
		}
	}()
	// The first marker also covers crashes during a large download.
	marker, _ := json.Marshal(time.Now().Add(service.VideoWorkbenchExecutionTimeout).Unix())
	if err := root.WriteFile(id+"/expires.json", marker, 0600); err != nil {
		return 0, "", err
	}
	file, err := root.OpenFile(id+"/video.tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, "", err
	}
	count, copyErr := io.Copy(file, &videoContextReader{ctx: ctx, reader: reader})
	closeErr := file.Close()
	if copyErr != nil {
		return 0, "", copyErr
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	if err := root.Rename(id+"/video.tmp", id+"/video"); err != nil {
		return 0, "", err
	}
	marker, _ = json.Marshal(time.Now().Add(retention).Unix())
	if err := root.WriteFile(id+"/expires.tmp", marker, 0600); err != nil {
		return 0, "", err
	}
	if err := root.Rename(id+"/expires.tmp", id+"/expires.json"); err != nil {
		return 0, "", err
	}
	success = true
	return count, contentType, nil
}

type videoContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *videoContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *LocalVideoWorkbenchStorage) Open(ctx context.Context, id string) (io.ReadSeekCloser, error) {
	root, err := s.root(ctx, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	marker, err := root.ReadFile(id + "/expires.json")
	var expires int64
	if err != nil || json.Unmarshal(marker, &expires) != nil || expires <= time.Now().Unix() {
		return nil, service.ErrVideoTaskNotFound
	}
	return root.Open(id + "/video")
}

func (s *LocalVideoWorkbenchStorage) Delete(ctx context.Context, id string) error {
	root, err := s.root(ctx, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.RemoveAll(id)
}

func (s *LocalVideoWorkbenchStorage) Sweep(ctx context.Context, now time.Time) error {
	if err := s.Ready(ctx); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !service.ValidVideoWorkbenchTaskID(entry.Name()) {
			continue
		}
		marker, readErr := root.ReadFile(entry.Name() + "/expires.json")
		var expires int64
		if readErr != nil || json.Unmarshal(marker, &expires) != nil {
			info, err := entry.Info()
			if err != nil || now.Sub(info.ModTime()) < service.VideoWorkbenchExecutionTimeout {
				continue
			}
		} else if expires > now.Unix() {
			continue
		}
		if err := root.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}
