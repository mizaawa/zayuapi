package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/usagelog"
	"github.com/Wei-Shaw/sub2api/ent/user"
)

const (
	redeemFailureRiskAction = "recharge_risk_redeem_failures"
	rechargeRiskLookback    = 30 * 24 * time.Hour
	maxRechargeRiskEvents   = 25
)

// RechargeRiskEvent is a read-only observation, never a user restriction.
type RechargeRiskEvent struct {
	ID               string    `json:"id"`
	Type             string    `json:"type"`
	UserID           int64     `json:"user_id"`
	UserEmail        string    `json:"user_email"`
	Count            int       `json:"count"`
	OrderCount       int       `json:"order_count"`
	UserBalance      float64   `json:"user_balance"`
	TotalConsumption float64   `json:"total_consumption"`
	FirstAt          time.Time `json:"first_at"`
	LastAt           time.Time `json:"last_at"`
	Result           string    `json:"result"` // success or failed
}

// recordRedeemFailureRisk stores only threshold crossings, without the code
// entered by the user. At most 25 of these observations are retained.
func (s *RedeemService) recordRedeemFailureRisk(ctx context.Context, userID int64) {
	if s.entClient == nil {
		return
	}
	key := fmt.Sprintf("redeem-risk:%d", userID)
	now := time.Now()
	exists, err := s.entClient.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(key), paymentauditlog.ActionEQ(redeemFailureRiskAction),
		paymentauditlog.CreatedAtGTE(now.Add(-time.Hour)),
	).Exist(ctx)
	if err != nil || exists {
		return
	}
	if _, err := s.entClient.PaymentAuditLog.Create().SetOrderID(key).SetAction(redeemFailureRiskAction).
		SetDetail("20 or more invalid redemption attempts within one hour").SetCreatedAt(now).Save(ctx); err != nil {
		return
	}
	retained, err := s.entClient.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ(redeemFailureRiskAction)).
		Order(dbent.Desc(paymentauditlog.FieldCreatedAt), dbent.Desc(paymentauditlog.FieldID)).Limit(maxRechargeRiskEvents).All(ctx)
	if err != nil || len(retained) < maxRechargeRiskEvents {
		return
	}
	ids := make([]int64, len(retained))
	for i, entry := range retained {
		ids[i] = entry.ID
	}
	_, _ = s.entClient.PaymentAuditLog.Delete().Where(
		paymentauditlog.ActionEQ(redeemFailureRiskAction), paymentauditlog.IDNotIn(ids...),
	).Exec(ctx)
}

