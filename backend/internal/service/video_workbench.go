package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	VideoWorkbenchQueueTimeout     = 30 * time.Minute
	VideoWorkbenchExecutionTimeout = 2 * time.Hour
	VideoWorkbenchLease            = 90 * time.Second
	VideoWorkbenchQueueLimit       = 100
	VideoWorkbenchMaxRequestBytes  = 64 << 20
)

var (
	ErrVideoWorkbenchDisabled = infraerrors.New(http.StatusNotFound, "VIDEO_WORKBENCH_DISABLED", "video workbench is disabled")
	ErrVideoTaskNotFound      = infraerrors.New(http.StatusNotFound, "VIDEO_TASK_NOT_FOUND", "video task not found")
	ErrVideoTaskForbidden     = infraerrors.New(http.StatusForbidden, "VIDEO_WORKBENCH_FORBIDDEN", "this API key is not available for the video workbench")
	ErrVideoTaskUnavailable   = infraerrors.New(http.StatusServiceUnavailable, "VIDEO_WORKBENCH_UNAVAILABLE", "video workbench is temporarily unavailable")
	ErrVideoTaskProcessing    = infraerrors.New(http.StatusConflict, "VIDEO_TASK_PROCESSING", "running video tasks cannot be deleted")
	ErrVideoQueueFull         = infraerrors.New(http.StatusTooManyRequests, "VIDEO_WORKBENCH_QUEUE_FULL", "video workbench queue is full; try again later")
)

type videoWorkbenchContextKey struct{}

func GroupAllowsVideoWorkbench(group *Group) bool {
	return group != nil && group.VideoWorkbenchEnabled && NormalizeGroupPlatform(group.Platform) == PlatformComposite
}

func WithVideoWorkbenchContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, videoWorkbenchContextKey{}, true)
}

func videoMediaUpstreamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil && ctx.Value(videoWorkbenchContextKey{}) == true {
		// A queued task already outlives its client; its execution deadline must
		// still cancel a stalled upstream request or download.
		return ctx, func() {}
	}
	return detachUpstreamContext(ctx)
}

type VideoWorkbenchMetadata struct {
	APIKeyID        int64  `json:"api_key_id"`
	Prompt          string `json:"prompt"`
	Model           string `json:"model"`
	Ratio           string `json:"ratio"`
	Duration        int    `json:"duration"`
	ReferenceImages int    `json:"reference_images"`
	ReferenceVideos int    `json:"reference_videos"`
	ReferenceAudios int    `json:"reference_audios"`
}

type VideoWorkbenchTask struct {
	ID          string                 `json:"id"`
	Status      string                 `json:"status"`
	CreatedAt   int64                  `json:"created_at"`
	StartedAt   int64                  `json:"started_at,omitempty"`
	CompletedAt int64                  `json:"completed_at,omitempty"`
	ExpiresAt   int64                  `json:"expires_at"`
	Workbench   VideoWorkbenchMetadata `json:"workbench"`
	SizeBytes   int64                  `json:"size_bytes,omitempty"`
	ContentType string                 `json:"content_type,omitempty"`
	ErrorCode   string                 `json:"error_code,omitempty"`
}

// The index contains neither API credentials nor uploaded reference data.
type VideoWorkbenchRecord struct {
	VideoWorkbenchTask
	UserID     int64 `json:"user_id"`
	LeaseUntil int64 `json:"lease_until"`
	Deadline   int64 `json:"deadline"`
}

type VideoWorkbenchStore interface {
	Create(context.Context, *VideoWorkbenchRecord, time.Duration) error
	Get(context.Context, string) (*VideoWorkbenchRecord, error)
	List(context.Context, int64, time.Time) ([]*VideoWorkbenchRecord, error)
	TryStart(context.Context, *VideoWorkbenchRecord, time.Time, time.Duration, int) (bool, error)
	Heartbeat(context.Context, *VideoWorkbenchRecord, time.Time) error
	Finish(context.Context, *VideoWorkbenchRecord, time.Duration, int64) (bool, error)
	Delete(context.Context, *VideoWorkbenchRecord, time.Time) error
	ExpiredLeases(context.Context, time.Time) ([]*VideoWorkbenchRecord, error)
	ExpiredFiles(context.Context, time.Time) ([]string, error)
	AcknowledgeCleanup(context.Context, string) error
}

