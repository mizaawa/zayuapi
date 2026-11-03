package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// DegradationMonitorRepository persists monitors, prompt presets and results.
type DegradationMonitorRepository interface {
	ListPrompts(ctx context.Context) ([]*DegradationPrompt, error)
	GetPrompt(ctx context.Context, id int64) (*DegradationPrompt, error)
	CreatePrompt(ctx context.Context, prompt *DegradationPrompt) error
	UpdatePrompt(ctx context.Context, prompt *DegradationPrompt) error
	DeletePrompt(ctx context.Context, id int64) error

	ListMonitors(ctx context.Context) ([]*DegradationMonitor, error)
	GetMonitor(ctx context.Context, id int64) (*DegradationMonitor, error)
	CreateMonitor(ctx context.Context, monitor *DegradationMonitor) error
	UpdateMonitor(ctx context.Context, monitor *DegradationMonitor) error
	DeleteMonitor(ctx context.Context, id int64) error
	UpdateMonitorSortOrders(ctx context.Context, updates []DegradationSortOrderUpdate) error
	UpdateMonitorAPIKey(ctx context.Context, id, apiKeyID int64) error
	// ClaimMonitorRun stamps last_run_at when the monitor is still due, so only
	// one scheduler wins a slot even when several instances share a database.
	ClaimMonitorRun(ctx context.Context, id int64, now, dueBefore time.Time) (bool, error)
	MarkMonitorRun(ctx context.Context, id int64, now time.Time) error
	MonitorStats(ctx context.Context, ids []int64) (map[int64]*DegradationMonitorStats, error)

	CreateRun(ctx context.Context, run *DegradationRun) error
	FinishRun(ctx context.Context, run *DegradationRun) error
	UpdateRunUsage(ctx context.Context, run *DegradationRun) error
	GetRun(ctx context.Context, id int64) (*DegradationRun, error)
	ListRecentRuns(ctx context.Context, monitorIDs []int64, perMonitor int) (map[int64][]*DegradationRun, error)
	StorageStats(ctx context.Context, monitorID int64) (*DegradationStorageStats, error)
	DeleteRuns(ctx context.Context, monitorID int64) (int64, error)
	PruneRuns(ctx context.Context, monitorID int64, keep int) (int64, error)
	FailStaleRuns(ctx context.Context, startedBefore time.Time, message string) (int64, error)
	SumUsage(ctx context.Context, apiKeyID int64, from, to time.Time) (*DegradationUsageSummary, error)
}

// DegradationMonitorKeyManager owns the managed probe keys. APIKeyService implements it.
type DegradationMonitorKeyManager interface {
	CreateDegradationMonitorKey(ctx context.Context, userID, groupID int64, monitorName string) (*APIKey, error)
	GetManagedMonitorKey(ctx context.Context, id int64) (*APIKey, error)
	DeleteManagedMonitorKeyByID(ctx context.Context, id, ownerID int64) error
}

