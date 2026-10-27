package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/ent/user"
)

const rechargeActivityPageSize = 25

// RechargeActivityFilter applies to the dashboard's read-only recharge timeline.
type RechargeActivityFilter struct {
	Page      int
	Email     string
	Status    string // paid, unpaid, or empty
	Channel   string // direct, redeem, manual, or empty
	StartTime time.Time
	EndTime   time.Time // exclusive
}

type RechargeActivityItem struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminRechargeActivity merges direct payment orders with used balance and
// administrator adjustment codes. Each source is bounded to the requested
// page prefix, so page two is not queried until the client requests page two.
func (s *PaymentService) AdminRechargeActivity(ctx context.Context, filter RechargeActivityFilter) ([]RechargeActivityItem, int, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := page * rechargeActivityPageSize
	items := make([]RechargeActivityItem, 0, limit)
	total := 0

	if filter.Channel == "" || filter.Channel == "direct" {
		q := s.entClient.PaymentOrder.Query().Where(paymentorder.OrderTypeEQ("balance"))
		if filter.Email != "" {
			q = q.Where(paymentorder.UserEmailContainsFold(strings.TrimSpace(filter.Email)))
		}
		if !filter.StartTime.IsZero() {
			q = q.Where(paymentorder.CreatedAtGTE(filter.StartTime))
		}
		if !filter.EndTime.IsZero() {
			q = q.Where(paymentorder.CreatedAtLT(filter.EndTime))
		}
		switch filter.Status {
		case "paid":
			q = q.Where(paymentorder.StatusIn(OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted,
				OrderStatusRefundRequested, OrderStatusRefunding, OrderStatusRefundPending,
				OrderStatusPartiallyRefunded, OrderStatusRefunded, OrderStatusRefundFailed))
		case "unpaid":
			q = q.Where(paymentorder.StatusIn(OrderStatusPending, OrderStatusExpired, OrderStatusCancelled, OrderStatusFailed))
		}
		count, err := q.Clone().Count(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("count direct recharge orders: %w", err)
		}
		total += count
		if count > 0 {
			orders, err := q.Order(dbent.Desc(paymentorder.FieldCreatedAt), dbent.Desc(paymentorder.FieldID)).Limit(limit).All(ctx)
			if err != nil {
				return nil, 0, fmt.Errorf("list direct recharge orders: %w", err)
			}
			for _, order := range orders {
				status := "unpaid"
				if rechargeOrderPaid(order.Status) {
					status = "paid"
				}
				items = append(items, RechargeActivityItem{
					ID: fmt.Sprintf("payment:%d", order.ID), UserID: order.UserID, UserEmail: order.UserEmail,
					Amount: order.Amount, Currency: "USD", Status: status, Channel: "direct", CreatedAt: order.CreatedAt,
				})
			}
		}
	}

	if filter.Status != "unpaid" && filter.Channel != "direct" {
		q := s.entClient.RedeemCode.Query().Where(
			redeemcode.StatusEQ(StatusUsed), redeemcode.ValueGT(0), redeemcode.UsedByNotNil(),
			redeemcode.UsedAtNotNil(), redeemcode.TypeIn(RedeemTypeBalance, AdjustmentTypeAdminBalance),
			redeemcode.Not(redeemcode.CodeHasPrefix("PAY-")),
		)
		switch filter.Channel {
		case "redeem":
			q = q.Where(redeemcode.TypeEQ(RedeemTypeBalance))
		case "manual":
			q = q.Where(redeemcode.TypeEQ(AdjustmentTypeAdminBalance))
		}
		if filter.Email != "" {
			q = q.Where(redeemcode.HasUserWith(user.EmailContainsFold(strings.TrimSpace(filter.Email))))
		}
		if !filter.StartTime.IsZero() {
			q = q.Where(redeemcode.UsedAtGTE(filter.StartTime))
		}
		if !filter.EndTime.IsZero() {
			q = q.Where(redeemcode.UsedAtLT(filter.EndTime))
		}
		count, err := q.Clone().Count(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("count code recharge activity: %w", err)
		}
		total += count
		if count > 0 {
			codes, err := q.Order(dbent.Desc(redeemcode.FieldUsedAt), dbent.Desc(redeemcode.FieldID)).Limit(limit).WithUser().All(ctx)
			if err != nil {
				return nil, 0, fmt.Errorf("list code recharge activity: %w", err)
			}
			for _, code := range codes {
				channel := "redeem"
				if code.Type == AdjustmentTypeAdminBalance {
					channel = "manual"
				}
				email := ""
				if code.Edges.User != nil {
					email = code.Edges.User.Email
				}
				items = append(items, RechargeActivityItem{
					ID: fmt.Sprintf("%s:%d", channel, code.ID), UserID: *code.UsedBy, UserEmail: email,
					Amount: code.Value, Currency: "USD", Status: "paid", Channel: channel, CreatedAt: *code.UsedAt,
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	start := (page - 1) * rechargeActivityPageSize
	if start >= len(items) {
		return []RechargeActivityItem{}, total, nil
	}
	end := start + rechargeActivityPageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func rechargeOrderPaid(status string) bool {
	switch status {
	case OrderStatusPaid, OrderStatusRecharging, OrderStatusCompleted,
		OrderStatusRefundRequested, OrderStatusRefunding, OrderStatusRefundPending,
		OrderStatusPartiallyRefunded, OrderStatusRefunded, OrderStatusRefundFailed:
		return true
	default:
		return false
	}
}
