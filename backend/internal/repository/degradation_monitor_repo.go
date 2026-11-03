package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type degradationMonitorRepository struct{ sql sqlExecutor }

// NewDegradationMonitorRepository stores degradation monitors with plain SQL.
func NewDegradationMonitorRepository(db *sql.DB) service.DegradationMonitorRepository {
	return newDegradationMonitorRepositoryWithSQL(db)
}

// newDegradationMonitorRepositoryWithSQL accepts a database or a transaction.
func newDegradationMonitorRepositoryWithSQL(executor sqlExecutor) *degradationMonitorRepository {
	return &degradationMonitorRepository{sql: executor}
}

type degradationRowScanner interface {
	Scan(dest ...any) error
}

// degradationRow defers a single-row query until Scan, like *sql.Row, but works
// with any sqlExecutor (a *sql.DB, a *sql.Tx or an ent transaction).
type degradationRow struct {
	ctx   context.Context
	q     sqlQueryer
	query string
	args  []any
}

func (r degradationRow) Scan(dest ...any) error {
	return scanSingleRow(r.ctx, r.q, r.query, r.args, dest...)
}

func (r *degradationMonitorRepository) queryRow(ctx context.Context, query string, args ...any) degradationRow {
	return degradationRow{ctx: ctx, q: r.sql, query: query, args: args}
}

// ---------- prompts ----------

const degradationPromptColumns = `id, name, prompt, builtin_key, sort_order, created_at, updated_at`

