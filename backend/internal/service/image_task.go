package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageTaskStatusQueued     = "queued"
	ImageTaskStatusProcessing = "processing"
	ImageTaskStatusCompleted  = "completed"
	ImageTaskStatusFailed     = "failed"

	defaultImageTaskTTL              = 24 * time.Hour
	defaultImageTaskExecutionTimeout = 30 * time.Minute
)

var (
	ErrImageTaskNotFound    = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_NOT_FOUND", "image task not found")
	ErrImageTaskForbidden   = infraerrors.New(http.StatusForbidden, "IMAGE_TASK_FORBIDDEN", "image task does not belong to this API key")
	ErrImageTaskUnavailable = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image task storage is unavailable")
)

// ImageTaskRecord is the private Redis representation of an asynchronous image
// request. Ownership fields are intentionally omitted from the public view.
type ImageTaskRecord struct {
	ID               string                  `json:"id"`
	UserID           int64                   `json:"user_id"`
	APIKeyID         int64                   `json:"api_key_id"`
	UserEmail        string                  `json:"user_email,omitempty"`
	Status           string                  `json:"status"`
	HTTPStatus       int                     `json:"http_status,omitempty"`
	Result           json.RawMessage         `json:"result,omitempty"`
	Error            json.RawMessage         `json:"error,omitempty"`
	CreatedAt        int64                   `json:"created_at"`
	StartedAt        *int64                  `json:"started_at,omitempty"`
	CompletedAt      *int64                  `json:"completed_at,omitempty"`
	ExpiresAt        int64                   `json:"expires_at"`
	Workbench        *ImageWorkbenchMetadata `json:"workbench,omitempty"`
	WorkbenchStorage string                  `json:"workbench_storage,omitempty"`
	QueueDeadline    int64                   `json:"queue_deadline,omitempty"`
	QueueLeaseUntil  int64                   `json:"queue_lease_until,omitempty"`
	// Admin-only provenance; never copied into the public ImageTask view.
	GroupID   int64  `json:"group_id,omitempty"`
	BillingID string `json:"billing_id,omitempty"`
}

// ImageTask is the API-safe task representation returned to callers.
type ImageTask struct {
	ID          string                  `json:"id"`
	TaskID      string                  `json:"task_id"`
	Object      string                  `json:"object"`
	Status      string                  `json:"status"`
	HTTPStatus  int                     `json:"http_status,omitempty"`
	ImageURL    string                  `json:"image_url,omitempty"`
	Result      json.RawMessage         `json:"result,omitempty"`
	Error       json.RawMessage         `json:"error,omitempty"`
	CreatedAt   int64                   `json:"created_at"`
	StartedAt   *int64                  `json:"started_at,omitempty"`
	CompletedAt *int64                  `json:"completed_at,omitempty"`
	ExpiresAt   int64                   `json:"expires_at"`
	Workbench   *ImageWorkbenchMetadata `json:"workbench,omitempty"`
}

type ImageTaskOwner struct {
	UserID    int64
	APIKeyID  int64
	UserEmail string
	IsAdmin   bool
	GroupID   int64
	BillingID string
}

type ImageTaskStore interface {
	Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
	Get(ctx context.Context, id string) (*ImageTaskRecord, error)
}

