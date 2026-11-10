package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetOperationsRechargeRanking ranks ordinary users by the balance they topped
// up in [start, end). It counts recharges exactly like the operations "today
// recharge" metric — paid balance orders plus used balance and administrator
// adjustment codes, without the PAY- codes that credit those orders — so the
// ranking adds up to that metric.
func (r *usageLogRepository) GetOperationsRechargeRanking(ctx context.Context, start, end time.Time, limit int) (*service.OperationsRechargeRankingData, error) {
	data := &service.OperationsRechargeRankingData{Users: make([]service.OperationsRechargeRankingEntry, 0)}
	const query = `
		WITH recharges AS (
			SELECT po.user_id, po.amount::double precision AS amount
			FROM payment_orders po
			WHERE po.order_type = 'balance' AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED')
				AND po.paid_at >= $1 AND po.paid_at < $2
			UNION ALL
			SELECT rc.used_by, rc.value::double precision
			FROM redeem_codes rc
			WHERE rc.status = 'used' AND rc.type IN ('balance', 'admin_balance') AND rc.value > 0
				AND rc.code NOT LIKE 'PAY-%' AND rc.used_at >= $1 AND rc.used_at < $2
		), per_user AS (
			SELECT u.id, u.email, SUM(r.amount) AS amount, COUNT(*) AS recharge_count
			FROM recharges r JOIN users u ON u.id = r.user_id
			WHERE u.role = 'user'
			GROUP BY u.id, u.email
		)
		SELECT id, email, amount, recharge_count,
			SUM(amount) OVER () AS total_amount, COUNT(*) OVER () AS total_users
		FROM per_user
		ORDER BY amount DESC, id ASC
		LIMIT $3
	`
	rows, err := r.sql.QueryContext(ctx, query, start, end, limit)
	if err != nil {
		return nil, fmt.Errorf("query operations recharge ranking: %w", err)
	}
	for rows.Next() {
		var entry service.OperationsRechargeRankingEntry
		if err := rows.Scan(&entry.UserID, &entry.Email, &entry.Amount, &entry.RechargeCount, &data.TotalAmount, &data.TotalUsers); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan operations recharge ranking: %w", err)
		}
		data.Users = append(data.Users, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate operations recharge ranking: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations recharge ranking rows: %w", err)
	}
	return data, nil
}
