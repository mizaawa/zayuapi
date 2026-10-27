package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetOperationsUserSuggestions searches active ordinary-user email addresses
// without running any spend aggregation while the admin is typing.
func (r *usageLogRepository) GetOperationsUserSuggestions(ctx context.Context, search string) ([]service.OperationsUserSuggestion, error) {
	result := make([]service.OperationsUserSuggestion, 0)
	if search == "" {
		return result, nil
	}
	const query = `
		SELECT id, email FROM users
		WHERE role = 'user' AND deleted_at IS NULL
			AND POSITION(LOWER($1) IN LOWER(email)) > 0
		ORDER BY email ASC LIMIT 20
	`
	rows, err := r.sql.QueryContext(ctx, query, search)
	if err != nil {
		return nil, fmt.Errorf("query operations user suggestions: %w", err)
	}
	for rows.Next() {
		var user service.OperationsUserSuggestion
		if err := rows.Scan(&user.UserID, &user.Email); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan operations user suggestion: %w", err)
		}
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate operations user suggestions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations user suggestions rows: %w", err)
	}
	return result, nil
}

// GetOperationsUsersData returns a spend-ranked user list and, when requested,
// the selected user's group and model breakdown for the same date range.
func (r *usageLogRepository) GetOperationsUsersData(
	ctx context.Context, rangeStart, rangeEnd time.Time, search string, userID int64,
) (*service.OperationsUsersData, error) {
	data := &service.OperationsUsersData{Users: make([]service.OperationsUserSpendStat, 0)}
	const usersQuery = `
		SELECT u.id, u.email,
			COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost
		FROM users u
		LEFT JOIN usage_logs ul ON ul.user_id = u.id AND ul.created_at >= $1 AND ul.created_at < $2
			AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
		WHERE u.role = 'user' AND u.deleted_at IS NULL
			AND ($3 = '' OR POSITION(LOWER($3) IN LOWER(u.email)) > 0)
		GROUP BY u.id, u.email
		ORDER BY user_cost DESC, u.id ASC
		LIMIT CASE WHEN $3 = '' THEN 25 ELSE 100 END
	`
	rows, err := r.sql.QueryContext(ctx, usersQuery, rangeStart, rangeEnd, search)
	if err != nil {
		return nil, fmt.Errorf("query operations users: %w", err)
	}
	for rows.Next() {
		var user service.OperationsUserSpendStat
		if err := rows.Scan(&user.UserID, &user.Email, &user.UserCost, &user.UpstreamCost); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan operations user: %w", err)
		}
		data.Users = append(data.Users, user)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate operations users: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations users rows: %w", err)
	}
	if userID <= 0 {
		return data, nil
	}

	detail := &service.OperationsUserDetail{
		UserID: userID,
		Groups: make([]service.OperationsGroupProfitStat, 0),
		Models: make([]service.OperationsModelProfitStat, 0),
	}
	if err := scanSingleRow(ctx, r.sql,
		`SELECT COALESCE((SELECT email FROM users WHERE id = $1 AND role = 'user' AND deleted_at IS NULL), '')`, []any{userID}, &detail.Email); err != nil {
		return nil, fmt.Errorf("query operations user: %w", err)
	}
	if detail.Email == "" {
		return data, nil
	}

	const groupsQuery = `
		SELECT COALESCE(ul.group_id, 0) AS group_id,
			CASE WHEN COALESCE(ul.group_id, 0) = 0 THEN 'Unassigned'
				ELSE COALESCE(g.name, 'Group #' || ul.group_id::text) END AS group_name,
			COALESCE(g.platform, 'custom') AS platform,
			COALESCE(g.rate_multiplier, 1)::double precision AS rate_multiplier,
			COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost
		FROM usage_logs ul LEFT JOIN groups g ON g.id = ul.group_id
		WHERE ul.user_id = $3 AND ul.created_at >= $1 AND ul.created_at < $2
			AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
		GROUP BY 1, 2, 3, 4 ORDER BY user_cost DESC, group_id ASC
	`
	rows, err = r.sql.QueryContext(ctx, groupsQuery, rangeStart, rangeEnd, userID)
	if err != nil {
		return nil, fmt.Errorf("query operations user groups: %w", err)
	}
	for rows.Next() {
		var group service.OperationsGroupProfitStat
		if err := rows.Scan(&group.GroupID, &group.GroupName, &group.Platform, &group.RateMultiplier, &group.UserCost, &group.UpstreamCost); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan operations user group: %w", err)
		}
		detail.Groups = append(detail.Groups, group)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate operations user groups: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations user groups rows: %w", err)
	}

	const modelsQuery = `
		SELECT COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS model,
			COALESCE(SUM(ul.actual_cost), 0)::double precision AS user_cost,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)::double precision AS upstream_cost,
			STRING_AGG(DISTINCT COALESCE(g.platform, 'custom'), ',' ORDER BY COALESCE(g.platform, 'custom')) AS platforms
		FROM usage_logs ul LEFT JOIN groups g ON g.id = ul.group_id
		WHERE ul.user_id = $3 AND ul.created_at >= $1 AND ul.created_at < $2
			AND NOT EXISTS (SELECT 1 FROM api_keys mk WHERE mk.id = ul.api_key_id AND mk.purpose = 'channel_monitor')
		GROUP BY 1 ORDER BY user_cost DESC, model ASC
	`
	rows, err = r.sql.QueryContext(ctx, modelsQuery, rangeStart, rangeEnd, userID)
	if err != nil {
		return nil, fmt.Errorf("query operations user models: %w", err)
	}
	for rows.Next() {
		var model service.OperationsModelProfitStat
		var platforms string
		if err := rows.Scan(&model.Model, &model.UserCost, &model.UpstreamCost, &platforms); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan operations user model: %w", err)
		}
		model.Platforms = strings.Split(platforms, ",")
		detail.Models = append(detail.Models, model)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate operations user models: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close operations user models rows: %w", err)
	}
	data.Detail = detail
	return data, nil
}
