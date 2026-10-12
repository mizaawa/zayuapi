package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// A pending payment still needs its provisional row for final statistics deltas.
const finalizedMediaUsageCondition = `NOT EXISTS (SELECT 1 FROM media_billing_reservations mr
	WHERE mr.id=usage_logs.request_id AND mr.api_key_id=usage_logs.api_key_id
	AND mr.state IN ('reserved','pending'))`

func writeMediaBillingEvent(ctx context.Context, tx *sql.Tx, id string, ordinal int, stage string, amount float64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO media_billing_events (reservation_id,ordinal,stage,amount) VALUES ($1,$2,$3,$4)`, id, ordinal, stage, service.QuantizeUsageBillingAmount(amount))
	return err
}

// The financial usage row is updated, never duplicated by audit stages. Its
// existing statistics triggers apply the difference between old and new costs.
func writeMediaUsageLog(ctx context.Context, tx *sql.Tx, usage *service.UsageLog) error {
	if usage.UserID <= 0 || usage.APIKeyID <= 0 || usage.AccountID <= 0 || usage.Model == "" {
		// Legacy/internal receipts without display metadata still have audit events.
		return nil
	}
	prepared := prepareUsageLogInsert(usage)
	columns := strings.Split(usageLogSelectColumns, ", ")[1:]
	if len(columns) != len(prepared.args) {
		return fmt.Errorf("media usage column mismatch")
	}
	values, updates := make([]string, len(columns)), []string{}
	for i, column := range columns {
		values[i] = fmt.Sprintf("$%d", i+1)
		switch column {
		case "user_id", "api_key_id", "request_id", "created_at":
		default:
			updates = append(updates, column+"=EXCLUDED."+column)
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO usage_logs ("+strings.Join(columns, ",")+") VALUES ("+strings.Join(values, ",")+") ON CONFLICT (request_id,api_key_id) DO UPDATE SET "+strings.Join(updates, ",")+" WHERE usage_logs.user_id=EXCLUDED.user_id", prepared.args...)
	return err
}

func (r *usageLogRepository) hydrateMediaBillingEvents(ctx context.Context, logs []service.UsageLog) error {
	args, owners := []any{}, []string{}
	for _, log := range logs {
		if !strings.HasPrefix(log.RequestID, "media:") {
			continue
		}
		owners = append(owners, fmt.Sprintf("($%d,$%d,$%d)", len(args)+1, len(args)+2, len(args)+3))
		args = append(args, log.RequestID, log.UserID, log.APIKeyID)
	}
	if len(owners) == 0 {
		return nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT e.reservation_id,r.user_id,r.api_key_id,e.stage,e.amount,e.created_at
		FROM media_billing_events e JOIN media_billing_reservations r ON r.id=e.reservation_id
		WHERE (r.id,r.user_id,r.api_key_id) IN (`+strings.Join(owners, ",")+`) ORDER BY e.reservation_id,e.ordinal`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	type owner struct {
		id        string
		user, key int64
	}
	events := map[owner][]service.MediaBillingEvent{}
	for rows.Next() {
		var o owner
		var event service.MediaBillingEvent
		if err := rows.Scan(&o.id, &o.user, &o.key, &event.Stage, &event.Amount, &event.CreatedAt); err != nil {
			return err
		}
		events[o] = append(events[o], event)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range logs {
		logs[i].BillingEvents = events[owner{logs[i].RequestID, logs[i].UserID, logs[i].APIKeyID}]
	}
	return nil
}