func scanDegradationPrompt(row degradationRowScanner) (*service.DegradationPrompt, error) {
	var item service.DegradationPrompt
	if err := row.Scan(&item.ID, &item.Name, &item.Prompt, &item.BuiltinKey, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *degradationMonitorRepository) ListPrompts(ctx context.Context) ([]*service.DegradationPrompt, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT `+degradationPromptColumns+` FROM degradation_prompts ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.DegradationPrompt, 0)
	for rows.Next() {
		item, err := scanDegradationPrompt(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *degradationMonitorRepository) GetPrompt(ctx context.Context, id int64) (*service.DegradationPrompt, error) {
	item, err := scanDegradationPrompt(r.queryRow(ctx, `SELECT `+degradationPromptColumns+` FROM degradation_prompts WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDegradationPromptNotFound
	}
	return item, err
}

func (r *degradationMonitorRepository) CreatePrompt(ctx context.Context, prompt *service.DegradationPrompt) error {
	return r.queryRow(ctx, `INSERT INTO degradation_prompts (name, prompt, sort_order)
		VALUES ($1, $2, COALESCE((SELECT MAX(sort_order) FROM degradation_prompts), 0) + 10)
		RETURNING id, builtin_key, sort_order, created_at, updated_at`,
		prompt.Name, prompt.Prompt,
	).Scan(&prompt.ID, &prompt.BuiltinKey, &prompt.SortOrder, &prompt.CreatedAt, &prompt.UpdatedAt)
}

func (r *degradationMonitorRepository) UpdatePrompt(ctx context.Context, prompt *service.DegradationPrompt) error {
	err := r.queryRow(ctx, `UPDATE degradation_prompts SET name = $2, prompt = $3, updated_at = NOW()
		WHERE id = $1 RETURNING updated_at`, prompt.ID, prompt.Name, prompt.Prompt).Scan(&prompt.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrDegradationPromptNotFound
	}
	return err
}

func (r *degradationMonitorRepository) DeletePrompt(ctx context.Context, id int64) error {
	result, err := r.sql.ExecContext(ctx, `DELETE FROM degradation_prompts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	return requireAffected(result, service.ErrDegradationPromptNotFound)
}

// ---------- monitors ----------

const degradationMonitorSelect = `SELECT m.id, m.name, m.group_id, COALESCE(g.name, ''), COALESCE(g.platform, ''),
	COALESCE(g.status, ''), (g.id IS NULL OR g.deleted_at IS NOT NULL), m.model, m.reasoning_effort, m.protocol,
	m.interval_minutes, m.prompt_id, COALESCE(p.name, ''), m.random_prompt, m.enabled, m.sort_order, m.api_key_id,
	m.created_by, m.last_run_at, m.created_at, m.updated_at
	FROM degradation_monitors m
	LEFT JOIN groups g ON g.id = m.group_id
	LEFT JOIN degradation_prompts p ON p.id = m.prompt_id`

func scanDegradationMonitor(row degradationRowScanner) (*service.DegradationMonitor, error) {
	var item service.DegradationMonitor
	var promptID sql.NullInt64
	var lastRunAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Name, &item.GroupID, &item.GroupName, &item.GroupPlatform, &item.GroupStatus,
		&item.GroupDeleted, &item.Model, &item.ReasoningEffort, &item.Protocol, &item.IntervalMinutes, &promptID,
		&item.PromptName, &item.RandomPrompt, &item.Enabled, &item.SortOrder, &item.APIKeyID, &item.CreatedBy,
		&lastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if promptID.Valid {
		value := promptID.Int64
		item.PromptID = &value
	}
	if lastRunAt.Valid {
		value := lastRunAt.Time
		item.LastRunAt = &value
	}
	return &item, nil
}

func (r *degradationMonitorRepository) ListMonitors(ctx context.Context) ([]*service.DegradationMonitor, error) {
	rows, err := r.sql.QueryContext(ctx, degradationMonitorSelect+` ORDER BY m.sort_order ASC, m.id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.DegradationMonitor, 0)
	for rows.Next() {
		item, err := scanDegradationMonitor(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *degradationMonitorRepository) GetMonitor(ctx context.Context, id int64) (*service.DegradationMonitor, error) {
	item, err := scanDegradationMonitor(r.queryRow(ctx, degradationMonitorSelect+` WHERE m.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDegradationMonitorNotFound
	}
	return item, err
}

func nullableDegradationID(id *int64) any {
	if id == nil || *id <= 0 {
		return nil
	}
	return *id
}

func (r *degradationMonitorRepository) CreateMonitor(ctx context.Context, monitor *service.DegradationMonitor) error {
	return r.queryRow(ctx, `INSERT INTO degradation_monitors
		(name, group_id, model, reasoning_effort, protocol, interval_minutes, prompt_id, random_prompt, enabled,
		 sort_order, api_key_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		 COALESCE((SELECT MAX(sort_order) FROM degradation_monitors), 0) + 10, $10, $11)
		RETURNING id, sort_order, created_at, updated_at`,
		monitor.Name, monitor.GroupID, monitor.Model, monitor.ReasoningEffort, monitor.Protocol, monitor.IntervalMinutes,
		nullableDegradationID(monitor.PromptID), monitor.RandomPrompt, monitor.Enabled, monitor.APIKeyID, monitor.CreatedBy,
	).Scan(&monitor.ID, &monitor.SortOrder, &monitor.CreatedAt, &monitor.UpdatedAt)
}

func (r *degradationMonitorRepository) UpdateMonitor(ctx context.Context, monitor *service.DegradationMonitor) error {
	result, err := r.sql.ExecContext(ctx, `UPDATE degradation_monitors SET name = $2, group_id = $3, model = $4,
		reasoning_effort = $5, protocol = $6, interval_minutes = $7, prompt_id = $8, random_prompt = $9, enabled = $10,
		api_key_id = $11, updated_at = NOW() WHERE id = $1`,
		monitor.ID, monitor.Name, monitor.GroupID, monitor.Model, monitor.ReasoningEffort, monitor.Protocol,
		monitor.IntervalMinutes, nullableDegradationID(monitor.PromptID), monitor.RandomPrompt, monitor.Enabled, monitor.APIKeyID)
	if err != nil {
		return err
	}
	return requireAffected(result, service.ErrDegradationMonitorNotFound)
}

func (r *degradationMonitorRepository) DeleteMonitor(ctx context.Context, id int64) error {
	result, err := r.sql.ExecContext(ctx, `DELETE FROM degradation_monitors WHERE id = $1`, id)
	if err != nil {
		return err
	}
	return requireAffected(result, service.ErrDegradationMonitorNotFound)
}

func (r *degradationMonitorRepository) UpdateMonitorSortOrders(ctx context.Context, updates []service.DegradationSortOrderUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(updates))
	orders := make([]int64, 0, len(updates))
	for _, update := range updates {
		ids = append(ids, update.ID)
		orders = append(orders, int64(update.SortOrder))
	}
	// A single statement keeps the reorder atomic without opening a nested transaction.
	_, err := r.sql.ExecContext(ctx, `UPDATE degradation_monitors AS m SET sort_order = u.sort_order
		FROM UNNEST($1::bigint[], $2::bigint[]) AS u(id, sort_order) WHERE m.id = u.id`,
		pq.Array(ids), pq.Array(orders))
	return err
}

func (r *degradationMonitorRepository) UpdateMonitorAPIKey(ctx context.Context, id, apiKeyID int64) error {
	result, err := r.sql.ExecContext(ctx, `UPDATE degradation_monitors SET api_key_id = $2, updated_at = NOW() WHERE id = $1`, id, apiKeyID)
	if err != nil {
		return err
	}
	return requireAffected(result, service.ErrDegradationMonitorNotFound)
}

func (r *degradationMonitorRepository) ClaimMonitorRun(ctx context.Context, id int64, now, dueBefore time.Time) (bool, error) {
	result, err := r.sql.ExecContext(ctx, `UPDATE degradation_monitors SET last_run_at = $2
		WHERE id = $1 AND enabled = TRUE AND (last_run_at IS NULL OR last_run_at <= $3)`, id, now, dueBefore)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (r *degradationMonitorRepository) MarkMonitorRun(ctx context.Context, id int64, now time.Time) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE degradation_monitors SET last_run_at = $2 WHERE id = $1`, id, now)
	return err
}

func (r *degradationMonitorRepository) MonitorStats(ctx context.Context, ids []int64) (map[int64]*service.DegradationMonitorStats, error) {
	stats := make(map[int64]*service.DegradationMonitorStats, len(ids))
	if len(ids) == 0 {
		return stats, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT monitor_id, COUNT(*), COALESCE(SUM(octet_length(output)), 0)
		FROM degradation_runs WHERE monitor_id = ANY($1) GROUP BY monitor_id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		stat := &service.DegradationMonitorStats{}
		if err := rows.Scan(&id, &stat.StoredRuns, &stat.StoredBytes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		stats[id] = stat
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	rows, err = r.sql.QueryContext(ctx, `SELECT DISTINCT ON (monitor_id) monitor_id, status, COALESCE(finished_at, started_at), error_message
		FROM degradation_runs WHERE monitor_id = ANY($1) ORDER BY monitor_id, id DESC`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var status, message string
		var at time.Time
		if err := rows.Scan(&id, &status, &at, &message); err != nil {
			return nil, err
		}
		stat := stats[id]
		if stat == nil {
			stat = &service.DegradationMonitorStats{}
			stats[id] = stat
		}
		stat.LastRunStatus = status
		stat.LastRunAt = &at
		stat.LastRunError = message
	}
	return stats, rows.Err()
}

// ---------- runs ----------

const degradationRunSummaryColumns = `id, monitor_id, status, trigger_source, model, response_model, reasoning_effort, protocol,
	prompt_id, prompt_name, octet_length(output), error_message, attempts, first_token_ms, duration_ms, input_tokens,
	output_tokens, cache_read_tokens, cache_creation_tokens, reasoning_tokens, cost, usage_synced, started_at, finished_at`

type degradationRunNullables struct {
	promptID     sql.NullInt64
	firstTokenMs sql.NullInt64
	durationMs   sql.NullInt64
	cost         sql.NullFloat64
	finishedAt   sql.NullTime
}

func (n degradationRunNullables) apply(run *service.DegradationRun) {
	if n.promptID.Valid {
		value := n.promptID.Int64
		run.PromptID = &value
	}
	if n.firstTokenMs.Valid {
		value := int(n.firstTokenMs.Int64)
		run.FirstTokenMs = &value
	}
	if n.durationMs.Valid {
		value := int(n.durationMs.Int64)
		run.DurationMs = &value
	}
	if n.cost.Valid {
		value := n.cost.Float64
		run.Cost = &value
	}
	if n.finishedAt.Valid {
		value := n.finishedAt.Time
		run.FinishedAt = &value
	}
}

func scanDegradationRunSummary(row degradationRowScanner) (*service.DegradationRun, error) {
	var run service.DegradationRun
	var nullable degradationRunNullables
	if err := row.Scan(&run.ID, &run.MonitorID, &run.Status, &run.TriggerSource, &run.Model, &run.ResponseModel,
		&run.ReasoningEffort, &run.Protocol, &nullable.promptID, &run.PromptName, &run.OutputBytes, &run.ErrorMessage,
		&run.Attempts, &nullable.firstTokenMs, &nullable.durationMs, &run.InputTokens, &run.OutputTokens,
		&run.CacheReadTokens, &run.CacheCreationTokens, &run.ReasoningTokens, &nullable.cost, &run.UsageSynced,
		&run.StartedAt, &nullable.finishedAt); err != nil {
		return nil, err
	}
	nullable.apply(&run)
	return &run, nil
}

func nullableDegradationInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableDegradationFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (r *degradationMonitorRepository) CreateRun(ctx context.Context, run *service.DegradationRun) error {
	return r.queryRow(ctx, `INSERT INTO degradation_runs
		(monitor_id, status, trigger_source, model, reasoning_effort, protocol, prompt_id, prompt_name, prompt, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		run.MonitorID, run.Status, run.TriggerSource, run.Model, run.ReasoningEffort, run.Protocol,
		nullableDegradationID(run.PromptID), run.PromptName, run.Prompt, run.StartedAt,
	).Scan(&run.ID)
}

func (r *degradationMonitorRepository) FinishRun(ctx context.Context, run *service.DegradationRun) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE degradation_runs SET status = $2, response_model = $3, output = $4,
		error_message = $5, attempts = $6, first_token_ms = $7, duration_ms = $8, input_tokens = $9, output_tokens = $10,
		cache_read_tokens = $11, cache_creation_tokens = $12, reasoning_tokens = $13, finished_at = $14 WHERE id = $1`,
		run.ID, run.Status, run.ResponseModel, run.Output, run.ErrorMessage, run.Attempts,
		nullableDegradationInt(run.FirstTokenMs), nullableDegradationInt(run.DurationMs), run.InputTokens,
		run.OutputTokens, run.CacheReadTokens, run.CacheCreationTokens, run.ReasoningTokens, run.FinishedAt)
	return err
}

func (r *degradationMonitorRepository) UpdateRunUsage(ctx context.Context, run *service.DegradationRun) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE degradation_runs SET input_tokens = $2, output_tokens = $3,
		cache_read_tokens = $4, cache_creation_tokens = $5, cost = $6, usage_synced = $7 WHERE id = $1`,
		run.ID, run.InputTokens, run.OutputTokens, run.CacheReadTokens, run.CacheCreationTokens,
		nullableDegradationFloat(run.Cost), run.UsageSynced)
	return err
}

func (r *degradationMonitorRepository) GetRun(ctx context.Context, id int64) (*service.DegradationRun, error) {
	var run service.DegradationRun
	var nullable degradationRunNullables
	err := r.queryRow(ctx, `SELECT id, monitor_id, status, trigger_source, model, response_model,
		reasoning_effort, protocol, prompt_id, prompt_name, prompt, output, octet_length(output), error_message, attempts,
		first_token_ms, duration_ms, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		reasoning_tokens, cost, usage_synced, started_at, finished_at FROM degradation_runs WHERE id = $1`, id,
	).Scan(&run.ID, &run.MonitorID, &run.Status, &run.TriggerSource, &run.Model, &run.ResponseModel,
		&run.ReasoningEffort, &run.Protocol, &nullable.promptID, &run.PromptName, &run.Prompt, &run.Output,
		&run.OutputBytes, &run.ErrorMessage, &run.Attempts, &nullable.firstTokenMs, &nullable.durationMs,
		&run.InputTokens, &run.OutputTokens, &run.CacheReadTokens, &run.CacheCreationTokens, &run.ReasoningTokens,
		&nullable.cost, &run.UsageSynced, &run.StartedAt, &nullable.finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDegradationRunNotFound
	}
	if err != nil {
		return nil, err
	}
	nullable.apply(&run)
	return &run, nil
}

func (r *degradationMonitorRepository) ListRecentRuns(ctx context.Context, monitorIDs []int64, perMonitor int) (map[int64][]*service.DegradationRun, error) {
	result := make(map[int64][]*service.DegradationRun, len(monitorIDs))
	if len(monitorIDs) == 0 || perMonitor <= 0 {
		return result, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT `+degradationRunSummaryColumns+` FROM (
			SELECT r.*, ROW_NUMBER() OVER (PARTITION BY r.monitor_id ORDER BY r.id DESC) AS position
			FROM degradation_runs r WHERE r.monitor_id = ANY($1)
		) ranked WHERE position <= $2 ORDER BY monitor_id, id DESC`, pq.Array(monitorIDs), perMonitor)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, err := scanDegradationRunSummary(rows)
		if err != nil {
			return nil, err
		}
		result[run.MonitorID] = append(result[run.MonitorID], run)
	}
	return result, rows.Err()
}

func (r *degradationMonitorRepository) StorageStats(ctx context.Context, monitorID int64) (*service.DegradationStorageStats, error) {
	stats := &service.DegradationStorageStats{}
	err := r.queryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(octet_length(output) + octet_length(prompt)), 0)
		FROM degradation_runs WHERE monitor_id = $1`, monitorID).Scan(&stats.Count, &stats.Bytes)
	return stats, err
}

func (r *degradationMonitorRepository) DeleteRuns(ctx context.Context, monitorID int64) (int64, error) {
	result, err := r.sql.ExecContext(ctx, `DELETE FROM degradation_runs WHERE monitor_id = $1 AND status <> 'running'`, monitorID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *degradationMonitorRepository) PruneRuns(ctx context.Context, monitorID int64, keep int) (int64, error) {
	if keep <= 0 {
		return 0, fmt.Errorf("keep must be positive")
	}
	result, err := r.sql.ExecContext(ctx, `DELETE FROM degradation_runs WHERE monitor_id = $1 AND id NOT IN (
		SELECT id FROM degradation_runs WHERE monitor_id = $1 ORDER BY id DESC LIMIT $2)`, monitorID, keep)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *degradationMonitorRepository) FailStaleRuns(ctx context.Context, startedBefore time.Time, message string) (int64, error) {
	result, err := r.sql.ExecContext(ctx, `UPDATE degradation_runs SET status = 'failed', error_message = $2,
		finished_at = NOW() WHERE status = 'running' AND started_at < $1`, startedBefore, message)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SumUsage totals the gateway usage logs written for a managed key. The cost is
// the upstream (account) cost, the same figure the operations dashboard counts
// as monitor consumption.
func (r *degradationMonitorRepository) SumUsage(ctx context.Context, apiKeyID int64, from, to time.Time) (*service.DegradationUsageSummary, error) {
	summary := &service.DegradationUsageSummary{}
	err := r.queryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_creation_tokens), 0),
		COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0)::double precision
		FROM usage_logs WHERE api_key_id = $1 AND created_at >= $2 AND created_at <= $3`, apiKeyID, from, to,
	).Scan(&summary.Requests, &summary.InputTokens, &summary.OutputTokens, &summary.CacheReadTokens,
		&summary.CacheCreationTokens, &summary.Cost)
	return summary, err
}

func requireAffected(result sql.Result, notFound error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return notFound
	}
	return nil
}
