package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	ImageWorkbenchStorageLocal  = "local"
)

var ErrImageWorkbenchDisabled = infraerrors.New(http.StatusNotFound, "IMAGE_WORKBENCH_DISABLED", "image workbench is disabled")
var ErrImageTaskProcessing = infraerrors.New(http.StatusConflict, "IMAGE_TASK_PROCESSING", "running image tasks cannot be deleted")

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
	Storage   string `json:"storage,omitempty"`
}

func (s *ImageTaskService) SetWorkbenchLocalStorage(storage ImageWorkbenchLocalStorage) {
	s.workbenchLocal = storage
}

func (s *ImageTaskService) localWorkbenchUploader() (*ImageResultUploader, bool) {
	if s == nil || s.workbenchLocal == nil {
		return nil, false
	}
	return NewImageResultUploader(s.workbenchLocal, "images/", 0, nil), true
}

// WorkbenchEnabled permits local storage when no object store is configured.
func (s *ImageTaskService) WorkbenchEnabled(ctx context.Context) bool {
	if s == nil || s.store == nil || !s.WorkbenchRuntime(ctx).Enabled {
		return false
	}
	if s.Enabled() {
		return true
	}
	if s.workbenchLocal == nil {
		return false
	}
	if err := s.workbenchLocal.Ready(ctx); err != nil {
		logger.L().Warn("image_workbench.local_storage_unavailable", zap.Error(err))
		return false
	}
	return true
}

type imageWorkbenchResult struct {
	Data []struct {
		URL         string `json:"url"`
		SizeBytes   int64  `json:"size_bytes,omitempty"`
		ContentType string `json:"content_type,omitempty"`
	} `json:"data"`
}

func workbenchPublicResult(taskID string, result json.RawMessage) json.RawMessage {
	var body imageWorkbenchResult
	if json.Unmarshal(result, &body) != nil || len(body.Data) == 0 || len(body.Data) > 20 {
		return nil
	}
	for index := range body.Data {
		item := &body.Data[index]
		item.URL = fmt.Sprintf("/api/v1/image-workbench/tasks/%s/images/%d", taskID, index)
		if !supportedImageContentType(item.ContentType) {
			item.ContentType = ""
		}
		if item.SizeBytes < 0 {
			item.SizeBytes = 0
		}
	}
	result, _ = json.Marshal(body)
	return result
}

// Use local messages only, including for records created before this policy.
func workbenchPublicError(status int) json.RawMessage {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return imageTaskErrorJSON("invalid_request_error", "image generation request was rejected; check the prompt and image settings")
	case http.StatusTooManyRequests:
		return imageTaskErrorJSON("rate_limit_error", "image generation is busy; try again later")
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return imageTaskErrorJSON("timeout_error", "image generation timed out; try again later")
	case http.StatusUnauthorized, http.StatusForbidden:
		return imageTaskErrorJSON("permission_error", "image generation is unavailable for this request; contact the administrator")
	default:
		return imageTaskErrorJSON("api_error", "image generation failed; try again later or contact the administrator")
	}
}

func (s *ImageTaskService) cleanupWorkbenchImages(ctx context.Context, task ImageWorkbenchCleanup) (bool, error) {
	uploader, _ := s.current()
	if task.Storage == ImageWorkbenchStorageLocal {
		uploader, _ = s.localWorkbenchUploader()
	}
	if uploader == nil {
		return false, nil
	}
	return true, uploader.forWorkbench().deleteTask(ctx, task.ID, task.UserEmail)
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
	if task.UserID != userID || task.Workbench == nil || task.ExpiresAt <= time.Now().Unix() {
		return ErrImageTaskNotFound
	}
	if task.Status == ImageTaskStatusProcessing {
		return ErrImageTaskProcessing
	}
	if task.Status == ImageTaskStatusQueued {
		// The store checks status atomically against a concurrent execution claim.
		if err := store.DeleteWorkbench(ctx, task); err != nil {
			return err
		}
		return store.RemoveWorkbenchCleanup(ctx, ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail, Storage: task.WorkbenchStorage})
	}
	// Keep the durable cleanup entry if storage is temporarily unavailable.
	cleanup := ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail, Storage: task.WorkbenchStorage}
	cleaned, err := s.cleanupWorkbenchImages(ctx, cleanup)
	if err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if cleaned {
		if err := store.RemoveWorkbenchCleanup(ctx, cleanup); err != nil {
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
	var result imageWorkbenchResult
	if json.Unmarshal(task.Result, &result) != nil || index < 0 || index >= len(result.Data) || result.Data[index].URL == "" {
		return nil, "", ErrImageTaskNotFound
	}
	uploader, _ := s.current()
	if task.WorkbenchStorage == ImageWorkbenchStorageLocal {
		uploader, _ = s.localWorkbenchUploader()
	}
	if uploader == nil {
		return nil, "", ErrImageTaskUnavailable
	}
	reader, ok := uploader.storage.(ImageStorageReader)
	if !ok || !supportedImageContentType(result.Data[index].ContentType) {
		return nil, "", ErrImageTaskUnavailable
	}
	// Derive the key from authenticated task ownership. Neither upstream URLs
	// nor client-supplied paths participate in reading a managed object.
	key := uploader.forWorkbench().buildKey(task.ID, task.UserEmail, index, result.Data[index].ContentType)
	data, _, err := reader.Read(ctx, key, uploader.maxDownloadBytes)
	if err != nil {
		return nil, "", ErrImageTaskUnavailable.WithCause(err)
	}
	contentType := detectedImageContentType(data)
	if contentType == "" || int64(len(data)) > uploader.maxDownloadBytes {
		return nil, "", ErrImageTaskUnavailable
	}
	return data, contentType, nil
}

func (s *ImageTaskService) CreateWorkbench(ctx context.Context, owner ImageTaskOwner, metadata ImageWorkbenchMetadata) (*ImageTask, error) {
	policy := s.WorkbenchRuntime(ctx)
	if !policy.Enabled {
		return nil, ErrImageWorkbenchDisabled
	}
	if !s.WorkbenchEnabled(ctx) {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(ImageWorkbenchStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	now := time.Now().UTC()
	metadata.APIKeyID = owner.APIKeyID
	ttl := ImageWorkbenchQueueTimeout + policy.Retention()
	task := &ImageTaskRecord{
		ID:              "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:          owner.UserID,
		APIKeyID:        owner.APIKeyID,
		UserEmail:       NormalizeImageOwnerEmail(owner.UserEmail),
		Status:          ImageTaskStatusQueued,
		CreatedAt:       now.Unix(),
		ExpiresAt:       now.Add(ttl).Unix(),
		Workbench:       &metadata,
		QueueDeadline:   now.Add(ImageWorkbenchQueueTimeout).Unix(),
		QueueLeaseUntil: now.Add(ImageWorkbenchQueueLease).Unix(),
	}
	if !s.Enabled() {
		task.WorkbenchStorage = ImageWorkbenchStorageLocal
	}
	if err := store.CreateWorkbench(ctx, task, ttl, ImageWorkbenchQueueLimit); err != nil {
		if errors.Is(err, ErrImageWorkbenchQueueFull) {
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
	if err := s.expireWorkbenchQueue(ctx); err != nil {
		return err
	}
	expired, err := store.ExpiredWorkbench(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, task := range expired {
		cleaned, err := s.cleanupWorkbenchImages(ctx, task)
		if err != nil {
			return err
		}
		if !cleaned {
			continue
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
