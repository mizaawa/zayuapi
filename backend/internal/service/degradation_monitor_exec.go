package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
	"unicode/utf8"
)

const degradationFinalizeTimeout = 10 * time.Second

// DueMonitorIDs lists enabled monitors whose next check time has passed.
// Monitors whose group was deleted or disabled are skipped instead of
// producing a failed result every interval.
func (s *DegradationMonitorService) DueMonitorIDs(ctx context.Context, now time.Time) ([]int64, error) {
	cfg := s.GetConfig(ctx)
	monitors, err := s.repo.ListMonitors(ctx)
	if err != nil {
		return nil, err
	}
	due := make([]int64, 0)
	for _, monitor := range monitors {
		if !monitor.Enabled || monitor.GroupDeleted || monitor.GroupStatus != StatusActive {
			continue
		}
		interval := time.Duration(monitor.EffectiveIntervalMinutes(cfg)) * time.Minute
		if monitor.LastRunAt == nil || !monitor.LastRunAt.Add(interval).After(now) {
			due = append(due, monitor.ID)
		}
	}
	return due, nil
}

// ClaimScheduledRun atomically reserves the next scheduled check of a monitor.
func (s *DegradationMonitorService) ClaimScheduledRun(ctx context.Context, id int64, now time.Time) (bool, error) {
	monitor, err := s.repo.GetMonitor(ctx, id)
	if err != nil {
		return false, err
	}
	interval := time.Duration(monitor.EffectiveIntervalMinutes(s.GetConfig(ctx))) * time.Minute
	return s.repo.ClaimMonitorRun(ctx, id, now, now.Add(-interval))
}

// RecoverStaleRuns fails results left in the running state by a crashed process.
// The cutoff exceeds the longest possible check so live runs are never touched.
func (s *DegradationMonitorService) RecoverStaleRuns(ctx context.Context) {
	cfg := s.GetConfig(ctx)
	attempts := time.Duration(cfg.MaxRetries + 1)
	longest := attempts*time.Duration(cfg.TimeoutMinutes)*time.Minute + attempts*s.retryDelay + s.usageWait + 5*time.Minute
	count, err := s.repo.FailStaleRuns(ctx, s.now().Add(-longest), "check interrupted before it finished")
	if err != nil {
		slog.Warn("degradation_monitor: recover stale runs failed", "error", err)
		return
	}
	if count > 0 {
		slog.Info("degradation_monitor: recovered stale runs", "count", count)
	}
}

// ExecuteRun performs one check and stores its result. Errors are recorded on
// the result row; only persistence problems are logged.
func (s *DegradationMonitorService) ExecuteRun(ctx context.Context, id int64, trigger string) {
	monitor, err := s.repo.GetMonitor(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrDegradationMonitorNotFound) {
			slog.Warn("degradation_monitor: load monitor failed", "monitor_id", id, "error", err)
		}
		return
	}
	if trigger == DegradationTriggerManual {
		if err := s.repo.MarkMonitorRun(ctx, id, s.now()); err != nil {
			slog.Warn("degradation_monitor: mark manual run failed", "monitor_id", id, "error", err)
		}
	}
	cfg := s.GetConfig(ctx)
	promptID, promptName, prompt := s.resolvePrompt(ctx, monitor, cfg)
	run := &DegradationRun{
		MonitorID:       monitor.ID,
		Status:          DegradationRunStatusRunning,
		TriggerSource:   trigger,
		Model:           monitor.Model,
		ReasoningEffort: monitor.EffectiveReasoningEffort(cfg),
		Protocol:        monitor.Protocol,
		PromptID:        promptID,
		PromptName:      promptName,
		Prompt:          prompt,
		StartedAt:       s.now(),
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		slog.Error("degradation_monitor: create run failed", "monitor_id", id, "error", err)
		return
	}

	apiKeyID, sent := s.performRun(ctx, monitor, cfg, run)

	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), degradationFinalizeTimeout+s.usageWait)
	defer cancel()
	finished := s.now()
	run.FinishedAt = &finished
	if err := s.repo.FinishRun(finishCtx, run); err != nil {
		slog.Error("degradation_monitor: finish run failed", "monitor_id", id, "run_id", run.ID, "error", err)
	}
	if sent && apiKeyID > 0 {
		s.syncRunUsage(finishCtx, run, apiKeyID)
	}
	if _, err := s.repo.PruneRuns(finishCtx, monitor.ID, DegradationRunRetention); err != nil {
		slog.Warn("degradation_monitor: prune runs failed", "monitor_id", id, "error", err)
	}
}

