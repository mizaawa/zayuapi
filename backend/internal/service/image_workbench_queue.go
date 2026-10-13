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

// WorkbenchPayloadReservation accounts for one accepted request body that a
// queued task keeps in process memory until its worker exits.
type WorkbenchPayloadReservation struct {
	budget   *imageWorkbenchPayloadBudget
	size     int64
	released bool
}

func (b *imageWorkbenchPayloadBudget) reserve(size int, full error) (*WorkbenchPayloadReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if size < 0 || b.count >= workbenchPayloadLimit || int64(size) > workbenchPayloadBytes-b.bytes {
		return nil, full
	}
	b.count++
	b.bytes += int64(size)
	return &WorkbenchPayloadReservation{budget: b, size: int64(size)}, nil
}

// Shrink lowers the admission estimate to the bytes actually retained. Bodies
// without a declared length are admitted at the request limit; left at that
// size, one small queued upload would hold the shared budget for every user.
func (r *WorkbenchPayloadReservation) Shrink(size int) {
	if r == nil || size < 0 {
		return
	}
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	if r.released || int64(size) >= r.size {
		return
	}
	r.budget.bytes -= r.size - int64(size)
	r.size = int64(size)
}

func (r *WorkbenchPayloadReservation) Release() {
	if r == nil {
		return
	}
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	r.budget.count--
	r.budget.bytes -= r.size
}

func (s *ImageTaskService) ReserveWorkbenchUpload(size int) (*WorkbenchPayloadReservation, error) {
	return s.workbenchPayloads.reserve(size, ErrImageWorkbenchQueueFull)
}

func (s *ImageTaskService) ReserveWorkbenchPayload(size int) (func(), error) {
	reservation, err := s.ReserveWorkbenchUpload(size)
	if err != nil {
		return nil, err
	}
	return reservation.Release, nil
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
