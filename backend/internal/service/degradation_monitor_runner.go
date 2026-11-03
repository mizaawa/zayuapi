package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	degradationRunnerConcurrency = 3
	degradationScanInterval      = 30 * time.Second
	degradationStartupDelay      = 15 * time.Second
	degradationScanTimeout       = 10 * time.Second
)

// degradationRunnerService is the part of DegradationMonitorService the runner
// uses; tests provide a lightweight stub.
type degradationRunnerService interface {
	Enabled(ctx context.Context) bool
	DueMonitorIDs(ctx context.Context, now time.Time) ([]int64, error)
	ClaimScheduledRun(ctx context.Context, id int64, now time.Time) (bool, error)
	ExecuteRun(ctx context.Context, id int64, trigger string)
	RecoverStaleRuns(ctx context.Context)
}

// DegradationMonitorRunner schedules degradation checks. A single loop scans
// for due monitors; each check runs in its own goroutine, at most
// degradationRunnerConcurrency at a time and never twice for one monitor.
type DegradationMonitorRunner struct {
	svc          degradationRunnerService
	scanInterval time.Duration
	startDelay   time.Duration
	capacity     int
	now          func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	kickCh chan struct{}
	wg     sync.WaitGroup

	mu       sync.Mutex
	inFlight map[int64]struct{}
	started  bool
	stopped  bool
}

// NewDegradationMonitorRunner creates a runner. Start must be called once.
func NewDegradationMonitorRunner(svc *DegradationMonitorService) *DegradationMonitorRunner {
	return newDegradationMonitorRunner(svc)
}

func newDegradationMonitorRunner(svc degradationRunnerService) *DegradationMonitorRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &DegradationMonitorRunner{
		svc:          svc,
		scanInterval: degradationScanInterval,
		startDelay:   degradationStartupDelay,
		capacity:     degradationRunnerConcurrency,
		now:          time.Now,
		ctx:          ctx,
		cancel:       cancel,
		kickCh:       make(chan struct{}, 1),
		inFlight:     make(map[int64]struct{}),
	}
}

// Start launches the scan loop.
func (r *DegradationMonitorRunner) Start() {
	if r == nil || r.svc == nil {
		return
	}
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.wg.Add(1)
	r.mu.Unlock()
	go r.loop()
}

// Stop cancels running checks and waits for them to record their outcome.
func (r *DegradationMonitorRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	r.mu.Unlock()
	r.cancel()
	r.wg.Wait()
}

// Kick asks the loop to rescan soon, e.g. after a monitor was created.
func (r *DegradationMonitorRunner) Kick() {
	if r == nil {
		return
	}
	select {
	case r.kickCh <- struct{}{}:
	default:
	}
}

// IsRunning reports whether a check of the monitor is in progress.
func (r *DegradationMonitorRunner) IsRunning(id int64) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.inFlight[id]
	return ok
}

// TriggerNow starts a manual check immediately.
func (r *DegradationMonitorRunner) TriggerNow(id int64) error {
	if r == nil {
		return ErrDegradationRunnerBusy
	}
	if err := r.acquire(id); err != nil {
		return err
	}
	r.launch(id, func(ctx context.Context) {
		r.svc.ExecuteRun(ctx, id, DegradationTriggerManual)
	})
	return nil
}

func (r *DegradationMonitorRunner) loop() {
	defer r.wg.Done()
	timer := time.NewTimer(r.startDelay)
	defer timer.Stop()
	ready := false
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-timer.C:
			ready = true
			r.scan()
			timer.Reset(r.scanInterval)
		case <-r.kickCh:
			// Kicks before the startup delay are covered by the first scan.
			if ready {
				r.scan()
			}
		}
	}
}

func (r *DegradationMonitorRunner) scan() {
	ctx, cancel := context.WithTimeout(r.ctx, degradationScanTimeout)
	defer cancel()
	r.svc.RecoverStaleRuns(ctx)
	if !r.svc.Enabled(ctx) {
		return
	}
	ids, err := r.svc.DueMonitorIDs(ctx, r.now())
	if err != nil {
		if r.ctx.Err() == nil {
			slog.Warn("degradation_monitor: load due monitors failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if r.acquire(id) != nil {
			continue
		}
		monitorID := id
		r.launch(monitorID, func(ctx context.Context) {
			claimed, err := r.svc.ClaimScheduledRun(ctx, monitorID, r.now())
			if err != nil {
				slog.Warn("degradation_monitor: claim run failed", "monitor_id", monitorID, "error", err)
				return
			}
			if claimed {
				r.svc.ExecuteRun(ctx, monitorID, DegradationTriggerSchedule)
			}
		})
	}
}

// acquire reserves a worker slot for the monitor.
func (r *DegradationMonitorRunner) acquire(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return ErrDegradationRunnerBusy
	}
	if _, ok := r.inFlight[id]; ok {
		return ErrDegradationRunInProgress
	}
	if len(r.inFlight) >= r.capacity {
		return ErrDegradationRunnerBusy
	}
	r.inFlight[id] = struct{}{}
	r.wg.Add(1)
	return nil
}

func (r *DegradationMonitorRunner) launch(id int64, task func(ctx context.Context)) {
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.inFlight, id)
			r.mu.Unlock()
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("degradation_monitor: runner panic", "monitor_id", id, "panic", recovered)
			}
		}()
		task(r.ctx)
	}()
}