// performRun fills run with the outcome. It returns the managed key used and
// whether at least one request reached the gateway.
func (s *DegradationMonitorService) performRun(ctx context.Context, monitor *DegradationMonitor, cfg DegradationConfig, run *DegradationRun) (int64, bool) {
	fail := func(message string) {
		run.Status = DegradationRunStatusFailed
		run.ErrorMessage = truncateDegradationError(message)
	}
	if err := s.requireGateway(); err != nil {
		fail(err.Error())
		return 0, false
	}
	group, err := s.resolveGroup(ctx, monitor.GroupID, true)
	if err != nil {
		fail("group unavailable: " + err.Error())
		return 0, false
	}
	key, err := s.ensureKey(ctx, monitor, group)
	if err != nil {
		fail("managed key unavailable: " + err.Error())
		return 0, false
	}
	request := degradationRequest{
		Endpoint: s.endpoint,
		APIKey:   key.Key,
		Protocol: monitor.Protocol,
		Model:    monitor.Model,
		Effort:   run.ReasoningEffort,
		Prompt:   run.Prompt,
		Timeout:  time.Duration(cfg.TimeoutMinutes) * time.Minute,
		Attestor: s.attestor,
	}
	var last *degradationAttempt
	sent := false
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 && !sleepContext(ctx, s.retryDelay) {
			break
		}
		run.Attempts = attempt + 1
		last = runDegradationAttempt(ctx, s.client, request)
		sent = sent || last.Sent
		if last.Err == nil || ctx.Err() != nil {
			break
		}
		slog.Info("degradation_monitor: attempt failed", "monitor_id", monitor.ID, "attempt", attempt+1, "error", sanitizeErrorMessageWithSecrets(last.Err.Error(), key.Key))
	}
	if last == nil {
		fail("check interrupted")
		return key.ID, sent
	}
	run.Output = last.Output
	run.ResponseModel = last.ResponseModel
	run.FirstTokenMs = last.FirstTokenMs
	duration := last.DurationMs
	run.DurationMs = &duration
	run.InputTokens = last.Usage.InputTokens
	run.OutputTokens = last.Usage.OutputTokens
	run.CacheReadTokens = last.Usage.CacheReadTokens
	run.CacheCreationTokens = last.Usage.CacheCreationTokens
	run.ReasoningTokens = last.ReasoningTokens
	if last.Err != nil {
		fail(sanitizeErrorMessageWithSecrets(last.Err.Error(), key.Key))
		return key.ID, sent
	}
	run.Status = DegradationRunStatusSuccess
	run.ErrorMessage = ""
	return key.ID, sent
}