type VideoWorkbenchLocalStorage interface {
	Ready(context.Context) error
	Write(context.Context, string, io.Reader, time.Duration) (int64, string, error)
	Open(context.Context, string) (io.ReadSeekCloser, error)
	Delete(context.Context, string) error
	Sweep(context.Context, time.Time) error
}

type VideoWorkbenchService struct {
	store    VideoWorkbenchStore
	storage  VideoWorkbenchLocalStorage
	settings func(context.Context) VideoWorkbenchRuntime
	payloads imageWorkbenchPayloadBudget
}

func NewVideoWorkbenchService(store VideoWorkbenchStore, storage VideoWorkbenchLocalStorage, settings *SettingService) *VideoWorkbenchService {
	s := NewVideoWorkbenchServiceWithSettings(store, storage, settings.GetVideoWorkbenchRuntime)
	go s.cleanupLoop()
	return s
}

func NewVideoWorkbenchServiceWithSettings(store VideoWorkbenchStore, storage VideoWorkbenchLocalStorage, settings func(context.Context) VideoWorkbenchRuntime) *VideoWorkbenchService {
	return &VideoWorkbenchService{store: store, storage: storage, settings: settings}
}

func (s *VideoWorkbenchService) Runtime(ctx context.Context) VideoWorkbenchRuntime {
	if s != nil && s.settings != nil {
		return s.settings(ctx)
	}
	return parseVideoWorkbenchRuntime(nil)
}

func (s *VideoWorkbenchService) ReservePayload(size int) (func(), error) {
	// Apply the same process memory budget as the image queue, independently.
	s.payloads.mu.Lock()
	defer s.payloads.mu.Unlock()
	if size < 0 || s.payloads.count >= workbenchPayloadLimit || int64(size) > workbenchPayloadBytes-s.payloads.bytes {
		return nil, ErrVideoQueueFull
	}
	s.payloads.count++
	s.payloads.bytes += int64(size)
	return sync.OnceFunc(func() {
		s.payloads.mu.Lock()
		defer s.payloads.mu.Unlock()
		s.payloads.count--
		s.payloads.bytes -= int64(size)
	}), nil
}

func (s *VideoWorkbenchService) Create(ctx context.Context, userID int64, metadata VideoWorkbenchMetadata) (*VideoWorkbenchRecord, error) {
	p := s.Runtime(ctx)
	if !p.Enabled {
		return nil, ErrVideoWorkbenchDisabled
	}
	if s.store == nil || s.storage == nil || userID <= 0 {
		return nil, ErrVideoTaskUnavailable
	}
	if err := s.storage.Ready(ctx); err != nil {
		return nil, ErrVideoTaskUnavailable.WithCause(err)
	}
	now := time.Now().UTC()
	ttl := VideoWorkbenchQueueTimeout + VideoWorkbenchExecutionTimeout + p.Retention()
	task := &VideoWorkbenchRecord{
		VideoWorkbenchTask: VideoWorkbenchTask{
			ID:     "vidtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			Status: "queued", CreatedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(), Workbench: metadata,
		},
		UserID: userID, LeaseUntil: now.Add(VideoWorkbenchLease).Unix(), Deadline: now.Add(VideoWorkbenchQueueTimeout).Unix(),
	}
	if err := s.store.Create(ctx, task, ttl); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *VideoWorkbenchService) AwaitSlot(ctx context.Context, task *VideoWorkbenchRecord) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		policy := s.Runtime(ctx)
		if !policy.Enabled {
			return ErrVideoWorkbenchDisabled
		}
		now := time.Now().UTC()
		if now.Unix() >= task.Deadline {
			return context.DeadlineExceeded
		}
		started, err := s.store.TryStart(ctx, task, now, policy.Retention(), policy.MaxConcurrent)
		if err != nil {
			return err
		}
		if started {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *VideoWorkbenchService) Heartbeat(ctx context.Context, task *VideoWorkbenchRecord) error {
	return s.store.Heartbeat(ctx, task, time.Now().UTC())
}

func (s *VideoWorkbenchService) Complete(ctx context.Context, task *VideoWorkbenchRecord, content io.Reader) error {
	retention := s.Runtime(ctx).Retention()
	size, contentType, err := s.storage.Write(ctx, task.ID, content, retention)
	if err != nil {
		return err
	}
	finished := *task
	finished.Status, finished.SizeBytes, finished.ContentType = "completed", size, contentType
	ok, err := s.finishWithRetention(ctx, &finished, 0, retention)
	if err != nil || !ok {
		_ = s.storage.Delete(context.WithoutCancel(ctx), task.ID)
		if err == nil {
			err = ErrVideoTaskNotFound
		}
	}
	return err
}

