package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type resourceLibraryRepository struct{ db *sql.DB }

func NewResourceLibraryRepository(db *sql.DB) service.ResourceLibraryRepository {
	return &resourceLibraryRepository{db: db}
}

func (r *resourceLibraryRepository) UserEmails(ctx context.Context, ids []int64) (map[int64]string, error) {
	result := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	// Deleted users stay resolvable so a result can be traced to its author.
	rows, err := r.db.QueryContext(ctx, `SELECT id, email FROM users WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		result[id] = email
	}
	return result, rows.Err()
}

func (r *resourceLibraryRepository) APIKeyGroups(ctx context.Context, ids []int64) (map[int64]int64, error) {
	result := make(map[int64]int64, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, COALESCE(group_id, 0) FROM api_keys WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, group int64
		if err := rows.Scan(&id, &group); err != nil {
			return nil, err
		}
		result[id] = group
	}
	return result, rows.Err()
}

func (r *resourceLibraryRepository) GroupNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	result := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, name FROM groups WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}
	return result, rows.Err()
}

// Billing reports the latest charged amount of each reservation: the settled
// amount, or the prepaid amount while settlement is pending.
func (r *resourceLibraryRepository) Billing(ctx context.Context, ids []string) (map[string]service.ResourceLibraryBilling, error) {
	result := make(map[string]service.ResourceLibraryBilling, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.id, r.state, r.group_id,
		COALESCE((SELECT e.amount FROM media_billing_events e WHERE e.reservation_id = r.id AND e.stage IN ('prepaid', 'settled') ORDER BY e.ordinal DESC LIMIT 1), 0)
		FROM media_billing_reservations r WHERE r.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, state string
		var billing service.ResourceLibraryBilling
		if err := rows.Scan(&id, &state, &billing.GroupID, &billing.Amount); err != nil {
			return nil, err
		}
		switch state {
		case "settled", "refunded":
			billing.State = state
		default:
			billing.State = "prepaid"
		}
		result[id] = billing
	}
	return result, rows.Err()
}

func (r *resourceLibraryRepository) SavedSources(ctx context.Context, taskIDs []string) (map[string]string, error) {
	result := make(map[string]string, len(taskIDs))
	if len(taskIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT source_task_id, source_index, id FROM resource_library_items WHERE source_task_id = ANY($1)`, pq.Array(taskIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var task, id string
		var index int
		if err := rows.Scan(&task, &index, &id); err != nil {
			return nil, err
		}
		result[service.ResourceSourceKey(task, index)] = id
	}
	return result, rows.Err()
}

const resourceLibrarySavedColumns = `id, kind, source_task_id, source_index, name, content_type, size_bytes, sha256,
	user_id, user_email, api_key_id, group_id, group_name, model, prompt, billing_id, cost, metadata,
	generated_at, source_expires_at, saved_by, saved_by_email, created_at`

func (r *resourceLibraryRepository) CreateSaved(ctx context.Context, item *service.ResourceLibrarySaved) (bool, error) {
	details := item.Details
	if details == nil {
		details = map[string]any{}
	}
	metadata, err := json.Marshal(details)
	if err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO resource_library_items (`+resourceLibrarySavedColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT ON CONSTRAINT resource_library_items_source_key DO NOTHING`,
		item.ID, item.Kind, item.SourceTaskID, item.SourceIndex, item.Name, item.ContentType, item.SizeBytes, item.SHA256,
		item.UserID, item.UserEmail, item.APIKeyID, item.GroupID, item.GroupName, item.Model, item.Prompt, item.BillingID,
		nullableFloat(item.Cost), metadata, item.GeneratedAt, item.SourceExpiresAt, item.SavedBy, item.SavedByEmail, item.CreatedAt)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

type resourceRowScanner interface {
	Scan(dest ...any) error
}

func scanResourceSaved(row resourceRowScanner) (*service.ResourceLibrarySaved, error) {
	var item service.ResourceLibrarySaved
	var cost sql.NullFloat64
	var metadata []byte
	var expires sql.NullTime
	if err := row.Scan(&item.ID, &item.Kind, &item.SourceTaskID, &item.SourceIndex, &item.Name, &item.ContentType,
		&item.SizeBytes, &item.SHA256, &item.UserID, &item.UserEmail, &item.APIKeyID, &item.GroupID, &item.GroupName,
		&item.Model, &item.Prompt, &item.BillingID, &cost, &metadata, &item.GeneratedAt, &expires,
		&item.SavedBy, &item.SavedByEmail, &item.CreatedAt); err != nil {
		return nil, err
	}
	if cost.Valid {
		value := cost.Float64
		item.Cost = &value
	}
	if expires.Valid {
		value := expires.Time
		item.SourceExpiresAt = &value
	}
	if len(metadata) > 0 && json.Unmarshal(metadata, &item.Details) != nil {
		item.Details = nil
	}
	return &item, nil
}

func (r *resourceLibraryRepository) GetSaved(ctx context.Context, id string) (*service.ResourceLibrarySaved, error) {
	item, err := scanResourceSaved(r.db.QueryRowContext(ctx, `SELECT `+resourceLibrarySavedColumns+` FROM resource_library_items WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrResourceNotFound
	}
	return item, err
}

func (r *resourceLibraryRepository) GetSavedBySource(ctx context.Context, taskID string, index int) (*service.ResourceLibrarySaved, error) {
	item, err := scanResourceSaved(r.db.QueryRowContext(ctx, `SELECT `+resourceLibrarySavedColumns+` FROM resource_library_items WHERE source_task_id = $1 AND source_index = $2`, taskID, index))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrResourceNotFound
	}
	return item, err
}

func escapeResourceLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func resourceSavedWhere(filter service.ResourceLibraryFilter) (string, []any) {
	clauses, args := []string{}, []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if filter.Kind != "" {
		add("kind = ?", filter.Kind)
	}
	if filter.Model != "" {
		add("model = ?", filter.Model)
	}
	if filter.GroupID > 0 {
		add("group_id = ?", filter.GroupID)
	}
	if filter.Query != "" {
		add("(name ILIKE ? OR source_task_id ILIKE ? OR user_email ILIKE ? OR prompt ILIKE ? OR model ILIKE ?)", "%"+escapeResourceLike(filter.Query)+"%")
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func (r *resourceLibraryRepository) ListSaved(ctx context.Context, filter service.ResourceLibraryFilter) ([]*service.ResourceLibrarySaved, int64, error) {
	where, args := resourceSavedWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_library_items`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (filter.Page - 1) * filter.PageSize
	pageArgs := append(append([]any{}, args...), filter.PageSize, offset)
	query := `SELECT ` + resourceLibrarySavedColumns + ` FROM resource_library_items` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := []*service.ResourceLibrarySaved{}
	for rows.Next() {
		item, err := scanResourceSaved(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *resourceLibraryRepository) SavedFacets(ctx context.Context) ([]string, []service.ResourceLibraryGroupOption, error) {
	models := []string{}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT model FROM resource_library_items WHERE model <> '' ORDER BY model`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	_ = rows.Close()
	groups := []service.ResourceLibraryGroupOption{}
	// The name snapshot keeps groups identifiable after they are renamed or deleted.
	rows, err = r.db.QueryContext(ctx, `SELECT group_id, MAX(group_name) FROM resource_library_items WHERE group_id > 0 GROUP BY group_id ORDER BY MAX(group_name), group_id`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var option service.ResourceLibraryGroupOption
		if err := rows.Scan(&option.ID, &option.Name); err != nil {
			return nil, nil, err
		}
		groups = append(groups, option)
	}
	return models, groups, rows.Err()
}

func (r *resourceLibraryRepository) DeleteSaved(ctx context.Context, id string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM resource_library_items WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
