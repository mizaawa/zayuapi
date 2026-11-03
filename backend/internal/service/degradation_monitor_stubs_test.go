//go:build unit

package service

import (
	"context"
	"sort"
	"sync"
	"time"
)

// memoryDegradationRepo is an in-memory DegradationMonitorRepository for unit tests.
type memoryDegradationRepo struct {
	mu       sync.Mutex
	nextID   int64
	prompts  map[int64]*DegradationPrompt
	monitors map[int64]*DegradationMonitor
	runs     map[int64]*DegradationRun
	groups   *degradationGroupStub
	usage    []*DegradationUsageSummary
	sumCalls int
}

func newMemoryDegradationRepo(groups *degradationGroupStub) *memoryDegradationRepo {
	return &memoryDegradationRepo{
		nextID:   100,
		prompts:  map[int64]*DegradationPrompt{},
		monitors: map[int64]*DegradationMonitor{},
		runs:     map[int64]*DegradationRun{},
		groups:   groups,
	}
}

func (r *memoryDegradationRepo) id() int64 {
	r.nextID++
	return r.nextID
}

func (r *memoryDegradationRepo) addPrompt(name, prompt string, sortOrder int) *DegradationPrompt {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := &DegradationPrompt{ID: r.id(), Name: name, Prompt: prompt, SortOrder: sortOrder}
	r.prompts[item.ID] = item
	return item
}