type ImageTaskFinishStore interface {
	Finish(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
}

// ImageTaskFinishResultStore returns whether the requested terminal state won
// atomically, including an already matching state. A successful write must not
// require a later read: its owner can immediately download and delete it.
type ImageTaskFinishResultStore interface {
	FinishWithResult(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) (bool, error)
}

// ImageStorageResolver reports the currently effective object-storage binding.
// It exists so the async image feature can be switched on and off from the admin
// UI without a restart: the wiring below is fixed at startup, but the answer to
// "is object storage configured right now" is re-read (and cached) per call.
type ImageStorageResolver func() (uploader *ImageResultUploader, enabled bool)

type ImageTaskService struct {
	store             ImageTaskStore
	uploader          *ImageResultUploader
	enabled           bool
	resolve           ImageStorageResolver
	ttl               time.Duration
	executionTimeout  time.Duration
	workbenchSettings func(context.Context) ImageWorkbenchRuntime
	workbenchLocal    ImageWorkbenchLocalStorage
	workbenchPayloads imageWorkbenchPayloadBudget
}

func NewImageTaskService(store ImageTaskStore) *ImageTaskService {
	return NewImageTaskServiceWithOptions(store, defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

func NewImageTaskServiceWithOptions(store ImageTaskStore, ttl, executionTimeout time.Duration) *ImageTaskService {
	if ttl <= 0 {
		ttl = defaultImageTaskTTL
	}
	if executionTimeout <= 0 {
		executionTimeout = defaultImageTaskExecutionTimeout
	}
	return &ImageTaskService{store: store, ttl: ttl, executionTimeout: executionTimeout}
}

// NewImageTaskServiceWithUploader 构造一个已启用的图片任务服务：结果会先经 uploader
// 转存到对象存储再落 Redis。uploader 为 nil 时不做转存（仅用于测试）。
func NewImageTaskServiceWithUploader(store ImageTaskStore, uploader *ImageResultUploader, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.uploader = uploader
	s.enabled = true
	return s
}

// NewImageTaskServiceWithResolver 构造一个由 resolver 决定启用状态的服务：
// 开关与凭证来自后台设置，保存后立即生效，无需重启。
func NewImageTaskServiceWithResolver(store ImageTaskStore, resolve ImageStorageResolver, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.resolve = resolve
	return s
}

// current 返回当前生效的 uploader 与启用状态。
// 注入了 resolver 时以 resolver 为准（后台设置可热切换），否则回落到构造时固定的值。
func (s *ImageTaskService) current() (*ImageResultUploader, bool) {
	if s == nil {
		return nil, false
	}
	if s.resolve != nil {
		return s.resolve()
	}
	return s.uploader, s.enabled
}

// Enabled 表示异步图片任务功能是否可用（总开关 + 凭证齐全）。
// 关闭时 handler 直接返回 404，不创建任务、不写 Redis。
func (s *ImageTaskService) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	_, enabled := s.current()
	return enabled
}

// Pollable 表示已创建的任务能否被查询。
// 比 Enabled 弱：只要 store 可用即可，从而在功能被关掉后仍能取回进行中的任务结果。
func (s *ImageTaskService) Pollable() bool {
	return s != nil && s.store != nil
}

func (s *ImageTaskService) ExecutionTimeout() time.Duration {
	if s == nil || s.executionTimeout <= 0 {
		return defaultImageTaskExecutionTimeout
	}
	return s.executionTimeout
}

func (s *ImageTaskService) Create(ctx context.Context, owner ImageTaskOwner) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	now := time.Now().UTC()
	task := &ImageTaskRecord{
		ID:        "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:    owner.UserID,
		APIKeyID:  owner.APIKeyID,
		UserEmail: NormalizeImageOwnerEmail(owner.UserEmail),
		Status:    ImageTaskStatusProcessing,
		CreatedAt: now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
		GroupID:   owner.GroupID,
		BillingID: owner.BillingID,
	}
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	ownerEmail := NormalizeImageOwnerEmail(owner.UserEmail)
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID ||
		(ownerEmail != "" && NormalizeImageOwnerEmail(task.UserEmail) != ownerEmail) || task.ExpiresAt <= time.Now().Unix() {
		// Do not reveal whether a random task ID exists for another caller.
		return nil, ErrImageTaskNotFound
	}
	if task.Workbench != nil && !s.WorkbenchRuntime(ctx).Enabled {
		return nil, ErrImageWorkbenchDisabled
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Complete(ctx context.Context, id string, statusCode int, result json.RawMessage) error {
	_, _, err := s.complete(ctx, id, statusCode, result)
	return err
}

// Deliver stores a successful generation result and reports whether its owner
// can retrieve it. Billing follows this answer, not the provider's success: a
// failed upload or task write leaves the owner without an image. A non-nil
// error means the stored state could not be determined.
func (s *ImageTaskService) Deliver(ctx context.Context, id string, statusCode int, result json.RawMessage) (bool, error) {
	attempted, expectedDelivery, err := s.complete(ctx, id, statusCode, result)
	if !attempted {
		if err != nil {
			logger.L().Warn("image_task.delivery_not_stored", zap.String("task_id", id), zap.Error(err))
		}
		return false, nil
	}
	if err == nil {
		return true, nil
	}
	// The completed record may have been written before its response was lost.
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if store, ok := s.store.(MediaWorkbenchDeliveryStore); ok && expectedDelivery != nil && expectedDelivery.BillingID != "" {
		receipt, receiptErr := store.GetMediaDelivery(checkCtx, id)
		if receiptErr != nil {
			return false, err
		}
		if receipt != nil {
			if *receipt != *expectedDelivery {
				return false, ErrUsageBillingRequestConflict
			}
			return true, nil
		}
	}
	task, getErr := s.store.Get(checkCtx, id)
	switch {
	case getErr == nil:
		return task.Status == ImageTaskStatusCompleted, nil
	case errors.Is(getErr, ErrImageTaskNotFound):
		return false, nil
	default:
		return false, err
	}
}

// complete reports true for a confirmed completion or an ambiguous terminal
// write, and preserves the original payment identity for delivery recovery.
func (s *ImageTaskService) complete(ctx context.Context, id string, statusCode int, result json.RawMessage) (bool, *MediaWorkbenchDeliveryReceipt, error) {
	if s == nil || s.store == nil {
		return false, nil, ErrImageTaskUnavailable
	}
	if !json.Valid(result) {
		return false, nil, s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "upstream returned a non-JSON image response"))
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		return false, nil, err
	}
	expectedDelivery := &MediaWorkbenchDeliveryReceipt{TaskID: task.ID, UserID: task.UserID, APIKeyID: task.APIKeyID, BillingID: task.BillingID}
	if task.Status == ImageTaskStatusCompleted || task.Status == ImageTaskStatusFailed {
		return task.Status == ImageTaskStatusCompleted, expectedDelivery, nil
	}
	uploader, enabled := s.current()
	if task.WorkbenchStorage == ImageWorkbenchStorageLocal {
		uploader, enabled = s.localWorkbenchUploader()
	}
	if s.resolve != nil && (!enabled || uploader == nil) {
		// Do not fall back to persisting provider base64 in Redis when storage is
		// disabled or incompletely resolved while an accepted task is still running.
		return false, expectedDelivery, s.Fail(ctx, id, http.StatusServiceUnavailable, imageTaskErrorJSON("api_error", "image storage is no longer available"))
	}
	if uploader != nil {
		if NormalizeImageOwnerEmail(task.UserEmail) == "" {
			return false, expectedDelivery, s.Fail(ctx, id, http.StatusForbidden, imageTaskErrorJSON("permission_error", "image task owner email is missing"))
		}
		if task.Workbench != nil {
			uploader = uploader.forWorkbench()
		}
		rewritten, err := uploader.RewriteForEmail(ctx, id, task.UserEmail, result)
		if err != nil {
			// 转存失败不回退存 base64，避免大 blob 撑爆 Redis：直接把任务标记为失败。
			logger.L().Error("image_task.offload_failed", zap.String("task_id", id), zap.Error(err))
			return false, expectedDelivery, s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "failed to store generated image"))
		}
		result = rewritten
	}
	stored, err := s.finish(ctx, id, ImageTaskStatusCompleted, statusCode, result, nil)
	return stored, expectedDelivery, err
}