func (s *VideoWorkbenchService) Fail(ctx context.Context, task *VideoWorkbenchRecord, code string) error {
	failed := *task
	failed.Status, failed.ErrorCode = "failed", code
	_, err := s.finish(ctx, &failed, 0)
	return err
}

func (s *VideoWorkbenchService) finish(ctx context.Context, task *VideoWorkbenchRecord, expiredBefore int64) (bool, error) {
	retention := s.Runtime(ctx).Retention()
	return s.finishWithRetention(ctx, task, expiredBefore, retention)
}

func (s *VideoWorkbenchService) finishWithRetention(ctx context.Context, task *VideoWorkbenchRecord, expiredBefore int64, retention time.Duration) (bool, error) {
	now := time.Now().UTC()
	task.CompletedAt, task.ExpiresAt = now.Unix(), now.Add(retention).Unix()
	return s.store.Finish(ctx, task, retention, expiredBefore)
}

func (s *VideoWorkbenchService) List(ctx context.Context, userID int64) ([]VideoWorkbenchTask, error) {
	if !s.Runtime(ctx).Enabled {
		return nil, ErrVideoWorkbenchDisabled
	}
	records, err := s.store.List(ctx, userID, time.Now().UTC())
	if err != nil {
		return nil, ErrVideoTaskUnavailable.WithCause(err)
	}
	tasks := make([]VideoWorkbenchTask, 0, len(records))
	for _, r := range records {
		if r.UserID == userID && r.ExpiresAt > time.Now().Unix() {
			tasks = append(tasks, r.VideoWorkbenchTask)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt > tasks[j].CreatedAt })
	return tasks, nil
}

func (s *VideoWorkbenchService) owned(ctx context.Context, userID int64, id string) (*VideoWorkbenchRecord, error) {
	if !s.Runtime(ctx).Enabled {
		return nil, ErrVideoWorkbenchDisabled
	}
	if !ValidVideoWorkbenchTaskID(id) {
		return nil, ErrVideoTaskNotFound
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.UserID != userID || task.ExpiresAt <= time.Now().Unix() {
		return nil, ErrVideoTaskNotFound
	}
	return task, nil
}

func (s *VideoWorkbenchService) Download(ctx context.Context, userID int64, id string) (io.ReadSeekCloser, *VideoWorkbenchTask, error) {
	task, err := s.owned(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	if task.Status != "completed" {
		return nil, nil, ErrVideoTaskNotFound
	}
	file, err := s.storage.Open(ctx, task.ID)
	if err != nil {
		return nil, nil, ErrVideoTaskNotFound
	}
	return file, &task.VideoWorkbenchTask, nil
}

func (s *VideoWorkbenchService) Delete(ctx context.Context, userID int64, id string) error {
	task, err := s.owned(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, task, time.Now().UTC()); err != nil {
		return err
	}
	// A durable cleanup entry remains until the private file is removed.
	if err := s.storage.Delete(ctx, task.ID); err != nil {
		return ErrVideoTaskUnavailable.WithCause(err)
	}
	return s.store.AcknowledgeCleanup(ctx, task.ID)
}

func ValidVideoWorkbenchTaskID(id string) bool {
	if len(id) != 40 || !strings.HasPrefix(id, "vidtask_") {
		return false
	}
	for _, c := range id[8:] {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

func (s *VideoWorkbenchService) Cleanup(ctx context.Context) error {
	now := time.Now().UTC()
	expired, err := s.store.ExpiredLeases(ctx, now)
	if err != nil {
		return err
	}
	for _, task := range expired {
		task.Status, task.ErrorCode = "failed", "interrupted"
		if _, err := s.finish(ctx, task, now.Unix()); err != nil && !errors.Is(err, ErrVideoTaskNotFound) {
			return err
		}
	}
	ids, err := s.store.ExpiredFiles(ctx, now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.storage.Delete(ctx, id); err != nil {
			return err
		}
		if err := s.store.AcknowledgeCleanup(ctx, id); err != nil {
			return err
		}
	}
	// Files also carry a disk expiry marker, so a Redis flush cannot orphan them.
	return s.storage.Sweep(ctx, now)
}

func (s *VideoWorkbenchService) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err := s.Cleanup(ctx)
		cancel()
		if err != nil {
			logger.L().Warn("video_workbench.cleanup_failed", zap.Error(err))
		}
	}
}
