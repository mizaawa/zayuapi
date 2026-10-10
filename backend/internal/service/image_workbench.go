package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageWorkbenchMaxConcurrent = 5
	ImageWorkbenchRetention     = 15 * time.Minute
)

var ErrImageWorkbenchLimit = infraerrors.New(http.StatusTooManyRequests, "IMAGE_WORKBENCH_LIMIT", "image workbench concurrency limit reached")
var ErrImageWorkbenchDisabled = infraerrors.New(http.StatusNotFound, "IMAGE_WORKBENCH_DISABLED", "image workbench is disabled")

func (s *ImageTaskService) SetWorkbenchSettingsResolver(resolve func(context.Context) ImageWorkbenchRuntime) {
	s.workbenchSettings = resolve
}

func (s *ImageTaskService) WorkbenchRuntime(ctx context.Context) ImageWorkbenchRuntime {
	if s != nil && s.workbenchSettings != nil {
		return s.workbenchSettings(ctx)
	}
	return parseImageWorkbenchRuntime(nil)
}

// Only generation settings are retained; reference images and credentials never
// enter the task index or metadata returned by the panel API.
type ImageWorkbenchMetadata struct {
	APIKeyID int64  `json:"api_key_id"`
	Prompt   string `json:"prompt"`
	Model    string `json:"model"`
	Quality  string `json:"quality"`
	Size     string `json:"size"`
	Count    int    `json:"count"`
}

type ImageWorkbenchCleanup struct {
	ID        string `json:"id"`
	UserEmail string `json:"user_email"`
}

type ImageWorkbenchStore interface {
	CreateWorkbench(ctx context.Context, task *ImageTaskRecord, ttl time.Duration, limit int) error
	ListWorkbench(ctx context.Context, userID int64, now time.Time) ([]*ImageTaskRecord, error)
	ExpiredWorkbench(ctx context.Context, now time.Time) ([]ImageWorkbenchCleanup, error)
	RemoveWorkbenchCleanup(ctx context.Context, task ImageWorkbenchCleanup) error
	DeleteWorkbench(ctx context.Context, task *ImageTaskRecord) error
}

func (s *ImageTaskService) DeleteWorkbench(ctx context.Context, userID int64, id string) error {
	if !s.WorkbenchRuntime(ctx).Enabled {
		return ErrImageWorkbenchDisabled
	}
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if task.UserID != userID || task.Workbench == nil {
		return ErrImageTaskNotFound
	}
	if task.Status == ImageTaskStatusProcessing {
		return infraerrors.New(http.StatusConflict, "IMAGE_TASK_PROCESSING", "running image tasks cannot be deleted")
	}
	// Keep the durable cleanup entry if storage is temporarily unavailable.
	uploader, _ := s.current()
	if uploader != nil {
		if err := uploader.forWorkbench().deleteTask(ctx, task.ID, task.UserEmail); err != nil {
			return ErrImageTaskUnavailable.WithCause(err)
		}
		if err := store.RemoveWorkbenchCleanup(ctx, ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail}); err != nil {
			return ErrImageTaskUnavailable.WithCause(err)
		}
	}
	return store.DeleteWorkbench(ctx, task)
}

func (s *ImageTaskService) DownloadWorkbenchImage(ctx context.Context, userID int64, id string, index int) ([]byte, string, error) {
	if !s.WorkbenchRuntime(ctx).Enabled {
		return nil, "", ErrImageWorkbenchDisabled
	}
	if s == nil || s.store == nil {
		return nil, "", ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, "", err
	}
	if task.UserID != userID || task.Workbench == nil || task.ExpiresAt <= time.Now().Unix() || task.Status != ImageTaskStatusCompleted {
		return nil, "", ErrImageTaskNotFound
	}
	var result struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(task.Result, &result) != nil || index < 0 || index >= len(result.Data) || result.Data[index].URL == "" {
		return nil, "", ErrImageTaskNotFound
	}
	uploader, _ := s.current()
	if uploader == nil {
		return nil, "", ErrImageTaskUnavailable
	}
	data, contentType, err := uploader.download(ctx, result.Data[index].URL)
	if err != nil {
		return nil, "", ErrImageTaskUnavailable.WithCause(err)
	}
	return data, contentType, nil
}

func (s *ImageTaskService) CreateWorkbench(ctx context.Context, owner ImageTaskOwner, metadata ImageWorkbenchMetadata) (*ImageTask, error) {
	policy := s.WorkbenchRuntime(ctx)
	if !policy.Enabled {
		return nil, ErrImageWorkbenchDisabled
	}
	if !s.Enabled() {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	now := time.Now().UTC()
	metadata.APIKeyID = owner.APIKeyID
	ttl := s.ExecutionTimeout() + policy.Retention()
	task := &ImageTaskRecord{
		ID:        "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:    owner.UserID,
		APIKeyID:  owner.APIKeyID,
		UserEmail: NormalizeImageOwnerEmail(owner.UserEmail),
		Status:    ImageTaskStatusProcessing,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		Workbench: &metadata,
	}
	if err := store.CreateWorkbench(ctx, task, ttl, policy.Limit(owner.IsAdmin)); err != nil {
		if errors.Is(err, ErrImageWorkbenchLimit) {
			return nil, err
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) ListWorkbench(ctx context.Context, userID int64) ([]*ImageTask, error) {
	if !s.WorkbenchRuntime(ctx).Enabled {
		return nil, ErrImageWorkbenchDisabled
	}
	if s == nil || s.store == nil || userID <= 0 {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	tasks, err := store.ListWorkbench(ctx, userID, time.Now().UTC())
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	result := make([]*ImageTask, 0, len(tasks))
	for _, task := range tasks {
		if task.UserID == userID && task.Workbench != nil && task.ExpiresAt > time.Now().Unix() {
			result = append(result, imageTaskToPublic(task))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

// The cleanup index outlives result records so expiry is enforced after a
// restart, even when the task itself has already expired from Redis.
func (s *ImageTaskService) RunWorkbenchCleanup(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return nil
	}
	uploader, _ := s.current()
	if uploader == nil {
		return nil
	}
	expired, err := store.ExpiredWorkbench(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, task := range expired {
		if err := uploader.forWorkbench().deleteTask(ctx, task.ID, task.UserEmail); err != nil {
			return err
		}
		if err := store.RemoveWorkbenchCleanup(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func (s *ImageTaskService) startWorkbenchCleanup() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			err := s.RunWorkbenchCleanup(ctx)
			cancel()
			if err != nil {
				logger.L().Warn("image_workbench.cleanup_failed", zap.Error(err))
			}
		}
	}()
}
