package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageSettlementReceipt struct {
	Command service.UsageBillingCommand
	Usage   service.UsageLog
}

func (r *usageBillingRepository) StageUsage(ctx context.Context, cmd *service.UsageBillingCommand, usage *service.UsageLog) error {
	if cmd == nil || usage == nil || cmd.RequestID == "" || cmd.RequestID != usage.RequestID || cmd.UserID != usage.UserID || cmd.APIKeyID != usage.APIKeyID || cmd.AccountID != usage.AccountID {
		return service.ErrUsageBillingRequestConflict
	}
	cmd.Normalize()
	row := *usage
	// Never serialize hydrated relationships: accounts and API keys contain credentials.
	row.User, row.APIKey, row.Account, row.Group, row.Subscription, row.BillingEvents = nil, nil, nil, nil, nil, nil
	payload, err := json.Marshal(usageSettlementReceipt{Command: *cmd, Usage: row})
	if err != nil {
		return err
	}
	// A replay must not replace the winner's original price or time. Check both active and archived claims
	// before making a receipt; completed requests must not reappear after usage-log retention.
	var fingerprint string
	err = r.db.QueryRowContext(ctx, `WITH claimed AS (
		SELECT request_fingerprint FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2
		UNION ALL SELECT request_fingerprint FROM usage_billing_dedup_archive WHERE request_id=$1 AND api_key_id=$2
	), staged AS (
		INSERT INTO pending_usage_settlements (request_id,api_key_id,fingerprint,receipt)
		SELECT $1,$2,$3,$4::jsonb WHERE NOT EXISTS (SELECT 1 FROM claimed)
		ON CONFLICT (request_id,api_key_id) DO UPDATE SET request_id=EXCLUDED.request_id
		RETURNING fingerprint
	) SELECT fingerprint FROM staged UNION ALL SELECT request_fingerprint FROM claimed LIMIT 1`,
		cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, string(payload)).Scan(&fingerprint)
	if err == nil && fingerprint != cmd.RequestFingerprint {
		return service.ErrUsageBillingRequestConflict
	}
	return err
}

func (r *usageBillingRepository) SettleUsage(ctx context.Context, requestID string, keyID int64, fingerprint string) (*service.UsageBillingApplyResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var payload []byte
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,receipt FROM pending_usage_settlements
		WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE`, requestID, keyID).Scan(&stored, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		matched, err := usageBillingClaimMatches(ctx, tx, requestID, keyID, fingerprint)
		if err != nil {
			return nil, err
		}
		if !matched {
			return nil, fmt.Errorf("missing pending usage settlement for key %d", keyID)
		}
		return &service.UsageBillingApplyResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	if stored != fingerprint {
		return nil, service.ErrUsageBillingRequestConflict
	}
	var receipt usageSettlementReceipt
	if err := json.Unmarshal(payload, &receipt); err != nil {
		return nil, err
	}
	cmd := &receipt.Command
	if cmd.RequestID != requestID || cmd.APIKeyID != keyID || cmd.RequestFingerprint != fingerprint {
		return nil, service.ErrUsageBillingRequestConflict
	}
	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	result := &service.UsageBillingApplyResult{Applied: applied}
	if applied {
		if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
			return nil, err
		}
		inserted, err := (&usageLogRepository{}).createSingle(ctx, tx, &receipt.Usage)
		if err != nil {
			return nil, err
		}
		if !inserted {
			return nil, service.ErrUsageBillingRequestConflict
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM pending_usage_settlements WHERE request_id=$1 AND api_key_id=$2`, requestID, keyID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *usageBillingRepository) ClaimPendingUsage(ctx context.Context) (*service.UsageBillingRecoveryKey, error) {
	var k service.UsageBillingRecoveryKey
	// Lease a single ready receipt. A crashed worker or failed transaction leaves it available to retry.
	err := r.db.QueryRowContext(ctx, `WITH next AS (
		SELECT request_id,api_key_id FROM pending_usage_settlements WHERE retry_at <= now()
		ORDER BY retry_at LIMIT 1 FOR UPDATE SKIP LOCKED
	) UPDATE pending_usage_settlements p SET attempts=attempts+1,
		retry_at=now()+make_interval(secs => LEAST(600, 120 + LEAST(attempts,8)*60)) FROM next
		WHERE p.request_id=next.request_id AND p.api_key_id=next.api_key_id
		RETURNING p.request_id,p.api_key_id,p.fingerprint,(p.receipt->'Command'->>'UserID')::bigint,
		COALESCE((p.receipt->'Usage'->>'GroupID')::bigint,0),COALESCE(p.receipt->'Command'->>'Platform','')`).Scan(
		&k.RequestID, &k.APIKeyID, &k.Fingerprint, &k.UserID, &k.GroupID, &k.Platform)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}
