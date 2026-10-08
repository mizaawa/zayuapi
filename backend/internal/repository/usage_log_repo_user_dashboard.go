package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetUserDashboardStatsWithOptions keeps current-day queries bounded and resolves
// platforms after grouping, so joins operate on routes rather than every log row.
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
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	where := "ul.user_id = $1"
	if !includeTotals {
		// Include the previous day's last five minutes at midnight for RPM/TPM.
		where += " AND ul.created_at >= LEAST($2::timestamptz, $3::timestamptz)"
	}
	if hideMonitor {
		where += " AND " + channelMonitorUsageLogCondition("ul")
	}
	query := `WITH route_usage AS MATERIALIZED (
		SELECT group_id, account_id,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS cost,
			COALESCE(SUM(actual_cost), 0) AS actual_cost,
			COALESCE(SUM(duration_ms), 0) AS duration_ms, COUNT(duration_ms) AS duration_count,
			COUNT(*) FILTER (WHERE created_at >= $2) AS today_requests,
			COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $2), 0) AS today_input_tokens,
			COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $2), 0) AS today_output_tokens,
			COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $2), 0) AS today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $2), 0) AS today_cache_read_tokens,
			COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $2), 0) AS today_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $2), 0) AS today_actual_cost,
			COUNT(*) FILTER (WHERE created_at >= $3) AS recent_requests,
			COALESCE(SUM(input_tokens + output_tokens) FILTER (WHERE created_at >= $3), 0) AS recent_tokens,
			COUNT(*) FILTER (WHERE actual_cost > 0) AS platform_requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) FILTER (WHERE actual_cost > 0), 0) AS platform_tokens,
			COALESCE(SUM(actual_cost) FILTER (WHERE actual_cost > 0), 0) AS platform_cost,
			COUNT(*) FILTER (WHERE actual_cost > 0 AND created_at >= $2) AS platform_today_requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) FILTER (WHERE actual_cost > 0 AND created_at >= $2), 0) AS platform_today_tokens,
			COALESCE(SUM(actual_cost) FILTER (WHERE actual_cost > 0 AND created_at >= $2), 0) AS platform_today_cost
		FROM usage_logs ul WHERE ` + where + ` GROUP BY group_id, account_id
	)
	SELECT COALESCE(` + usageLogEffectivePlatformExpr + `, '') AS platform,
		SUM(requests), SUM(input_tokens), SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
		SUM(cost), SUM(actual_cost), SUM(duration_ms), SUM(duration_count),
		SUM(today_requests), SUM(today_input_tokens), SUM(today_output_tokens), SUM(today_cache_creation_tokens), SUM(today_cache_read_tokens),
		SUM(today_cost), SUM(today_actual_cost), SUM(recent_requests), SUM(recent_tokens),
		SUM(platform_requests), SUM(platform_tokens), SUM(platform_cost),
		SUM(platform_today_requests), SUM(platform_today_tokens), SUM(platform_today_cost)
	FROM route_usage ul
	LEFT JOIN groups g ON g.id = ul.group_id
	LEFT JOIN accounts a ON a.id = ul.account_id
	GROUP BY 1 ORDER BY SUM(platform_cost) DESC, platform`
	rows, err := r.sql.QueryContext(ctx, query, userID, today, fiveMinutesAgo)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			stats, err = nil, closeErr
		}
	}()
	var duration, durationCount, recentRequests, recentTokens int64
	for rows.Next() {
		var part UserDashboardStats
		var platform PlatformDashboardStats
		var d, dc, requests, tokens int64
		if err = rows.Scan(&platform.Platform,
			&part.TotalRequests, &part.TotalInputTokens, &part.TotalOutputTokens, &part.TotalCacheCreationTokens, &part.TotalCacheReadTokens,
			&part.TotalCost, &part.TotalActualCost, &d, &dc,
			&part.TodayRequests, &part.TodayInputTokens, &part.TodayOutputTokens, &part.TodayCacheCreationTokens, &part.TodayCacheReadTokens,
			&part.TodayCost, &part.TodayActualCost, &requests, &tokens,
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
		recentRequests += requests
		recentTokens += tokens
		if platform.Platform != "" && platform.TotalRequests > 0 {
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
	stats.Rpm, stats.Tpm = recentRequests/5, recentTokens/5
	return stats, nil
}
