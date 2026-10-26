package service

import (
	"context"
	"errors"
	"time"
)

var ErrOperationsDashboardUnavailable = errors.New("operations dashboard is unavailable")

type OperationsDashboardMetrics struct {
	TodayUserConsumption    float64            `json:"today_user_consumption"`
	TodayUserRecharge       float64            `json:"today_user_recharge"`
	TodayMonitorConsumption float64            `json:"today_monitor_consumption"`
	TodayRevenue            map[string]float64 `json:"today_revenue"`
	TotalUserConsumption    float64            `json:"total_user_consumption"`
	TotalUserBalance        float64            `json:"total_user_balance"`
	TotalMonitorConsumption float64            `json:"total_monitor_consumption"`
	AdminBalance            float64            `json:"admin_balance"`
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
	RateMultiplier float64 `json:"rate_multiplier"`
	UserCost       float64 `json:"user_cost"`
	UpstreamCost   float64 `json:"upstream_cost"`
}

type OperationsModelProfitStat struct {
	Model        string  `json:"model"`
	UserCost     float64 `json:"user_cost"`
	UpstreamCost float64 `json:"upstream_cost"`
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
