//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type simulatedMonitorRepoStub struct {
	groupMonitorRepoStub
	history   []*ChannelMonitorHistoryRow
	checkedAt time.Time
}

func (r *simulatedMonitorRepoStub) InsertHistoryBatch(_ context.Context, rows []*ChannelMonitorHistoryRow) error {
	r.history = append(r.history, rows...)
	return nil
}

func (r *simulatedMonitorRepoStub) MarkChecked(_ context.Context, _ int64, checkedAt time.Time) error {
	r.checkedAt = checkedAt
	return nil
}

func TestChannelMonitorSimulatedCheckSendsNoRequestsAndCanResumeRealChecks(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(server.Close)
	groupID := int64(1)
	repo := &simulatedMonitorRepoStub{groupMonitorRepoStub: groupMonitorRepoStub{existing: &ChannelMonitor{
		ID: 10, Name: "simulated", Provider: MonitorProviderOpenAI,
		PrimaryModel: "primary", ExtraModels: []string{"extra"},
		APIKey: "enc:monitor-key", GroupID: &groupID, Endpoint: server.URL,
		SimulateRequests: true, BodyOverrideMode: MonitorBodyOverrideModeReplace,
		BodyOverride: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "ping"}}},
	}}}
	encryptor := &groupMonitorEncryptor{}
	svc := NewChannelMonitorService(repo, encryptor)
	svc.SetGroupDependencies(&groupMonitorReaderStub{groups: map[int64]*Group{
		groupID: activeMonitorGroup(groupID, "test", MonitorProviderOpenAI),
	}}, nil)
	svc.SetManagedGatewayEndpoint(server.URL)
	attestor, err := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	require.NoError(t, err)
	svc.SetManagedGatewayAttestor(attestor)

	results, err := svc.RunCheck(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Zero(t, requests.Load(), "simulation must skip both ping and model requests")
	require.Empty(t, encryptor.decryptInputs)
	require.Len(t, repo.history, 2)
	require.False(t, repo.checkedAt.IsZero())
	for i, result := range results {
		require.Equal(t, MonitorStatusOperational, result.Status)
		require.NotNil(t, result.LatencyMs)
		require.Zero(t, *result.LatencyMs)
		require.NotNil(t, result.PingLatencyMs)
		require.Zero(t, *result.PingLatencyMs)
		require.Contains(t, result.Message, "Simulated")
		require.Equal(t, result.Model, repo.history[i].Model)
		require.Equal(t, result.Status, repo.history[i].Status)
		require.Equal(t, result.Message, repo.history[i].Message)
	}

	repo.existing.SimulateRequests = false
	results, err = svc.RunCheck(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.EqualValues(t, 3, requests.Load(), "real checks send one ping and one request per model")
	require.Equal(t, []string{"enc:monitor-key"}, encryptor.decryptInputs)
	for _, result := range results {
		require.Equal(t, MonitorStatusOperational, result.Status)
		require.NotContains(t, result.Message, "Simulated")
	}
}

func TestChannelMonitorSimulationDoesNotRequireExecutionCredentials(t *testing.T) {
	groupID := int64(42)
	for _, group := range []*int64{nil, &groupID} {
		repo := &simulatedMonitorRepoStub{groupMonitorRepoStub: groupMonitorRepoStub{existing: &ChannelMonitor{
			ID: 10, PrimaryModel: "primary", GroupID: group,
			APIKey: "broken-ciphertext", APIKeyDecryptFailed: true, SimulateRequests: true,
		}}}
		svc := NewChannelMonitorService(repo, nil)
		results, err := svc.RunCheck(context.Background(), 10)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, MonitorStatusOperational, results[0].Status)
	}
}

func TestChannelMonitorSimulationCreateUpdateAndDuplicate(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "test", MonitorProviderOpenAI),
	}}
	svc := newGroupMonitorService(repo, reader, &groupMonitorKeyManagerStub{}, &groupMonitorEncryptor{})
	params := groupMonitorCreateParams(MonitorProviderOpenAI, 1)
	params.SimulateRequests = true
	created, err := svc.Create(context.Background(), params)
	require.NoError(t, err)
	require.True(t, created.SimulateRequests)
	require.True(t, repo.created[0].SimulateRequests)
	repo.existing = repo.created[0]

	updated, err := svc.Update(context.Background(), created.ID, ChannelMonitorUpdateParams{})
	require.NoError(t, err)
	require.True(t, updated.SimulateRequests, "omitted update must preserve simulation")

	duplicate, err := svc.Duplicate(context.Background(), created.ID, 9, "admin:9", "")
	require.NoError(t, err)
	require.True(t, duplicate.SimulateRequests)
	require.False(t, duplicate.Enabled)

	simulate := false
	updated, err = svc.Update(context.Background(), created.ID, ChannelMonitorUpdateParams{SimulateRequests: &simulate})
	require.NoError(t, err)
	require.False(t, updated.SimulateRequests)
	require.False(t, repo.updated[len(repo.updated)-1].SimulateRequests)
}

func TestSchedule_SimulatedMonitorDoesNotRequireWorkingAPIKey(t *testing.T) {
	svc := &stubMonitorSvc{runCalled: make(chan int64, 1)}
	runner := newRunnerForTest(svc)
	runner.Start()
	t.Cleanup(runner.Stop)
	monitor := &ChannelMonitor{ID: 10, Enabled: true, IntervalSeconds: 60,
		APIKeyDecryptFailed: true, SimulateRequests: true}
	runner.Schedule(monitor)
	require.Equal(t, 1, runnerTaskCount(runner))
	select {
	case id := <-svc.runCalled:
		require.Equal(t, monitor.ID, id)
	case <-time.After(2 * time.Second):
		t.Fatal("simulated monitor was not scheduled")
	}
	monitor.Enabled = false
	runner.Schedule(monitor)
	require.Zero(t, runnerTaskCount(runner), "disabled simulated monitors must remain unscheduled")
}