func (s *ImageTaskService) Fail(ctx context.Context, id string, statusCode int, taskErr json.RawMessage) error {
	if !json.Valid(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	_, err := s.finish(ctx, id, ImageTaskStatusFailed, statusCode, nil, taskErr)
	return err
}

func (s *ImageTaskService) finish(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage) (bool, error) {
	if s == nil || s.store == nil {
		return false, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return false, ErrImageTaskNotFound
		}
		return false, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.Status == ImageTaskStatusCompleted || task.Status == ImageTaskStatusFailed {
		return task.Status == status, nil
	}
	now := time.Now().UTC()
	completedAt := now.Unix()
	task.Status = status
	task.HTTPStatus = statusCode
	task.Result = result
	task.Error = taskErr
	task.CompletedAt = &completedAt
	ttl := s.ttl
	if task.Workbench != nil {
		task.QueueDeadline = 0
		task.QueueLeaseUntil = 0
		if status == ImageTaskStatusFailed {
			// Classify before replacing: the raw payload is the only source that
			// names why the provider rejected the request.
			task.Error = workbenchPublicError(statusCode, taskErr)
		}
		ttl = s.WorkbenchRuntime(ctx).Retention()
	}
	task.ExpiresAt = now.Add(ttl).Unix()
	var saveErr error
	stored := true
	if store, ok := s.store.(ImageTaskFinishResultStore); ok {
		stored, saveErr = store.FinishWithResult(ctx, task, ttl)
	} else if store, ok := s.store.(ImageTaskFinishStore); ok {
		saveErr = store.Finish(ctx, task, ttl)
	} else {
		saveErr = s.store.Save(ctx, task, ttl)
	}
	if saveErr != nil {
		// A transport failure can hide a successful terminal write. Deliver
		// resolves the uncertain state instead of assuming it was rejected.
		return true, ErrImageTaskUnavailable.WithCause(saveErr)
	}
	return stored, nil
}

func imageTaskToPublic(task *ImageTaskRecord) *ImageTask {
	if task == nil {
		return nil
	}
	result := task.Result
	taskError := task.Error
	if task.Workbench != nil {
		result = workbenchPublicResult(task.ID, result)
		if task.Status == ImageTaskStatusFailed || len(taskError) > 0 {
			taskError = workbenchPublicError(task.HTTPStatus, task.Error)
		}
	}
	return &ImageTask{
		ID:          task.ID,
		TaskID:      task.ID,
		Object:      "image.generation.task",
		Status:      task.Status,
		HTTPStatus:  task.HTTPStatus,
		ImageURL:    firstImageTaskURL(result),
		Result:      result,
		Error:       taskError,
		CreatedAt:   task.CreatedAt,
		StartedAt:   task.StartedAt,
		CompletedAt: task.CompletedAt,
		ExpiresAt:   task.ExpiresAt,
		Workbench:   task.Workbench,
	}
}

func firstImageTaskURL(result json.RawMessage) string {
	if len(result) == 0 || !json.Valid(result) {
		return ""
	}
	var response struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil || len(response.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(response.Data[0].URL)
}

func imageTaskErrorJSON(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": errorType, "message": message})
	return data
}
