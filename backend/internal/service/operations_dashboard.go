package service

import (
	"context"
	"errors"
	"time"
)

var ErrOperationsDashboardUnavailable = errors.New("operations dashboard is unavailable")

type OperationsDashboardMetrics struct {
	TodayUserConsumption        float64            `json:"today_user_consumption"`
	TodayUserRecharge           float64            `json:"today_user_recharge"`
	TodayMonitorConsumption     float64            `json:"today_monitor_consumption"`
	TodayRevenue                map[string]float64 `json:"today_revenue"`
	TotalUserConsumption        float64            `json:"total_user_consumption"`
	TotalUserBalance            float64            `json:"total_user_balance"`
	TotalMonitorConsumption     float64            `json:"total_monitor_consumption"`
	AdminBalance                float64            `json:"admin_balance"`
	YesterdayUserConsumption    float64            `json:"yesterday_user_consumption"`
	YesterdayUserRecharge       float64            `json:"yesterday_user_recharge"`
	YesterdayMonitorConsumption float64            `json:"yesterday_monitor_consumption"`
	YesterdayRevenue            map[string]float64 `json:"yesterday_revenue"`
	YesterdayUserBalanceChange  *float64           `json:"yesterday_user_balance_change"`
	YesterdayAdminConsumption   float64            `json:"yesterday_admin_consumption"`
}

type OperationsProfitTrendPoint struct {
	Date         string  `json:"date"`
	UpstreamCost float64 `json:"upstream_cost"`
	UserCost     float64 `json:"user_cost"`
	Profit       float64 `json:"profit"`
}

type OperationsGroupProfitStat struct {
	GroupID        int64   `json:"group_id"`
	GroupName      string  `json:"group_name"`
	Platform       string  `json:"platform"`
	RateMultiplier float64 `json:"rate_multiplier"`
	UserCost       float64 `json:"user_cost"`
	UpstreamCost   float64 `json:"upstream_cost"`
}

type OperationsModelProfitStat struct {
	Model        string   `json:"model"`
	Platforms    []string `json:"platforms"`
	UserCost     float64  `json:"user_cost"`
	UpstreamCost float64  `json:"upstream_cost"`
}

type OperationsUserSpendStat struct {
	UserID       int64   `json:"user_id"`
	Email        string  `json:"email"`
	UserCost     float64 `json:"user_cost"`
	UpstreamCost float64 `json:"upstream_cost"`
}

type OperationsUserDetail struct {
	UserID int64                       `json:"user_id"`
	Email  string                      `json:"email"`
	Groups []OperationsGroupProfitStat `json:"groups"`
	Models []OperationsModelProfitStat `json:"models"`
}

type OperationsUsersData struct {
	Users  []OperationsUserSpendStat `json:"users"`
	Detail *OperationsUserDetail     `json:"detail"`
}

type OperationsUserSuggestion struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
}

type OperationsDashboardData struct {
	Metrics OperationsDashboardMetrics   `json:"metrics"`
	Trend   []OperationsProfitTrendPoint `json:"trend"`
	Groups  []OperationsGroupProfitStat  `json:"groups"`
	Models  []OperationsModelProfitStat  `json:"models"`
}

type operationsDashboardRepository interface {
	GetOperationsDashboardData(ctx context.Context, todayStart, todayEnd, rangeStart, rangeEnd, trendStart, trendEnd time.Time, timezone string, groupID int64) (*OperationsDashboardData, error)
}

func (s *DashboardService) GetOperationsDashboardData(ctx context.Context, todayStart, todayEnd, rangeStart, rangeEnd, trendStart, trendEnd time.Time, timezone string, groupID int64) (*OperationsDashboardData, error) {
	repo, ok := s.usageRepo.(operationsDashboardRepository)
	if !ok {
		return nil, ErrOperationsDashboardUnavailable
	}
	return repo.GetOperationsDashboardData(ctx, todayStart, todayEnd, rangeStart, rangeEnd, trendStart, trendEnd, timezone, groupID)
}

type operationsUsersRepository interface {
	GetOperationsUsersData(ctx context.Context, rangeStart, rangeEnd time.Time, search string, userID int64) (*OperationsUsersData, error)
}

func (s *DashboardService) GetOperationsUsersData(ctx context.Context, rangeStart, rangeEnd time.Time, search string, userID int64) (*OperationsUsersData, error) {
	repo, ok := s.usageRepo.(operationsUsersRepository)
	if !ok {
		return nil, ErrOperationsDashboardUnavailable
	}
	return repo.GetOperationsUsersData(ctx, rangeStart, rangeEnd, search, userID)
}

func (s *DashboardService) GetOperationsUserSuggestions(ctx context.Context, search string) ([]OperationsUserSuggestion, error) {
	repo, ok := s.usageRepo.(interface {
		GetOperationsUserSuggestions(context.Context, string) ([]OperationsUserSuggestion, error)
	})
	if !ok {
		return nil, ErrOperationsDashboardUnavailable
	}
	return repo.GetOperationsUserSuggestions(ctx, search)
}
