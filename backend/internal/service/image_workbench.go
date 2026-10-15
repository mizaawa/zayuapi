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
	"github.com/tidwall/gjson"
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
// Provider text is never forwarded: it may embed private hosts, credentials or
// internal task identifiers. An allowlisted upstream code is classified into
// one of the fixed public answers below, so a user can tell a rejected prompt
// from a channel that refuses this request, without learning anything about
// the upstream account behind it.
func workbenchPublicError(status int, raw ...json.RawMessage) json.RawMessage {
	errorType, code, message := workbenchPublicErrorIdentity(status)
	for _, payload := range raw {
		if len(payload) == 0 {
			continue
		}
		if publicType, publicCode, publicMessage, ok := classifyWorkbenchImageError(payload); ok {
			errorType, code, message = publicType, publicCode, publicMessage
			break
		}
	}
	data, _ := json.Marshal(map[string]string{"type": errorType, "code": code, "message": message})
	return data
}

// workbenchPublicErrorIdentity keeps the published status-based answer.
func workbenchPublicErrorIdentity(status int) (string, string, string) {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "invalid_request_error", "request_rejected", "image generation request was rejected; check the prompt and image settings"
	case http.StatusTooManyRequests:
		return "rate_limit_error", "rate_limited", "image generation is busy; try again later"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "timeout_error", "timeout", "image generation timed out; try again later"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "permission_error", "authorization_failed", "image generation is unavailable for this request; contact the administrator"
	default:
		return "api_error", "generation_failed", "image generation failed; try again later or contact the administrator"
	}
}

// classifyWorkbenchImageError maps an allowlisted upstream error code, or a
// provider message carrying a known cause, onto a fixed public answer. Values
// outside these lists fall through so callers keep the status-based answer.
func classifyWorkbenchImageError(payload json.RawMessage) (string, string, string, bool) {
	for _, path := range []string{"code", "error.code"} {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, path).String())) {
		case "invalid_image_input", "invalid_image", "invalid_image_format", "image_too_large", "image_parse_error":
			return "invalid_request_error", "invalid_image_input", "the channel rejected the prompt or the reference image; retry, change the model, or contact the administrator", true
		case "moderation_blocked", "content_policy_violation", "content_rejected", "safety_system_rejected":
			return "invalid_request_error", "content_rejected", "the prompt or reference image was rejected by the channel's content moderation; revise it and retry", true
		case "invalid_size", "unsupported_size", "invalid_image_size":
			return "invalid_request_error", "unsupported_size", "the channel does not support this size; change the resolution or aspect ratio and retry", true
		case "invalid_parameter", "invalid_parameters", "invalid_request_parameter":
			return "invalid_request_error", "invalid_parameters", "the channel rejected a request parameter; lower the quality or resolution and retry", true
		case "model_not_found", "model_not_available", "model_not_exist", "unknown_model":
			return "invalid_request_error", "model_unavailable", "this model is unavailable on the channel serving this group; pick another model or contact the administrator", true
		case "insufficient_quota", "insufficient_balance", "quota_exceeded", "billing_hard_limit_reached":
			return "invalid_request_error", "insufficient_balance", "the quota or balance for this request is exhausted; contact the administrator", true
		case "rate_limit_exceeded", "too_many_requests":
			return "rate_limit_error", "rate_limited", "image generation is busy; try again later", true
		}
	}
	for _, path := range []string{"type", "error.type"} {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, path).String())) {
		case "moderation_blocked", "content_policy_violation":
			return "invalid_request_error", "content_rejected", "the prompt or reference image was rejected by the channel's content moderation; revise it and retry", true
		}
	}
	for _, path := range []string{"message", "error.message", "error"} {
		value := gjson.GetBytes(payload, path)
		if value.Type != gjson.String {
			continue
		}
		lower := strings.ToLower(value.String())
		for _, rule := range []struct {
			errorType string
			code      string
			message   string
			phrases   []string
		}{
			{"invalid_request_error", "content_rejected", "the prompt or reference image was rejected by the channel's content moderation; revise it and retry",
				[]string{"content policy", "moderation", "safety system", "内容审核"}},
			{"invalid_request_error", "model_unavailable", "this model is unavailable on the channel serving this group; pick another model or contact the administrator",
				[]string{"model not found", "unknown model", "no such model", "模型不存在", "没有该模型"}},
			{"invalid_request_error", "insufficient_balance", "the quota or balance for this request is exhausted; contact the administrator",
				[]string{"insufficient balance", "insufficient quota", "余额不足", "额度不足"}},
			{"invalid_request_error", "invalid_image_input", "the channel rejected the prompt or the reference image; retry, change the model, or contact the administrator",
				[]string{"invalid_image_input", "invalid image"}},
			{"invalid_request_error", "unsupported_size", "the channel does not support this size; change the resolution or aspect ratio and retry",
				[]string{"unsupported size", "invalid size", "分辨率不支持"}},
		} {
			for _, phrase := range rule.phrases {
				if strings.Contains(lower, phrase) {
					return rule.errorType, rule.code, rule.message, true
				}
			}
		}
	}
	return "", "", "", false
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
	return s.deleteWorkbenchTask(ctx, store, task)
}

// deleteWorkbenchTask removes one authorized task. Callers have already
// established ownership (user path) or administrator access (resource library).
func (s *ImageTaskService) deleteWorkbenchTask(ctx context.Context, store ImageWorkbenchStore, task *ImageTaskRecord) error {
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
	return s.readWorkbenchImage(ctx, task, index)
}

func (s *ImageTaskService) readWorkbenchImage(ctx context.Context, task *ImageTaskRecord, index int) ([]byte, string, error) {
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
		GroupID:         owner.GroupID,
		BillingID:       owner.BillingID,
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
