//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type degradationRunnerSvcStub struct {
	mu        sync.Mutex
	enabled   bool
	due       []int64
	claimable map[int64]bool
	executed  []string
	block     chan struct{}
	started   chan int64
	recovered int
	dueCalls  int
}

func newDegradationRunnerSvcStub() *degradationRunnerSvcStub {
	return &degradationRunnerSvcStub{enabled: true, claimable: map[int64]bool{}, started: make(chan int64, 16)}
}

func (s *degradationRunnerSvcStub) Enabled(context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

func (s *degradationRunnerSvcStub) DueMonitorIDs(context.Context, time.Time) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dueCalls++
	return append([]int64(nil), s.due...), nil
}

func (s *degradationRunnerSvcStub) ClaimScheduledRun(_ context.Context, id int64, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimable[id], nil
}

func (s *degradationRunnerSvcStub) ExecuteRun(ctx context.Context, id int64, trigger string) {
	s.mu.Lock()
	s.executed = append(s.executed, trigger)
	block := s.block
	s.mu.Unlock()
	s.started <- id
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
}

func (s *degradationRunnerSvcStub) RecoverStaleRuns(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovered++
}

func (s *degradationRunnerSvcStub) executions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.executed...)
}

func waitStarted(t *testing.T, ch <-chan int64) int64 {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("check did not start")
		return 0
	}
}

func TestDegradationRunnerTriggerNowGuardsInFlightAndCapacity(t *testing.T) {
	svc := newDegradationRunnerSvcStub()
	svc.block = make(chan struct{})
	runner := newDegradationMonitorRunner(svc)
	runner.capacity = 2

	require.NoError(t, runner.TriggerNow(1))
	require.Equal(t, int64(1), waitStarted(t, svc.started))
	require.True(t, runner.IsRunning(1))
	require.ErrorIs(t, runner.TriggerNow(1), ErrDegradationRunInProgress)

	require.NoError(t, runner.TriggerNow(2))
	waitStarted(t, svc.started)
	require.ErrorIs(t, runner.TriggerNow(3), ErrDegradationRunnerBusy)

	close(svc.block)
	require.Eventually(t, func() bool { return !runner.IsRunning(1) && !runner.IsRunning(2) }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{DegradationTriggerManual, DegradationTriggerManual}, svc.executions())

	runner.Stop()
	require.ErrorIs(t, runner.TriggerNow(4), ErrDegradationRunnerBusy, "a stopped runner accepts no work")
}

func TestDegradationRunnerScanRunsOnlyClaimedMonitors(t *testing.T) {
	svc := newDegradationRunnerSvcStub()
	svc.due = []int64{10, 11}
	svc.claimable[10] = true
	runner := newDegradationMonitorRunner(svc)

	runner.scan()
	require.Equal(t, int64(10), waitStarted(t, svc.started))
	require.Eventually(t, func() bool { return !runner.IsRunning(10) && !runner.IsRunning(11) }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, []string{DegradationTriggerSchedule}, svc.executions())
	require.Equal(t, 1, svc.recovered)

	svc.mu.Lock()
	svc.enabled = false
	svc.mu.Unlock()
	runner.scan()
	svc.mu.Lock()
	dueCalls := svc.dueCalls
	svc.mu.Unlock()
	require.Equal(t, 1, dueCalls, "a disabled feature never looks for due monitors")
	runner.Stop()
}

func TestDegradationRunnerStartScansAfterDelayAndStopCancelsChecks(t *testing.T) {
	svc := newDegradationRunnerSvcStub()
	svc.due = []int64{5}
	svc.claimable[5] = true
	svc.block = make(chan struct{})
	runner := newDegradationMonitorRunner(svc)
	runner.startDelay = 10 * time.Millisecond
	runner.scanInterval = time.Hour

	runner.Start()
	runner.Start() // idempotent
	require.Equal(t, int64(5), waitStarted(t, svc.started))

	done := make(chan struct{})
	go func() {
		runner.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop must cancel running checks and return")
	}
	runner.Stop() // idempotent
}

func TestDegradationRunnerKickRescans(t *testing.T) {
	svc := newDegradationRunnerSvcStub()
	runner := newDegradationMonitorRunner(svc)
	runner.startDelay = 5 * time.Millisecond
	runner.scanInterval = time.Hour
	runner.Start()
	defer runner.Stop()

	require.Eventually(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		return svc.dueCalls == 1
	}, 2*time.Second, 5*time.Millisecond)

	svc.mu.Lock()
	svc.due = []int64{9}
	svc.claimable[9] = true
	svc.mu.Unlock()
	runner.Kick()
	require.Equal(t, int64(9), waitStarted(t, svc.started))
}