// AdminRechargeRisks detects recent recharge anomalies from existing orders
// and threshold observations. A user and risk type appear only once; the 25
// most recent observations are returned, with no enforcement side effects.
func (s *PaymentService) AdminRechargeRisks(ctx context.Context) ([]RechargeRiskEvent, error) {
	now := time.Now()
	orders, err := s.entClient.PaymentOrder.Query().Where(
		paymentorder.OrderTypeEQ("balance"), paymentorder.CreatedAtGTE(now.Add(-rechargeRiskLookback)),
	).Order(dbent.Desc(paymentorder.FieldCreatedAt), dbent.Desc(paymentorder.FieldID)).Limit(5000).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recharge orders for risk review: %w", err)
	}
	events := detectRechargeOrderRisks(orders)
	audits, err := s.entClient.PaymentAuditLog.Query().Where(
		paymentauditlog.ActionEQ(redeemFailureRiskAction), paymentauditlog.CreatedAtGTE(now.Add(-rechargeRiskLookback)),
	).Order(dbent.Desc(paymentauditlog.FieldCreatedAt), dbent.Desc(paymentauditlog.FieldID)).Limit(maxRechargeRiskEvents).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list redeem failure observations: %w", err)
	}
	for _, entry := range audits {
		id, parseErr := strconv.ParseInt(strings.TrimPrefix(entry.OrderID, "redeem-risk:"), 10, 64)
		if parseErr != nil || id <= 0 {
			continue
		}
		events = append(events, RechargeRiskEvent{
			ID: fmt.Sprintf("redeem_failures:%d", id), Type: "redeem_failures", UserID: id,
			Count: 20, FirstAt: entry.CreatedAt.Add(-time.Hour), LastAt: entry.CreatedAt, Result: "failed",
		})
	}
	byKey := make(map[string]RechargeRiskEvent, len(events))
	for _, event := range events {
		if previous, ok := byKey[event.ID]; !ok || event.LastAt.After(previous.LastAt) {
			byKey[event.ID] = event
		}
	}
	events = events[:0]
	for _, event := range byKey {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].LastAt.After(events[j].LastAt) })
	if len(events) > maxRechargeRiskEvents {
		events = events[:maxRechargeRiskEvents]
	}
	if len(events) == 0 {
		return []RechargeRiskEvent{}, nil
	}
	ids := make([]int64, 0, len(events))
	seen := make(map[int64]bool, len(events))
	for _, event := range events {
		if !seen[event.UserID] {
			ids = append(ids, event.UserID)
			seen[event.UserID] = true
		}
	}
	users, err := s.entClient.User.Query().Where(user.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recharge risk accounts: %w", err)
	}
	userByID := make(map[int64]*dbent.User, len(users))
	for _, account := range users {
		userByID[account.ID] = account
	}
	var spend []struct {
		UserID int64   `json:"user_id"`
		Sum    float64 `json:"sum"`
	}
	if err := s.entClient.UsageLog.Query().Where(usagelog.UserIDIn(ids...)).
		GroupBy(usagelog.FieldUserID).Aggregate(dbent.As(dbent.Sum(usagelog.FieldActualCost), "sum")).Scan(ctx, &spend); err != nil {
		return nil, fmt.Errorf("load recharge risk consumption: %w", err)
	}
	spendByID := make(map[int64]float64, len(spend))
	for _, row := range spend {
		spendByID[row.UserID] = row.Sum
	}
	for i := range events {
		if account := userByID[events[i].UserID]; account != nil {
			events[i].UserEmail = account.Email
			events[i].UserBalance = account.Balance
		}
		events[i].TotalConsumption = spendByID[events[i].UserID]
	}
	return events, nil
}

func detectRechargeOrderRisks(orders []*dbent.PaymentOrder) []RechargeRiskEvent {
	byUser := make(map[int64][]*dbent.PaymentOrder)
	for _, order := range orders {
		byUser[order.UserID] = append(byUser[order.UserID], order)
	}
	events := make([]RechargeRiskEvent, 0)
	for userID, userOrders := range byUser {
		sort.Slice(userOrders, func(i, j int) bool { return userOrders[i].CreatedAt.Before(userOrders[j].CreatedAt) })
		types := []struct {
			name   string
			window time.Duration
			min    int
			match  func(*dbent.PaymentOrder) bool
		}{
			{"rapid_orders", 30 * time.Minute, 10, func(*dbent.PaymentOrder) bool { return true }},
			{"unpaid_orders", 24 * time.Hour, 5, func(o *dbent.PaymentOrder) bool { return !rechargeOrderPaid(o.Status) }},
			{"repeated_amount", time.Hour, 5, func(*dbent.PaymentOrder) bool { return true }},
		}
		for _, kind := range types {
			window := make([]*dbent.PaymentOrder, 0)
			for _, order := range userOrders {
				if !kind.match(order) {
					continue
				}
				window = append(window, order)
				for len(window) > 0 && order.CreatedAt.Sub(window[0].CreatedAt) > kind.window {
					window = window[1:]
				}
				matching := window
				if kind.name == "repeated_amount" {
					matching = make([]*dbent.PaymentOrder, 0, len(window))
					for _, candidate := range window {
						if candidate.Amount == order.Amount {
							matching = append(matching, candidate)
						}
					}
				}
				if len(matching) < kind.min {
					continue
				}
				result := "failed"
				for _, candidate := range matching {
					if rechargeOrderPaid(candidate.Status) {
						result = "success"
						break
					}
				}
				id := fmt.Sprintf("%s:%d", kind.name, userID)
				event := RechargeRiskEvent{
					ID: id, Type: kind.name, UserID: userID, UserEmail: order.UserEmail,
					Count: len(matching), OrderCount: len(matching), FirstAt: matching[0].CreatedAt,
					LastAt: order.CreatedAt, Result: result,
				}
				found := false
				for i := range events {
					if events[i].ID == id {
						events[i] = event
						found = true
						break
					}
				}
				if !found {
					events = append(events, event)
				}
			}
		}
	}
	return events
}
