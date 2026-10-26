package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageLogRepository) GetOperationsDashboardData(
	ctx context.Context,
	todayStart, todayEnd, rangeStart, rangeEnd, trendStart, trendEnd time.Time,
	userTimezone string,
	groupID int64,
) (*service.OperationsDashboardData, error) {
	data := &service.OperationsDashboardData{
		Trend:  make([]service.OperationsProfitTrendPoint, 0),
		Groups: make([]service.OperationsGroupProfitStat, 0),
		Models: make([]service.OperationsModelProfitStat, 0),
	}
	data.Metrics.TodayRevenue = make(map[string]float64)

	const metricsQuery = `
		SELECT
			COALESCE((SELECT SUM(ul.actual_cost)::double precision
				FROM usage_logs ul JOIN users u ON u.id = ul.user_id
				WHERE u.role = 'user' AND ul.created_at >= $1 AND ul.created_at < $2
				  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(po.amount)::double precision FROM payment_orders po JOIN users u ON u.id = po.user_id
				WHERE u.role = 'user' AND po.order_type = 'balance'
				  AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED') AND po.paid_at >= $1 AND po.paid_at < $2), 0) +
			COALESCE((SELECT SUM(rc.value)::double precision FROM redeem_codes rc JOIN users u ON u.id = rc.used_by
				WHERE u.role = 'user' AND rc.status = 'used' AND rc.type IN ('balance', 'admin_balance')
				  AND rc.value > 0 AND rc.used_at >= $1 AND rc.used_at < $2), 0),
			COALESCE((SELECT SUM(ul.actual_cost)::double precision
				FROM usage_logs ul JOIN api_keys mk ON mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor'
				WHERE ul.created_at >= $1 AND ul.created_at < $2), 0),
			COALESCE((SELECT SUM(uc.actual_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE uc.bucket_date = DATE '0001-01-01' AND u.role = 'user'
			  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(f.amount)::double precision FROM dashboard_user_financials f
				JOIN users u ON u.id = f.user_id WHERE f.metric = 'balance' AND u.role = 'user' AND u.deleted_at IS NULL), 0),
			COALESCE((SELECT SUM(uc.actual_cost)::double precision FROM usage_counters uc
				JOIN api_keys mk ON mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor'
				WHERE uc.bucket_date = DATE '0001-01-01'), 0),
			COALESCE((SELECT SUM(f.amount)::double precision FROM dashboard_user_financials f
				JOIN users u ON u.id = f.user_id WHERE f.metric = 'balance' AND u.role = 'admin' AND u.deleted_at IS NULL), 0)
	`
	if err := scanSingleRow(ctx, r.sql, metricsQuery, []any{todayStart, todayEnd},
		&data.Metrics.TodayUserConsumption,
		&data.Metrics.TodayUserRecharge,
		&data.Metrics.TodayMonitorConsumption,
		&data.Metrics.TotalUserConsumption,
		&data.Metrics.TotalUserBalance,
		&data.Metrics.TotalMonitorConsumption,
		&data.Metrics.AdminBalance,
	); err != nil {
		return nil, fmt.Errorf("query operations dashboard metrics: %w", err)
	}

	revenueQuery := `
		SELECT CASE WHEN UPPER(TRIM(COALESCE(po.provider_snapshot->>'currency', ''))) ~ '^[A-Z]{3}$'
				THEN UPPER(TRIM(po.provider_snapshot->>'currency')) ELSE 'CNY' END AS currency,
			COALESCE(SUM(po.pay_amount), 0)::double precision AS amount
		FROM payment_orders po JOIN users u ON u.id = po.user_id
		WHERE u.role = 'user' AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED')
		  AND po.paid_at >= $1 AND po.paid_at < $2
		GROUP BY currency ORDER BY currency
	`
	revenueRows, err := r.sql.QueryContext(ctx, revenueQuery, todayStart, todayEnd)
	if err != nil {
		return nil, fmt.Errorf("query operations dashboard revenue: %w", err)
	}
	for revenueRows.Next() {
		var currency string
		var amount float64
		if err := revenueRows.Scan(&currency, &amount); err != nil {
			_ = revenueRows.Close()
			return nil, err
		}
		data.Metrics.TodayRevenue[currency] = amount
	}
	if err := revenueRows.Err(); err != nil {
		_ = revenueRows.Close()
		return nil, err
	}
	if err := revenueRows.Close(); err != nil {
		return nil, fmt.Errorf("close operations dashboard revenue rows: %w", err)
	}

	trendQuery := `
		SELECT TO_CHAR(ul.created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS date,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost,
			COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
			COALESCE(SUM(ul.actual_cost - COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS profit
		FROM usage_logs ul JOIN users u ON u.id = ul.user_id
		WHERE u.role = 'user' AND ul.created_at >= $1 AND ul.created_at < $2
		  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
		GROUP BY date ORDER BY date ASC
	`
	rows, err := r.sql.QueryContext(ctx, trendQuery, trendStart, trendEnd, userTimezone)
	if err != nil {
		return nil, fmt.Errorf("query operations dashboard trend: %w", err)
	}
	for rows.Next() {
		var point service.OperationsProfitTrendPoint
		if err := rows.Scan(&point.Date, &point.UpstreamCost, &point.UserCost, &point.Profit); err != nil {
			_ = rows.Close()
			return nil, err
		}
		data.Trend = append(data.Trend, point)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations dashboard trend rows: %w", err)
	}

	groupQuery := `
		WITH usage_by_group AS (
			SELECT COALESCE(ul.group_id, 0) AS group_id,
				COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
				COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost
			FROM usage_logs ul JOIN users u ON u.id = ul.user_id
			WHERE u.role = 'user' AND ul.created_at >= $1 AND ul.created_at < $2
			  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
			GROUP BY COALESCE(ul.group_id, 0)
		)
		SELECT g.id, g.name, g.rate_multiplier::double precision,
			COALESCE(ug.user_cost, 0), COALESCE(ug.upstream_cost, 0)
		FROM groups g LEFT JOIN usage_by_group ug ON ug.group_id = g.id
		UNION ALL
		SELECT 0, 'Unassigned', 1::double precision, COALESCE(ug.user_cost, 0), COALESCE(ug.upstream_cost, 0)
		FROM usage_by_group ug WHERE ug.group_id = 0
		ORDER BY 1 ASC
	`
	rows, err = r.sql.QueryContext(ctx, groupQuery, rangeStart, rangeEnd)
	if err != nil {
		return nil, fmt.Errorf("query operations dashboard groups: %w", err)
	}
	for rows.Next() {
		var row service.OperationsGroupProfitStat
		if err := rows.Scan(&row.GroupID, &row.GroupName, &row.RateMultiplier, &row.UserCost, &row.UpstreamCost); err != nil {
			_ = rows.Close()
			return nil, err
		}
		data.Groups = append(data.Groups, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations dashboard group rows: %w", err)
	}

	modelQuery := `
		SELECT COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS model,
			COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost
		FROM usage_logs ul JOIN users u ON u.id = ul.user_id
		WHERE u.role = 'user' AND ul.created_at >= $1 AND ul.created_at < $2
		  AND (($3 = 0) OR ($3 = -1 AND COALESCE(ul.group_id, 0) = 0) OR COALESCE(ul.group_id, 0) = $3)
		  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
		GROUP BY 1 ORDER BY 1
	`
	rows, err = r.sql.QueryContext(ctx, modelQuery, rangeStart, rangeEnd, groupID)
	if err != nil {
		return nil, fmt.Errorf("query operations dashboard models: %w", err)
	}
	for rows.Next() {
		var row service.OperationsModelProfitStat
		if err := rows.Scan(&row.Model, &row.UserCost, &row.UpstreamCost); err != nil {
			_ = rows.Close()
			return nil, err
		}
		data.Models = append(data.Models, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations dashboard model rows: %w", err)
	}

	return data, nil
}
