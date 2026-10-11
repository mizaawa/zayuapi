package service

import (
	"context"
	"net/http"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ImageWorkbenchQueueTimeout = 30 * time.Minute
	ImageWorkbenchQueueLease   = time.Minute
	ImageWorkbenchQueueLimit   = 100
	workbenchPayloadLimit      = 128
	workbenchPayloadBytes      = 256 << 20
)

var (
	ErrImageWorkbenchQueueFull    = infraerrors.New(http.StatusTooManyRequests, "IMAGE_WORKBENCH_QUEUE_FULL", "image workbench queue is full; try again later")
	ErrImageWorkbenchQueueExpired = infraerrors.New(http.StatusRequestTimeout, "IMAGE_WORKBENCH_QUEUE_EXPIRED", "queued image task expired; submit it again")
)

// Redis stores only task metadata and leases. Request bodies and credentials
// remain in the accepting process and are discarded after execution or expiry.
type ImageWorkbenchQueueStore interface {
	TryStartWorkbench(ctx context.Context, owner ImageTaskOwner, id string, now time.Time, ttl time.Duration, limit int) (bool, error)
	ExpiredWorkbenchQueue(ctx context.Context, now time.Time) ([]*ImageTaskRecord, error)
	FailExpiredWorkbenchQueue(ctx context.Context, task *ImageTaskRecord, now time.Time, ttl time.Duration) error
}

type imageWorkbenchPayloadBudget struct {
	mu    sync.Mutex
	count int
	bytes int64
}

func (s *ImageTaskService) ReserveWorkbenchPayload(size int) (func(), error) {
	budget := &s.workbenchPayloads
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if size < 0 || budget.count >= workbenchPayloadLimit || int64(size) > workbenchPayloadBytes-budget.bytes {
		return nil, ErrImageWorkbenchQueueFull
	}
	budget.count++
	budget.bytes += int64(size)
	return sync.OnceFunc(func() {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		budget.count--
		budget.bytes -= int64(size)
	}), nil
}

func (s *ImageTaskService) StartWorkbench(ctx context.Context, owner ImageTaskOwner, id string) (bool, error) {
	policy := s.WorkbenchRuntime(ctx)
	if !policy.Enabled {
		return false, ErrImageWorkbenchDisabled
	}
	store, ok := s.store.(ImageWorkbenchQueueStore)
	if !ok {
		return false, ErrImageTaskUnavailable
	}
	return store.TryStartWorkbench(ctx, owner, id, time.Now().UTC(), s.ExecutionTimeout()+policy.Retention(), policy.Limit(owner.IsAdmin))
}

func (s *ImageTaskService) AwaitWorkbenchSlot(ctx context.Context, id string, authorize func(context.Context) (ImageTaskOwner, error)) (ImageTaskOwner, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		current, err := authorize(attemptCtx)
		if err != nil {
			cancel()
			return ImageTaskOwner{}, err
		}
		started, err := s.StartWorkbench(attemptCtx, current, id)
		cancel()
		if err != nil {
			return ImageTaskOwner{}, err
		}
		if started {
			return current, nil
		}
		select {
		case <-ctx.Done():
			return ImageTaskOwner{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *ImageTaskService) expireWorkbenchQueue(ctx context.Context) error {
	store, ok := s.store.(ImageWorkbenchQueueStore)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	tasks, err := store.ExpiredWorkbenchQueue(ctx, now)
	if err != nil {
		return err
	}
	ttl := s.WorkbenchRuntime(ctx).Retention()
	for _, task := range tasks {
		completedAt := now.Unix()
		task.Status = ImageTaskStatusFailed
		task.HTTPStatus = http.StatusGatewayTimeout
		task.Error = workbenchPublicError(task.HTTPStatus)
		task.CompletedAt = &completedAt
		task.ExpiresAt = now.Add(ttl).Unix()
		task.QueueDeadline = 0
		task.QueueLeaseUntil = 0
		if err := store.FailExpiredWorkbenchQueue(ctx, task, now, ttl); err != nil {
			return err
		}
	}
	return nil
}
