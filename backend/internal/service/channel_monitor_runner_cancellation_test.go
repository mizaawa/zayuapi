//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunOne_SkipsCanceledQueuedCheckAndReleasesSlot(t *testing.T) {
	svc := &stubMonitorSvc{}
	runner := newRunnerForTest(svc)
	t.Cleanup(runner.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	require.True(t, runner.tryAcquireInFlight(42))
	cancel()

	runner.runOne(ctx, 42, "canceled")

	require.Zero(t, svc.runCount.Load())
	require.True(t, runner.tryAcquireInFlight(42), "canceled queued checks must release their slot")
	runner.releaseInFlight(42)
}

func TestSchedule_CanceledCheckCannotUnscheduleReplacement(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	releaseError := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseError) }) }
	var calls atomic.Int32
	svc := &stubMonitorSvc{runCheck: func(ctx context.Context, _ int64) ([]*CheckResult, error) {
		if calls.Add(1) != 1 {
			return nil, nil
		}
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
		case <-releaseError:
		}
		<-releaseError
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}}
	runner := newRunnerForTest(svc)
	t.Cleanup(func() {
		release()
		runner.Stop()
	})
	runner.Start()
	monitor := &ChannelMonitor{ID: 7, Enabled: true, IntervalSeconds: 60}
	runner.Schedule(monitor)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("original check did not start")
	}

	monitor.SimulateRequests = true
	runner.Schedule(monitor)
	replacement := runnerTaskPtr(runner, monitor.ID)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("replacing the task did not cancel its active check")
	}
	release()
	waitFor(t, time.Second, "canceled check released its slot", func() bool {
		return !runnerHasInFlight(runner, monitor.ID)
	})
	require.Same(t, replacement, runnerTaskPtr(runner, monitor.ID))
	require.Equal(t, 1, runnerTaskCount(runner))
}

type runnerSimulationRepoStub struct {
	ChannelMonitorRepository
	mu      sync.Mutex
	monitor *ChannelMonitor
}

func (r *runnerSimulationRepoStub) GetByID(context.Context, int64) (*ChannelMonitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneGroupMonitor(r.monitor), nil
}

func (r *runnerSimulationRepoStub) Update(_ context.Context, monitor *ChannelMonitor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.monitor = cloneGroupMonitor(monitor)
	return nil
}

func (r *runnerSimulationRepoStub) ListEnabled(context.Context) ([]*ChannelMonitor, error) {
	return nil, nil
}

func (r *runnerSimulationRepoStub) InsertHistoryBatch(ctx context.Context, _ []*ChannelMonitorHistoryRow) error {
	return ctx.Err()
}

func (r *runnerSimulationRepoStub) MarkChecked(ctx context.Context, _ int64, _ time.Time) error {
	return ctx.Err()
}

func TestSchedule_EnablingSimulationDuringPingCancelsSubsequentModelRequests(t *testing.T) {
	pingStarted := make(chan struct{})
	pingCanceled := make(chan struct{})
	releasePing := make(chan struct{})
	var modelRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodHead {
			close(pingStarted)
			select {
			case <-req.Context().Done():
				close(pingCanceled)
			case <-releasePing:
			}
			return
		}
		modelRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	groupID := int64(1)
	monitor := &ChannelMonitor{
		ID: 10, Name: "cancelable", Provider: MonitorProviderOpenAI,
		PrimaryModel: "primary", ExtraModels: []string{"extra"},
		APIKey: "enc:monitor-key", GroupID: &groupID, Endpoint: server.URL,
		Enabled: true, IntervalSeconds: 60, CreatedBy: 9,
		BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride:     map[string]any{"messages": []any{map[string]any{"role": "user", "content": "ping"}}},
	}
	repo := &runnerSimulationRepoStub{monitor: monitor}
	svc := NewChannelMonitorService(repo, &groupMonitorEncryptor{})
	svc.SetGroupDependencies(&groupMonitorReaderStub{groups: map[int64]*Group{
		groupID: activeMonitorGroup(groupID, "test", MonitorProviderOpenAI),
	}}, nil)
	svc.SetManagedGatewayEndpoint(server.URL)
	attestor, err := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	require.NoError(t, err)
	svc.SetManagedGatewayAttestor(attestor)
	runner := newRunnerForTest(svc)
	t.Cleanup(func() {
		close(releasePing)
		runner.Stop()
		server.Close()
	})
	svc.SetScheduler(runner)
	runner.Start()
	runner.Schedule(monitor)
	select {
	case <-pingStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("real check did not start its ping")
	}

	simulate := true
	updated, err := svc.Update(context.Background(), monitor.ID, ChannelMonitorUpdateParams{SimulateRequests: &simulate})
	require.NoError(t, err)
	require.True(t, updated.SimulateRequests)
	select {
	case <-pingCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("enabling simulation did not cancel the active ping")
	}
	waitFor(t, time.Second, "old check completed after cancellation", func() bool {
		return !runnerHasInFlight(runner, monitor.ID)
	})
	require.Zero(t, modelRequests.Load(), "the old check must not send model requests after simulation is enabled")
	require.Equal(t, 1, runnerTaskCount(runner))
	results, err := svc.RunCheck(context.Background(), monitor.ID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		require.Equal(t, MonitorStatusOperational, result.Status)
		require.Contains(t, result.Message, "Simulated")
	}
	require.Zero(t, modelRequests.Load())
}

func runnerHasInFlight(runner *ChannelMonitorRunner, id int64) bool {
	runner.inFlightMu.Lock()
	defer runner.inFlightMu.Unlock()
	_, ok := runner.inFlight[id]
	return ok
}
