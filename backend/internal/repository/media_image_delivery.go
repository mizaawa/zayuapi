package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.MediaImageBillingRepository = (*usageBillingRepository)(nil)
var _ service.MediaImageRecoveryRepository = (*usageBillingRepository)(nil)

// PendingMedia batches can wait behind slow upstream checks. Read the live
// lease/outcome immediately before deciding whether an image worker expired.
func (r *usageBillingRepository) GetMediaImageReservation(ctx context.Context, id string) (*service.MediaBillingReservation, error) {
	var data, final []byte
	var refund bool
	var lease, dispatch sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT quote,final_quote,refund_requested,queue_lease_until,dispatch_started_at
		FROM media_billing_reservations WHERE id=$1 AND workbench_kind='image' AND state IN ('reserved','pending')`, id).Scan(&data, &final, &refund, &lease, &dispatch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result service.MediaBillingReservation
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	result.RefundPending = refund
	result.QueueLeaseUntil, result.Dispatched = lease.Time, dispatch.Valid
	if len(final) > 0 {
		if err := json.Unmarshal(final, &result.FinalQuote); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

func (r *usageBillingRepository) BindMediaImageTask(ctx context.Context, id, taskID string, userID, keyID int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE media_billing_reservations
		SET quote=jsonb_set(quote,'{image_task_id}',to_jsonb($2::text)),updated_at=NOW()
		WHERE id=$1 AND user_id=$3 AND api_key_id=$4 AND workbench_kind='image' AND state='reserved'
		AND dispatch_started_at IS NULL AND NOT refund_requested AND final_quote IS NULL
		AND COALESCE(quote->>'image_task_id','') IN ('',$2)`, id, taskID, userID, keyID)
	return mediaQueueUpdateResult(result, err)
}

func (r *usageBillingRepository) StageMediaImageOutcome(ctx context.Context, id string, quote *service.MediaBillingQuote) error {
	if quote == nil || quote.Command.RequestID != id {
		return service.ErrUsageBillingRequestConflict
	}
	data, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE media_billing_reservations
		SET quote=jsonb_set(quote,'{image_outcome}',$2::jsonb),updated_at=NOW(),next_check_at=NOW()
		WHERE id=$1 AND user_id=$3 AND api_key_id=$4 AND workbench_kind='image' AND state='reserved'
		AND COALESCE(quote->>'image_task_id','')<>'' AND NOT refund_requested AND final_quote IS NULL
		AND (NOT (quote ? 'image_outcome') OR quote->'image_outcome'=$2::jsonb)`, id, data, quote.Command.UserID, quote.Command.APIKeyID)
	return mediaQueueUpdateResult(result, err)
}