// DegradationSettingsStore is the slice of the settings repository this feature needs.
type DegradationSettingsStore interface {
	GetValue(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// degradationScheduler is implemented by DegradationMonitorRunner.
type degradationScheduler interface {
	Kick()
	IsRunning(id int64) bool
	TriggerNow(id int64) error
}

// DegradationMonitorService manages degradation monitors and executes their checks.
type DegradationMonitorService struct {
	repo        DegradationMonitorRepository
	settings    DegradationSettingsStore
	groupReader ChannelMonitorGroupReader
	keys        DegradationMonitorKeyManager
	endpoint    string
	attestor    *ChannelMonitorAttestor
	client      *http.Client
	scheduler   degradationScheduler
	now         func() time.Time
	randomIndex func(n int) int
	usageWait   time.Duration
	usagePoll   time.Duration
	retryDelay  time.Duration
}

// NewDegradationMonitorService creates the service. Gateway and scheduler
// dependencies are injected separately so focused tests can omit them.
func NewDegradationMonitorService(
	repo DegradationMonitorRepository,
	settings DegradationSettingsStore,
	groupReader ChannelMonitorGroupReader,
	keys DegradationMonitorKeyManager,
) *DegradationMonitorService {
	return &DegradationMonitorService{
		repo:        repo,
		settings:    settings,
		groupReader: groupReader,
		keys:        keys,
		client:      degradationHTTPClient,
		now:         time.Now,
		randomIndex: func(n int) int { return rand.IntN(n) },
		usageWait:   15 * time.Second,
		usagePoll:   time.Second,
		retryDelay:  degradationRetryDelay,
	}
}

// SetManagedGateway pins checks to this deployment's own gateway listener.
func (s *DegradationMonitorService) SetManagedGateway(endpoint string, attestor *ChannelMonitorAttestor) {
	s.endpoint = normalizeEndpoint(endpoint)
	s.attestor = attestor
}

// SetScheduler wires the runner back into the service for CRUD hooks.
func (s *DegradationMonitorService) SetScheduler(scheduler degradationScheduler) {
	s.scheduler = scheduler
}

func (s *DegradationMonitorService) kick() {
	if s.scheduler != nil {
		s.scheduler.Kick()
	}
}

// Enabled reports the feature switch. A settings error fails closed because
// every check spends upstream quota.
func (s *DegradationMonitorService) Enabled(ctx context.Context) bool {
	if s == nil || s.settings == nil {
		return false
	}
	value, err := s.settings.GetValue(ctx, SettingKeyDegradationMonitorEnabled)
	return err == nil && strings.TrimSpace(value) == "true"
}

// GetConfig returns the saved configuration, repaired to valid ranges.
func (s *DegradationMonitorService) GetConfig(ctx context.Context) DegradationConfig {
	cfg := DefaultDegradationConfig()
	if s == nil || s.settings == nil {
		return cfg
	}
	raw, err := s.settings.GetValue(ctx, SettingKeyDegradationMonitorConfig)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("degradation_monitor: invalid stored config, using defaults", "error", err)
		return DefaultDegradationConfig()
	}
	return cfg.normalized()
}

// UpdateConfig validates and stores the global configuration.
func (s *DegradationMonitorService) UpdateConfig(ctx context.Context, cfg DegradationConfig) (DegradationConfig, error) {
	cfg.DefaultReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.DefaultReasoningEffort))
	if err := cfg.Validate(); err != nil {
		return DegradationConfig{}, err
	}
	if cfg.DefaultPromptID > 0 {
		if _, err := s.repo.GetPrompt(ctx, cfg.DefaultPromptID); err != nil {
			return DegradationConfig{}, err
		}
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return DegradationConfig{}, fmt.Errorf("marshal degradation config: %w", err)
	}
	if err := s.settings.Set(ctx, SettingKeyDegradationMonitorConfig, string(payload)); err != nil {
		return DegradationConfig{}, fmt.Errorf("save degradation config: %w", err)
	}
	s.kick()
	return cfg, nil
}

// ---------- prompt presets ----------

// ListPrompts returns every preset in display order.
func (s *DegradationMonitorService) ListPrompts(ctx context.Context) ([]*DegradationPrompt, error) {
	return s.repo.ListPrompts(ctx)
}

// CreatePrompt stores a new preset. An empty name is taken from the prompt text.
func (s *DegradationMonitorService) CreatePrompt(ctx context.Context, name, prompt string) (*DegradationPrompt, error) {
	name, prompt, err := normalizeDegradationPrompt(name, prompt)
	if err != nil {
		return nil, err
	}
	item := &DegradationPrompt{Name: name, Prompt: prompt}
	if err := s.repo.CreatePrompt(ctx, item); err != nil {
		return nil, fmt.Errorf("create degradation prompt: %w", err)
	}
	return item, nil
}

