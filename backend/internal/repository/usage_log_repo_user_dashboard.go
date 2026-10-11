package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetUserDashboardStatsWithOptions reads durable lifetime and daily counters.
// Only the bounded five-minute performance window still reads request logs.
func (r *usageLogRepository) GetUserDashboardStatsWithOptions(ctx context.Context, userID int64, includeTotals, hideMonitor bool) (stats *UserDashboardStats, err error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user ID")
	}
	stats = &UserDashboardStats{TotalsPending: !includeTotals}
	if err = scanSingleRow(ctx, r.sql, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status = $2)
		FROM api_keys WHERE user_id = $1 AND deleted_at IS NULL AND purpose = ''`,
		[]any{userID, service.StatusActive}, &stats.TotalAPIKeys, &stats.ActiveAPIKeys); err != nil {
		return nil, err
	}
	today := timezone.Today()
	where := "ul.user_id = $1 AND ul.bucket_date IN (DATE '0001-01-01', $2::date)"
	if !includeTotals {
		where = "ul.user_id = $1 AND ul.bucket_date = $2::date"
	}
	if hideMonitor {
		where += " AND " + channelMonitorUsageLogCondition("ul")
	}
	query := `WITH route_usage AS MATERIALIZED (
		SELECT group_id, account_id,
			COALESCE(SUM(requests) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS requests,
			COALESCE(SUM(input_tokens) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS input_tokens,
			COALESCE(SUM(output_tokens) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS actual_cost,
			COALESCE(SUM(total_duration_ms) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS duration_ms,
			COALESCE(SUM(duration_count) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS duration_count,
			COALESCE(SUM(requests) FILTER (WHERE bucket_date = $2::date), 0) AS today_requests,
			COALESCE(SUM(input_tokens) FILTER (WHERE bucket_date = $2::date), 0) AS today_input_tokens,
			COALESCE(SUM(output_tokens) FILTER (WHERE bucket_date = $2::date), 0) AS today_output_tokens,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE bucket_date = $2::date), 0) AS today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE bucket_date = $2::date), 0) AS today_cache_read_tokens,
			COALESCE(SUM(total_cost) FILTER (WHERE bucket_date = $2::date), 0) AS today_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE bucket_date = $2::date), 0) AS today_actual_cost,
			COALESCE(SUM(billable_requests) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS platform_requests,
			COALESCE(SUM(billable_tokens) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS platform_tokens,
			COALESCE(SUM(billable_cost) FILTER (WHERE bucket_date = DATE '0001-01-01'), 0) AS platform_cost,
			COALESCE(SUM(billable_requests) FILTER (WHERE bucket_date = $2::date), 0) AS platform_today_requests,
			COALESCE(SUM(billable_tokens) FILTER (WHERE bucket_date = $2::date), 0) AS platform_today_tokens,
			COALESCE(SUM(billable_cost) FILTER (WHERE bucket_date = $2::date), 0) AS platform_today_cost
		FROM usage_counters ul WHERE ` + where + ` GROUP BY group_id, account_id
	)
	SELECT COALESCE(` + usageLogEffectivePlatformExpr + `, '') AS platform,
		SUM(requests), SUM(input_tokens), SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
		SUM(cost), SUM(actual_cost), SUM(duration_ms), SUM(duration_count),
		SUM(today_requests), SUM(today_input_tokens), SUM(today_output_tokens), SUM(today_cache_creation_tokens), SUM(today_cache_read_tokens),
		SUM(today_cost), SUM(today_actual_cost),
		SUM(platform_requests), SUM(platform_tokens), SUM(platform_cost),
		SUM(platform_today_requests), SUM(platform_today_tokens), SUM(platform_today_cost)
	FROM route_usage ul
	LEFT JOIN groups g ON g.id = ul.group_id
	LEFT JOIN accounts a ON a.id = ul.account_id
	GROUP BY 1 ORDER BY SUM(platform_cost) DESC, platform`
	rows, err := r.sql.QueryContext(ctx, query, userID, today.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			stats, err = nil, closeErr
		}
	}()
	var duration, durationCount int64
	for rows.Next() {
		var part UserDashboardStats
		var platform PlatformDashboardStats
		var d, dc int64
		if err = rows.Scan(&platform.Platform,
			&part.TotalRequests, &part.TotalInputTokens, &part.TotalOutputTokens, &part.TotalCacheCreationTokens, &part.TotalCacheReadTokens,
			&part.TotalCost, &part.TotalActualCost, &d, &dc,
			&part.TodayRequests, &part.TodayInputTokens, &part.TodayOutputTokens, &part.TodayCacheCreationTokens, &part.TodayCacheReadTokens,
			&part.TodayCost, &part.TodayActualCost,
			&platform.TotalRequests, &platform.TotalTokens, &platform.TotalActualCost,
			&platform.TodayRequests, &platform.TodayTokens, &platform.TodayActualCost); err != nil {
			return nil, err
		}
		stats.TotalRequests += part.TotalRequests
		stats.TotalInputTokens += part.TotalInputTokens
		stats.TotalOutputTokens += part.TotalOutputTokens
		stats.TotalCacheCreationTokens += part.TotalCacheCreationTokens
		stats.TotalCacheReadTokens += part.TotalCacheReadTokens
		stats.TotalCost += part.TotalCost
		stats.TotalActualCost += part.TotalActualCost
		stats.TodayRequests += part.TodayRequests
		stats.TodayInputTokens += part.TodayInputTokens
		stats.TodayOutputTokens += part.TodayOutputTokens
		stats.TodayCacheCreationTokens += part.TodayCacheCreationTokens
		stats.TodayCacheReadTokens += part.TodayCacheReadTokens
		stats.TodayCost += part.TodayCost
		stats.TodayActualCost += part.TodayActualCost
		duration += d
		durationCount += dc
		if platform.Platform != "" && (platform.TotalRequests > 0 || platform.TodayRequests > 0) {
			if !includeTotals {
				platform.TotalRequests, platform.TotalTokens, platform.TotalActualCost = 0, 0, 0
			}
			stats.ByPlatform = append(stats.ByPlatform, platform)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if includeTotals {
		stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
		if durationCount > 0 {
			stats.AverageDurationMs = float64(duration) / float64(durationCount)
		}
	} else {
		stats.TotalRequests, stats.TotalInputTokens, stats.TotalOutputTokens = 0, 0, 0
		stats.TotalCacheCreationTokens, stats.TotalCacheReadTokens = 0, 0
		stats.TotalCost, stats.TotalActualCost = 0, 0
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	performanceQuery := `SELECT COUNT(*), COALESCE(SUM(input_tokens + output_tokens), 0)
		FROM usage_logs ul WHERE ul.user_id = $1 AND ul.created_at >= $2`
	if hideMonitor {
		performanceQuery += " AND " + channelMonitorUsageLogCondition("ul")
	}
	var recentRequests, recentTokens int64
	if err := scanSingleRow(ctx, r.sql, performanceQuery, []any{userID, time.Now().Add(-5 * time.Minute)},
		&recentRequests, &recentTokens); err != nil {
		return nil, err
	}
	stats.Rpm, stats.Tpm = recentRequests/5, recentTokens/5
	return stats, nil
}
