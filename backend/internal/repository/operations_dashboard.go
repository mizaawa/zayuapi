package repository

import (
	"context"
	"fmt"
	"strings"
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
	data.Metrics.YesterdayRevenue = make(map[string]float64)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	const metricsQuery = `
		SELECT
			COALESCE((SELECT SUM(uc.actual_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE u.role = 'user' AND uc.bucket_date = $1::date
				  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(po.amount)::double precision FROM payment_orders po JOIN users u ON u.id = po.user_id
				WHERE u.role = 'user' AND po.order_type = 'balance'
				  AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED') AND po.paid_at >= $2 AND po.paid_at < $3), 0) +
			COALESCE((SELECT SUM(rc.value)::double precision FROM redeem_codes rc JOIN users u ON u.id = rc.used_by
				WHERE u.role = 'user' AND rc.status = 'used' AND rc.type IN ('balance', 'admin_balance')
				  AND rc.value > 0 AND rc.used_at >= $2 AND rc.used_at < $3), 0),
			COALESCE((SELECT SUM(uc.account_cost)::double precision
				FROM usage_counters uc JOIN api_keys mk ON mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor'
				WHERE uc.bucket_date = $1::date), 0),
			COALESCE((SELECT SUM(uc.actual_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE uc.bucket_date = DATE '0001-01-01' AND u.role = 'user'
			  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(f.amount)::double precision FROM dashboard_user_financials f
				JOIN users u ON u.id = f.user_id WHERE f.metric = 'balance' AND u.role = 'user' AND u.deleted_at IS NULL), 0),
			COALESCE((SELECT SUM(uc.account_cost)::double precision FROM usage_counters uc
				JOIN api_keys mk ON mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor'
				WHERE uc.bucket_date = DATE '0001-01-01'), 0),
			COALESCE((SELECT SUM(f.amount)::double precision FROM dashboard_user_financials f
				JOIN users u ON u.id = f.user_id WHERE f.metric = 'balance' AND u.role = 'admin' AND u.deleted_at IS NULL), 0),
			COALESCE((SELECT SUM(uc.account_cost)::double precision FROM usage_counters uc
				JOIN users u ON u.id = uc.user_id
				WHERE u.role = 'user' AND uc.bucket_date = $1::date
				  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(uc.actual_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE u.role = 'user' AND uc.bucket_date = $4::date
				  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			COALESCE((SELECT SUM(po.amount)::double precision FROM payment_orders po JOIN users u ON u.id = po.user_id
				WHERE u.role = 'user' AND po.order_type = 'balance'
				  AND po.status IN ('PAID', 'RECHARGING', 'COMPLETED') AND po.paid_at >= $5 AND po.paid_at < $2), 0) +
			COALESCE((SELECT SUM(rc.value)::double precision FROM redeem_codes rc JOIN users u ON u.id = rc.used_by
				WHERE u.role = 'user' AND rc.status = 'used' AND rc.type IN ('balance', 'admin_balance')
				  AND rc.value > 0 AND rc.used_at >= $5 AND rc.used_at < $2), 0),
			COALESCE((SELECT SUM(uc.account_cost)::double precision
				FROM usage_counters uc JOIN api_keys mk ON mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor'
				WHERE uc.bucket_date = $4::date), 0),
			COALESCE((SELECT SUM(uc.account_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE u.role = 'user' AND uc.bucket_date = $4::date
				  AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = uc.api_key_id AND mk.purpose = 'channel_monitor')), 0),
			(SELECT CASE WHEN $4::date >= t.started_on THEN
				COALESCE((SELECT SUM(d.amount)::double precision FROM dashboard_balance_daily_changes d
					WHERE d.bucket_date = $4::date AND d.role = 'user'), 0)
				ELSE NULL END FROM dashboard_balance_tracking t WHERE t.singleton),
			COALESCE((SELECT SUM(uc.actual_cost)::double precision
				FROM usage_counters uc JOIN users u ON u.id = uc.user_id
				WHERE u.role = 'admin' AND uc.bucket_date = $4::date), 0)
	`
	var todayUserUpstreamCost, yesterdayUserUpstreamCost float64
	if err := scanSingleRow(ctx, r.sql, metricsQuery, []any{todayStart.Format("2006-01-02"), todayStart, todayEnd, yesterdayStart.Format("2006-01-02"), yesterdayStart},
		&data.Metrics.TodayUserConsumption,
		&data.Metrics.TodayUserRecharge,
		&data.Metrics.TodayMonitorConsumption,
		&data.Metrics.TotalUserConsumption,
		&data.Metrics.TotalUserBalance,
		&data.Metrics.TotalMonitorConsumption,
		&data.Metrics.AdminBalance,
		&todayUserUpstreamCost,
		&data.Metrics.YesterdayUserConsumption,
		&data.Metrics.YesterdayUserRecharge,
		&data.Metrics.YesterdayMonitorConsumption,
		&yesterdayUserUpstreamCost,
		&data.Metrics.YesterdayUserBalanceChange,
		&data.Metrics.YesterdayAdminConsumption,
	); err != nil {
		return nil, fmt.Errorf("query operations dashboard metrics: %w", err)
	}
	// Monitor probes are free for site users but still spend upstream quota.
	data.Metrics.TodayRevenue["USD"] = data.Metrics.TodayUserConsumption - todayUserUpstreamCost - data.Metrics.TodayMonitorConsumption
	data.Metrics.YesterdayRevenue["USD"] = data.Metrics.YesterdayUserConsumption - yesterdayUserUpstreamCost - data.Metrics.YesterdayMonitorConsumption

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
		SELECT g.id, g.name, g.platform, g.rate_multiplier::double precision,
			COALESCE(ug.user_cost, 0), COALESCE(ug.upstream_cost, 0)
		FROM groups g LEFT JOIN usage_by_group ug ON ug.group_id = g.id
		UNION ALL
		SELECT 0, 'Unassigned', 'custom', 1::double precision, COALESCE(ug.user_cost, 0), COALESCE(ug.upstream_cost, 0)
		FROM usage_by_group ug WHERE ug.group_id = 0
		ORDER BY 1 ASC
	`
	rows, err = r.sql.QueryContext(ctx, groupQuery, rangeStart, rangeEnd)
	if err != nil {
		return nil, fmt.Errorf("query operations dashboard groups: %w", err)
	}
	for rows.Next() {
		var row service.OperationsGroupProfitStat
		if err := rows.Scan(&row.GroupID, &row.GroupName, &row.Platform, &row.RateMultiplier, &row.UserCost, &row.UpstreamCost); err != nil {
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
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost,
			STRING_AGG(DISTINCT COALESCE(g.platform, 'custom'), ',' ORDER BY COALESCE(g.platform, 'custom')) AS platforms
		FROM usage_logs ul JOIN users u ON u.id = ul.user_id
		LEFT JOIN groups g ON g.id = ul.group_id
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
		var platforms string
		if err := rows.Scan(&row.Model, &row.UserCost, &row.UpstreamCost, &platforms); err != nil {
			_ = rows.Close()
			return nil, err
		}
		row.Platforms = strings.Split(platforms, ",")
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