// UpdatePrompt edits an existing preset.
func (s *DegradationMonitorService) UpdatePrompt(ctx context.Context, id int64, name, prompt string) (*DegradationPrompt, error) {
	existing, err := s.repo.GetPrompt(ctx, id)
	if err != nil {
		return nil, err
	}
	name, prompt, err = normalizeDegradationPrompt(name, prompt)
	if err != nil {
		return nil, err
	}
	existing.Name = name
	existing.Prompt = prompt
	if err := s.repo.UpdatePrompt(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeletePrompt removes a preset. Monitors that selected it fall back to the default.
func (s *DegradationMonitorService) DeletePrompt(ctx context.Context, id int64) error {
	if err := s.repo.DeletePrompt(ctx, id); err != nil {
		return err
	}
	cfg := s.GetConfig(ctx)
	if cfg.DefaultPromptID == id {
		cfg.DefaultPromptID = 0
		if _, err := s.UpdateConfig(ctx, cfg); err != nil {
			slog.Warn("degradation_monitor: reset default prompt failed", "prompt_id", id, "error", err)
		}
	}
	return nil
}

func normalizeDegradationPrompt(name, prompt string) (string, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > DegradationPromptMaxBytes {
		return "", "", ErrDegradationInvalidPrompt
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = degradationPromptTitle(prompt)
	}
	if utf8.RuneCountInString(name) > DegradationNameMaxRunes {
		return "", "", ErrDegradationInvalidPromptName
	}
	return name, prompt, nil
}

// degradationPromptTitle takes the start of the prompt as a preset name.
func degradationPromptTitle(prompt string) string {
	line := strings.TrimSpace(strings.SplitN(prompt, "\n", 2)[0])
	runes := []rune(line)
	if len(runes) > 30 {
		return string(runes[:30]) + "…"
	}
	return line
}

// ---------- monitors ----------

// ListMonitors returns every monitor with stored-result statistics.
func (s *DegradationMonitorService) ListMonitors(ctx context.Context) ([]*DegradationMonitorView, DegradationConfig, error) {
	cfg := s.GetConfig(ctx)
	monitors, err := s.repo.ListMonitors(ctx)
	if err != nil {
		return nil, cfg, fmt.Errorf("list degradation monitors: %w", err)
	}
	ids := make([]int64, 0, len(monitors))
	for _, monitor := range monitors {
		ids = append(ids, monitor.ID)
	}
	stats, err := s.repo.MonitorStats(ctx, ids)
	if err != nil {
		return nil, cfg, fmt.Errorf("load degradation monitor stats: %w", err)
	}
	views := make([]*DegradationMonitorView, 0, len(monitors))
	for _, monitor := range monitors {
		view := &DegradationMonitorView{
			Monitor:                  monitor,
			EffectiveIntervalMinutes: monitor.EffectiveIntervalMinutes(cfg),
			EffectiveReasoningEffort: monitor.EffectiveReasoningEffort(cfg),
			Running:                  s.scheduler != nil && s.scheduler.IsRunning(monitor.ID),
		}
		if stat := stats[monitor.ID]; stat != nil {
			view.Stats = *stat
		}
		views = append(views, view)
	}
	return views, cfg, nil
}

// GetMonitor loads one monitor.
func (s *DegradationMonitorService) GetMonitor(ctx context.Context, id int64) (*DegradationMonitor, error) {
	return s.repo.GetMonitor(ctx, id)
}

// CreateMonitor validates the input, creates the managed key and stores the monitor.
func (s *DegradationMonitorService) CreateMonitor(ctx context.Context, input DegradationMonitorInput, createdBy int64) (*DegradationMonitor, error) {
	if err := s.requireGateway(); err != nil {
		return nil, err
	}
	group, err := s.resolveGroup(ctx, input.GroupID, true)
	if err != nil {
		return nil, err
	}
	monitor := &DegradationMonitor{CreatedBy: createdBy}
	if err := s.applyMonitorInput(ctx, monitor, input, group); err != nil {
		return nil, err
	}
	if s.keys == nil {
		return nil, fmt.Errorf("degradation monitor key manager is not configured")
	}
	key, err := s.keys.CreateDegradationMonitorKey(ctx, createdBy, group.ID, degradationMonitorKeyName(monitor, group))
	if err != nil {
		return nil, fmt.Errorf("create managed degradation key: %w", err)
	}
	monitor.APIKeyID = key.ID
	if err := s.repo.CreateMonitor(ctx, monitor); err != nil {
		s.deleteKey(ctx, key.ID, createdBy, 0)
		return nil, fmt.Errorf("create degradation monitor: %w", err)
	}
	s.kick()
	return s.repo.GetMonitor(ctx, monitor.ID)
}

// UpdateMonitor edits a monitor. Moving it to another group rotates its managed key.
func (s *DegradationMonitorService) UpdateMonitor(ctx context.Context, id int64, input DegradationMonitorInput) (*DegradationMonitor, error) {
	existing, err := s.repo.GetMonitor(ctx, id)
	if err != nil {
		return nil, err
	}
	groupChanged := input.GroupID != existing.GroupID
	group, err := s.resolveGroup(ctx, input.GroupID, groupChanged)
	if err != nil {
		return nil, err
	}
	if err := s.applyMonitorInput(ctx, existing, input, group); err != nil {
		return nil, err
	}
	oldKeyID := existing.APIKeyID
	var newKeyID int64
	if groupChanged {
		if err := s.requireGateway(); err != nil {
			return nil, err
		}
		key, err := s.keys.CreateDegradationMonitorKey(ctx, existing.CreatedBy, group.ID, degradationMonitorKeyName(existing, group))
		if err != nil {
			return nil, fmt.Errorf("rotate managed degradation key: %w", err)
		}
		newKeyID = key.ID
		existing.APIKeyID = key.ID
	}
	if err := s.repo.UpdateMonitor(ctx, existing); err != nil {
		if newKeyID > 0 {
			s.deleteKey(ctx, newKeyID, existing.CreatedBy, existing.ID)
		}
		return nil, err
	}
	if newKeyID > 0 {
		s.deleteKey(ctx, oldKeyID, existing.CreatedBy, existing.ID)
	}
	s.kick()
	return s.repo.GetMonitor(ctx, id)
}

// DeleteMonitor removes a monitor, its stored animations and its managed key.
func (s *DegradationMonitorService) DeleteMonitor(ctx context.Context, id int64) error {
	existing, err := s.repo.GetMonitor(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteMonitor(ctx, id); err != nil {
		return err
	}
	s.deleteKey(ctx, existing.APIKeyID, existing.CreatedBy, existing.ID)
	return nil
}

// UpdateSortOrders persists a drag-and-drop order.
func (s *DegradationMonitorService) UpdateSortOrders(ctx context.Context, updates []DegradationSortOrderUpdate) error {
	if err := s.repo.UpdateMonitorSortOrders(ctx, updates); err != nil {
		return fmt.Errorf("update degradation monitor sort orders: %w", err)
	}
	return nil
}

// StorageStats reports how many animations a monitor keeps and their size.
func (s *DegradationMonitorService) StorageStats(ctx context.Context, id int64) (*DegradationStorageStats, error) {
	if _, err := s.repo.GetMonitor(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.StorageStats(ctx, id)
}

// ReleaseStorage deletes every finished animation stored for a monitor.
func (s *DegradationMonitorService) ReleaseStorage(ctx context.Context, id int64) (int64, error) {
	if _, err := s.repo.GetMonitor(ctx, id); err != nil {
		return 0, err
	}
	return s.repo.DeleteRuns(ctx, id)
}

// RunNow queues an immediate check of one monitor.
func (s *DegradationMonitorService) RunNow(ctx context.Context, id int64) error {
	if !s.Enabled(ctx) {
		return ErrDegradationDisabled
	}
	if _, err := s.repo.GetMonitor(ctx, id); err != nil {
		return err
	}
	if err := s.requireGateway(); err != nil {
		return err
	}
	if s.scheduler == nil {
		return ErrDegradationRunnerBusy
	}
	return s.scheduler.TriggerNow(id)
}

func (s *DegradationMonitorService) requireGateway() error {
	if s == nil || strings.TrimSpace(s.endpoint) == "" || s.attestor == nil {
		return ErrDegradationGatewayUnavailable
	}
	return nil
}

func (s *DegradationMonitorService) resolveGroup(ctx context.Context, groupID int64, requireActive bool) (*Group, error) {
	if groupID <= 0 {
		return nil, ErrDegradationMissingGroup
	}
	if s.groupReader == nil {
		return nil, fmt.Errorf("degradation monitor group reader is not configured")
	}
	group, err := s.groupReader.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, ErrGroupNotFound
	}
	if requireActive && group.Status != StatusActive {
		return nil, ErrDegradationGroupInactive
	}
	if !IsDegradationGroupPlatform(group.Platform) {
		return nil, ErrDegradationGroupPlatform
	}
	return group, nil
}

func (s *DegradationMonitorService) applyMonitorInput(ctx context.Context, monitor *DegradationMonitor, input DegradationMonitorInput, group *Group) error {
	name := strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(name) > DegradationNameMaxRunes {
		return ErrDegradationInvalidName
	}
	model := strings.TrimSpace(input.Model)
	if model == "" || len(model) > DegradationModelMaxLength {
		return ErrDegradationInvalidModel
	}
	effort := strings.ToLower(strings.TrimSpace(input.ReasoningEffort))
	if effort != "" && !IsDegradationReasoningEffort(effort) {
		return ErrDegradationInvalidEffort
	}
	protocol := strings.TrimSpace(input.Protocol)
	if protocol == "" {
		protocol = DefaultDegradationProtocol(group.Platform)
	}
	if !IsDegradationProtocol(protocol) {
		return ErrDegradationInvalidProtocol
	}
	if input.IntervalMinutes != 0 && (input.IntervalMinutes < DegradationMinIntervalMinutes || input.IntervalMinutes > DegradationMaxIntervalMinutes) {
		return ErrDegradationInvalidInterval
	}
	var promptID *int64
	if input.PromptID != nil && *input.PromptID > 0 {
		if _, err := s.repo.GetPrompt(ctx, *input.PromptID); err != nil {
			return err
		}
		id := *input.PromptID
		promptID = &id
	}
	monitor.Name = name
	monitor.GroupID = group.ID
	monitor.GroupName = group.Name
	monitor.GroupPlatform = group.Platform
	monitor.Model = model
	monitor.ReasoningEffort = effort
	monitor.Protocol = protocol
	monitor.IntervalMinutes = input.IntervalMinutes
	monitor.PromptID = promptID
	monitor.RandomPrompt = input.RandomPrompt
	monitor.Enabled = input.Enabled
	return nil
}

func degradationMonitorKeyName(monitor *DegradationMonitor, group *Group) string {
	if monitor != nil && strings.TrimSpace(monitor.Name) != "" {
		return monitor.Name
	}
	if group != nil {
		return group.Name
	}
	return "monitor"
}

func (s *DegradationMonitorService) deleteKey(ctx context.Context, keyID, ownerID, monitorID int64) {
	if s.keys == nil || keyID <= 0 {
		return
	}
	if err := s.keys.DeleteManagedMonitorKeyByID(ctx, keyID, ownerID); err != nil {
		slog.Warn("degradation_monitor: managed key cleanup failed", "monitor_id", monitorID, "api_key_id", keyID, "error", err)
	}
}

// ---------- user views ----------

// Overview returns enabled monitors with their latest results, without document bodies.
func (s *DegradationMonitorService) Overview(ctx context.Context) (*DegradationOverview, error) {
	cfg := s.GetConfig(ctx)
	overview := &DegradationOverview{Enabled: s.Enabled(ctx), DisplayLimit: cfg.MaxDisplayPerGroup, Monitors: []*DegradationUserMonitor{}}
	if !overview.Enabled {
		return overview, nil
	}
	monitors, err := s.repo.ListMonitors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list degradation monitors: %w", err)
	}
	visible := make([]*DegradationMonitor, 0, len(monitors))
	ids := make([]int64, 0, len(monitors))
	for _, monitor := range monitors {
		if !monitor.Enabled || monitor.GroupDeleted {
			continue
		}
		visible = append(visible, monitor)
		ids = append(ids, monitor.ID)
	}
	runs, err := s.repo.ListRecentRuns(ctx, ids, cfg.MaxDisplayPerGroup)
	if err != nil {
		return nil, fmt.Errorf("list degradation results: %w", err)
	}
	for _, monitor := range visible {
		name := monitor.Name
		if name == "" {
			name = monitor.GroupName
		}
		items := runs[monitor.ID]
		if items == nil {
			items = []*DegradationRun{}
		}
		overview.Monitors = append(overview.Monitors, &DegradationUserMonitor{
			ID:                       monitor.ID,
			Name:                     name,
			GroupName:                monitor.GroupName,
			Platform:                 monitor.GroupPlatform,
			Model:                    monitor.Model,
			Protocol:                 monitor.Protocol,
			EffectiveReasoningEffort: monitor.EffectiveReasoningEffort(cfg),
			EffectiveIntervalMinutes: monitor.EffectiveIntervalMinutes(cfg),
			LastRunAt:                monitor.LastRunAt,
			Runs:                     items,
		})
	}
	return overview, nil
}

// GetRunForUser returns one result with its document when its monitor is visible.
func (s *DegradationMonitorService) GetRunForUser(ctx context.Context, id int64) (*DegradationRun, error) {
	if !s.Enabled(ctx) {
		return nil, ErrDegradationRunNotFound
	}
	run, err := s.repo.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	monitor, err := s.repo.GetMonitor(ctx, run.MonitorID)
	if err != nil {
		if errors.Is(err, ErrDegradationMonitorNotFound) {
			return nil, ErrDegradationRunNotFound
		}
		return nil, err
	}
	if !monitor.Enabled || monitor.GroupDeleted {
		return nil, ErrDegradationRunNotFound
	}
	return run, nil
}

// GetRun returns one result for administrators.
func (s *DegradationMonitorService) GetRun(ctx context.Context, id int64) (*DegradationRun, error) {
	return s.repo.GetRun(ctx, id)
}