// ensureKey returns the monitor's managed key, recreating it when it was
// removed or no longer matches the monitored group.
func (s *DegradationMonitorService) ensureKey(ctx context.Context, monitor *DegradationMonitor, group *Group) (*APIKey, error) {
	if s.keys == nil {
		return nil, errors.New("key manager is not configured")
	}
	key, err := s.keys.GetManagedMonitorKey(ctx, monitor.APIKeyID)
	if err != nil && !errors.Is(err, ErrAPIKeyNotFound) {
		// A transient read failure must not rotate a healthy credential.
		return nil, err
	}
	if err == nil && key != nil && key.IsActive() && key.GroupID != nil && *key.GroupID == monitor.GroupID {
		return key, nil
	}
	replacement, createErr := s.keys.CreateDegradationMonitorKey(ctx, monitor.CreatedBy, monitor.GroupID, degradationMonitorKeyName(monitor, group))
	if createErr != nil {
		return nil, createErr
	}
	if err := s.repo.UpdateMonitorAPIKey(ctx, monitor.ID, replacement.ID); err != nil {
		s.deleteKey(ctx, replacement.ID, monitor.CreatedBy, monitor.ID)
		return nil, err
	}
	s.deleteKey(ctx, monitor.APIKeyID, monitor.CreatedBy, monitor.ID)
	monitor.APIKeyID = replacement.ID
	return replacement, nil
}

// resolvePrompt selects the preset for this run: a random preset, the monitor's
// own preset, the global default, the first preset, then the built-in pelican.
func (s *DegradationMonitorService) resolvePrompt(ctx context.Context, monitor *DegradationMonitor, cfg DegradationConfig) (*int64, string, string) {
	prompts, err := s.repo.ListPrompts(ctx)
	if err != nil {
		slog.Warn("degradation_monitor: list prompts failed", "error", err)
		prompts = nil
	}
	pick := func(prompt *DegradationPrompt) (*int64, string, string) {
		id := prompt.ID
		return &id, prompt.Name, prompt.Prompt
	}
	if monitor.RandomPrompt && len(prompts) > 0 {
		return pick(prompts[s.randomIndex(len(prompts))])
	}
	for _, wanted := range []*int64{monitor.PromptID, &cfg.DefaultPromptID} {
		if wanted == nil || *wanted <= 0 {
			continue
		}
		for _, prompt := range prompts {
			if prompt.ID == *wanted {
				return pick(prompt)
			}
		}
	}
	if len(prompts) > 0 {
		return pick(prompts[0])
	}
	return nil, DegradationPelicanPromptName, DegradationPelicanPrompt
}

// syncRunUsage copies billed tokens and the upstream cost of every request in
// this run from the gateway usage logs, which are written asynchronously.
func (s *DegradationMonitorService) syncRunUsage(ctx context.Context, run *DegradationRun, apiKeyID int64) {
	from := run.StartedAt.Add(-time.Second)
	deadline := s.now().Add(s.usageWait)
	var previous *DegradationUsageSummary
	for {
		summary, err := s.repo.SumUsage(ctx, apiKeyID, from, s.now().Add(time.Second))
		if err != nil {
			slog.Warn("degradation_monitor: read usage failed", "run_id", run.ID, "error", err)
			return
		}
		// Wait until the request count stops growing so retries are included.
		if summary.Requests > 0 && previous != nil && previous.Requests == summary.Requests {
			s.applyUsage(ctx, run, summary)
			return
		}
		if summary.Requests > 0 {
			previous = summary
		}
		if !s.now().Before(deadline) {
			if previous != nil {
				s.applyUsage(ctx, run, previous)
			}
			return
		}
		if !sleepContext(ctx, s.usagePoll) {
			return
		}
	}
}

func (s *DegradationMonitorService) applyUsage(ctx context.Context, run *DegradationRun, summary *DegradationUsageSummary) {
	run.InputTokens = summary.InputTokens
	run.OutputTokens = summary.OutputTokens
	run.CacheReadTokens = summary.CacheReadTokens
	run.CacheCreationTokens = summary.CacheCreationTokens
	cost := summary.Cost
	run.Cost = &cost
	run.UsageSynced = true
	if err := s.repo.UpdateRunUsage(ctx, run); err != nil {
		slog.Warn("degradation_monitor: store usage failed", "run_id", run.ID, "error", err)
	}
}

func truncateDegradationError(message string) string {
	if len(message) <= degradationErrorMaxBytes {
		return message
	}
	cut := degradationErrorMaxBytes
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + "…"
}

// sleepContext waits for d and reports false when ctx ends first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