func (r *memoryDegradationRepo) ListPrompts(context.Context) ([]*DegradationPrompt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*DegradationPrompt, 0, len(r.prompts))
	for _, item := range r.prompts {
		cloned := *item
		items = append(items, &cloned)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (r *memoryDegradationRepo) GetPrompt(_ context.Context, id int64) (*DegradationPrompt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.prompts[id]
	if !ok {
		return nil, ErrDegradationPromptNotFound
	}
	cloned := *item
	return &cloned, nil
}

func (r *memoryDegradationRepo) CreatePrompt(_ context.Context, prompt *DegradationPrompt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	prompt.ID = r.id()
	prompt.SortOrder = int(prompt.ID)
	cloned := *prompt
	r.prompts[prompt.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) UpdatePrompt(_ context.Context, prompt *DegradationPrompt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prompts[prompt.ID]; !ok {
		return ErrDegradationPromptNotFound
	}
	cloned := *prompt
	r.prompts[prompt.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) DeletePrompt(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prompts[id]; !ok {
		return ErrDegradationPromptNotFound
	}
	delete(r.prompts, id)
	for _, monitor := range r.monitors {
		if monitor.PromptID != nil && *monitor.PromptID == id {
			monitor.PromptID = nil
		}
	}
	return nil
}

func (r *memoryDegradationRepo) decorate(monitor *DegradationMonitor) *DegradationMonitor {
	cloned := *monitor
	cloned.GroupDeleted = true
	if group := r.groups.lookup(monitor.GroupID); group != nil {
		cloned.GroupName = group.Name
		cloned.GroupPlatform = group.Platform
		cloned.GroupStatus = group.Status
		cloned.GroupDeleted = false
	}
	if cloned.PromptID != nil {
		if prompt, ok := r.prompts[*cloned.PromptID]; ok {
			cloned.PromptName = prompt.Name
		}
	}
	return &cloned
}

func (r *memoryDegradationRepo) ListMonitors(context.Context) ([]*DegradationMonitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*DegradationMonitor, 0, len(r.monitors))
	for _, monitor := range r.monitors {
		items = append(items, r.decorate(monitor))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (r *memoryDegradationRepo) GetMonitor(_ context.Context, id int64) (*DegradationMonitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	monitor, ok := r.monitors[id]
	if !ok {
		return nil, ErrDegradationMonitorNotFound
	}
	return r.decorate(monitor), nil
}

func (r *memoryDegradationRepo) CreateMonitor(_ context.Context, monitor *DegradationMonitor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	monitor.ID = r.id()
	monitor.SortOrder = int(monitor.ID) * 10
	monitor.CreatedAt = time.Now()
	monitor.UpdatedAt = monitor.CreatedAt
	cloned := *monitor
	r.monitors[monitor.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) UpdateMonitor(_ context.Context, monitor *DegradationMonitor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.monitors[monitor.ID]; !ok {
		return ErrDegradationMonitorNotFound
	}
	cloned := *monitor
	r.monitors[monitor.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) DeleteMonitor(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.monitors[id]; !ok {
		return ErrDegradationMonitorNotFound
	}
	delete(r.monitors, id)
	for runID, run := range r.runs {
		if run.MonitorID == id {
			delete(r.runs, runID)
		}
	}
	return nil
}

func (r *memoryDegradationRepo) UpdateMonitorSortOrders(_ context.Context, updates []DegradationSortOrderUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, update := range updates {
		if monitor, ok := r.monitors[update.ID]; ok {
			monitor.SortOrder = update.SortOrder
		}
	}
	return nil
}

func (r *memoryDegradationRepo) UpdateMonitorAPIKey(_ context.Context, id, apiKeyID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	monitor, ok := r.monitors[id]
	if !ok {
		return ErrDegradationMonitorNotFound
	}
	monitor.APIKeyID = apiKeyID
	return nil
}

func (r *memoryDegradationRepo) ClaimMonitorRun(_ context.Context, id int64, now, dueBefore time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	monitor, ok := r.monitors[id]
	if !ok || !monitor.Enabled {
		return false, nil
	}
	if monitor.LastRunAt != nil && monitor.LastRunAt.After(dueBefore) {
		return false, nil
	}
	stamp := now
	monitor.LastRunAt = &stamp
	return true, nil
}

func (r *memoryDegradationRepo) MarkMonitorRun(_ context.Context, id int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if monitor, ok := r.monitors[id]; ok {
		stamp := now
		monitor.LastRunAt = &stamp
	}
	return nil
}

func (r *memoryDegradationRepo) MonitorStats(_ context.Context, ids []int64) (map[int64]*DegradationMonitorStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stats := map[int64]*DegradationMonitorStats{}
	for _, id := range ids {
		for _, run := range r.runs {
			if run.MonitorID != id {
				continue
			}
			stat := stats[id]
			if stat == nil {
				stat = &DegradationMonitorStats{}
				stats[id] = stat
			}
			stat.StoredRuns++
			stat.StoredBytes += int64(len(run.Output))
		}
	}
	return stats, nil
}

func (r *memoryDegradationRepo) CreateRun(_ context.Context, run *DegradationRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = r.id()
	cloned := *run
	r.runs[run.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) FinishRun(_ context.Context, run *DegradationRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := *run
	cloned.OutputBytes = int64(len(run.Output))
	r.runs[run.ID] = &cloned
	return nil
}

func (r *memoryDegradationRepo) UpdateRunUsage(_ context.Context, run *DegradationRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.runs[run.ID]
	if !ok {
		return ErrDegradationRunNotFound
	}
	stored.InputTokens = run.InputTokens
	stored.OutputTokens = run.OutputTokens
	stored.CacheReadTokens = run.CacheReadTokens
	stored.CacheCreationTokens = run.CacheCreationTokens
	stored.Cost = run.Cost
	stored.UsageSynced = run.UsageSynced
	return nil
}

func (r *memoryDegradationRepo) GetRun(_ context.Context, id int64) (*DegradationRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, ErrDegradationRunNotFound
	}
	cloned := *run
	return &cloned, nil
}

func (r *memoryDegradationRepo) monitorRuns(monitorID int64) []*DegradationRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*DegradationRun, 0)
	for _, run := range r.runs {
		if run.MonitorID == monitorID {
			cloned := *run
			items = append(items, &cloned)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items
}

func (r *memoryDegradationRepo) ListRecentFinishedRuns(_ context.Context, monitorIDs []int64, perMonitor int) (map[int64][]*DegradationRun, error) {
	result := map[int64][]*DegradationRun{}
	for _, id := range monitorIDs {
		items := make([]*DegradationRun, 0)
		for _, item := range r.monitorRuns(id) {
			if item.Status != DegradationRunStatusRunning {
				items = append(items, item)
			}
		}
		if len(items) > perMonitor {
			items = items[:perMonitor]
		}
		for _, item := range items {
			item.Output = ""
			item.Prompt = ""
		}
		result[id] = items
	}
	return result, nil
}

func (r *memoryDegradationRepo) StorageStats(_ context.Context, monitorID int64) (*DegradationStorageStats, error) {
	stats := &DegradationStorageStats{}
	for _, run := range r.monitorRuns(monitorID) {
		stats.Count++
		stats.Bytes += int64(len(run.Output) + len(run.Prompt))
	}
	return stats, nil
}

func (r *memoryDegradationRepo) DeleteRuns(_ context.Context, monitorID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for id, run := range r.runs {
		if run.MonitorID == monitorID && run.Status != DegradationRunStatusRunning {
			delete(r.runs, id)
			deleted++
		}
	}
	return deleted, nil
}

func (r *memoryDegradationRepo) PruneRuns(_ context.Context, monitorID int64, keep int) (int64, error) {
	items := r.monitorRuns(monitorID)
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for index, item := range items {
		if index >= keep {
			delete(r.runs, item.ID)
			deleted++
		}
	}
	return deleted, nil
}

func (r *memoryDegradationRepo) FailStaleRuns(_ context.Context, startedBefore time.Time, message string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, run := range r.runs {
		if run.Status == DegradationRunStatusRunning && run.StartedAt.Before(startedBefore) {
			run.Status = DegradationRunStatusFailed
			run.ErrorMessage = message
			count++
		}
	}
	return count, nil
}

func (r *memoryDegradationRepo) SumUsage(context.Context, int64, time.Time, time.Time) (*DegradationUsageSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sumCalls++
	if len(r.usage) == 0 {
		return &DegradationUsageSummary{}, nil
	}
	index := r.sumCalls - 1
	if index >= len(r.usage) {
		index = len(r.usage) - 1
	}
	cloned := *r.usage[index]
	return &cloned, nil
}

// degradationGroupStub resolves site groups.
type degradationGroupStub struct {
	mu     sync.Mutex
	groups map[int64]*Group
}

func newDegradationGroupStub(groups ...*Group) *degradationGroupStub {
	stub := &degradationGroupStub{groups: map[int64]*Group{}}
	for _, group := range groups {
		stub.groups[group.ID] = group
	}
	return stub
}

func (s *degradationGroupStub) lookup(id int64) *Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, ok := s.groups[id]
	if !ok {
		return nil
	}
	cloned := *group
	return &cloned
}

func (s *degradationGroupStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	group := s.lookup(id)
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

// degradationKeyStub records managed key operations.
type degradationKeyStub struct {
	mu      sync.Mutex
	nextID  int64
	keys    map[int64]*APIKey
	deleted []int64
	getErr  error
}

func newDegradationKeyStub() *degradationKeyStub {
	return &degradationKeyStub{nextID: 500, keys: map[int64]*APIKey{}}
}

func (s *degradationKeyStub) CreateDegradationMonitorKey(_ context.Context, userID, groupID int64, name string) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	group := groupID
	key := &APIKey{ID: s.nextID, UserID: userID, GroupID: &group, Name: name, Key: "sk-managed-degradation-key-" + time.Now().Format("150405.000000"), Status: StatusAPIKeyActive, Purpose: APIKeyPurposeChannelMonitor}
	s.keys[key.ID] = key
	cloned := *key
	return &cloned, nil
}

func (s *degradationKeyStub) GetManagedMonitorKey(_ context.Context, id int64) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	key, ok := s.keys[id]
	if !ok {
		return nil, ErrAPIKeyNotFound
	}
	cloned := *key
	return &cloned, nil
}

func (s *degradationKeyStub) DeleteManagedMonitorKeyByID(_ context.Context, id, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, id)
	s.deleted = append(s.deleted, id)
	return nil
}

// degradationSettingsStub is an in-memory settings store.
type degradationSettingsStub struct {
	mu     sync.Mutex
	values map[string]string
}

func newDegradationSettingsStub(enabled bool) *degradationSettingsStub {
	stub := &degradationSettingsStub{values: map[string]string{}}
	if enabled {
		stub.values[SettingKeyDegradationMonitorEnabled] = "true"
	}
	return stub
}

func (s *degradationSettingsStub) GetValue(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *degradationSettingsStub) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

// degradationSchedulerStub records scheduler interactions.
type degradationSchedulerStub struct {
	mu       sync.Mutex
	kicks    int
	running  map[int64]bool
	triggers []int64
	err      error
}

func (s *degradationSchedulerStub) Kick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kicks++
}

func (s *degradationSchedulerStub) IsRunning(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[id]
}

func (s *degradationSchedulerStub) TriggerNow(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.triggers = append(s.triggers, id)
	return nil
}

const degradationTestEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type degradationTestEnv struct {
	svc       *DegradationMonitorService
	repo      *memoryDegradationRepo
	groups    *degradationGroupStub
	keys      *degradationKeyStub
	settings  *degradationSettingsStub
	scheduler *degradationSchedulerStub
	attestor  *ChannelMonitorAttestor
}

func newDegradationTestEnv(enabled bool, endpoint string) *degradationTestEnv {
	groups := newDegradationGroupStub(
		&Group{ID: 1, Name: "OpenAI Pro", Platform: PlatformOpenAI, Status: StatusActive},
		&Group{ID: 2, Name: "Claude Max", Platform: PlatformAnthropic, Status: StatusActive},
		&Group{ID: 3, Name: "Paused", Platform: PlatformOpenAI, Status: "inactive"},
		&Group{ID: 4, Name: "Retired", Platform: PlatformAntigravity, Status: StatusActive},
	)
	repo := newMemoryDegradationRepo(groups)
	keys := newDegradationKeyStub()
	settings := newDegradationSettingsStub(enabled)
	svc := NewDegradationMonitorService(repo, settings, groups, keys)
	attestor, err := NewChannelMonitorAttestor(degradationTestEncryptionKey)
	if err != nil {
		panic(err)
	}
	if endpoint == "" {
		endpoint = "http://127.0.0.1:1"
	}
	svc.SetManagedGateway(endpoint, attestor)
	scheduler := &degradationSchedulerStub{running: map[int64]bool{}}
	svc.SetScheduler(scheduler)
	svc.usageWait = 30 * time.Millisecond
	svc.usagePoll = 2 * time.Millisecond
	svc.retryDelay = 0
	return &degradationTestEnv{svc: svc, repo: repo, groups: groups, keys: keys, settings: settings, scheduler: scheduler, attestor: attestor}
}
