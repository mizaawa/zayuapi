package service

import (
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestDetectRechargeOrderRisksGroupsRepeatedBehaviorByUserAndType(t *testing.T) {
	base := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	orders := make([]*dbent.PaymentOrder, 0, 12)
	for i := 0; i < 12; i++ {
		orders = append(orders, &dbent.PaymentOrder{
			ID: int64(i + 1), UserID: 7, UserEmail: "risk@example.com", Amount: 25,
			Status: OrderStatusPending, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	events := detectRechargeOrderRisks(orders)
	byType := make(map[string]RechargeRiskEvent, len(events))
	for _, event := range events {
		byType[event.Type] = event
		require.Equal(t, int64(7), event.UserID)
		require.Equal(t, "failed", event.Result)
	}
	require.Len(t, byType, 3)
	require.Equal(t, 12, byType["rapid_orders"].OrderCount)
	require.Equal(t, 12, byType["unpaid_orders"].OrderCount)
	require.Equal(t, 12, byType["repeated_amount"].OrderCount)
	require.Equal(t, base.Add(11*time.Minute), byType["rapid_orders"].LastAt)
}

func TestDetectRechargeOrderRisksDoesNotCountDistinctAmountsAsDuplicates(t *testing.T) {
	base := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	orders := make([]*dbent.PaymentOrder, 0, 10)
	for i := 0; i < 10; i++ {
		orders = append(orders, &dbent.PaymentOrder{
			ID: int64(i + 1), UserID: 8, UserEmail: "varied@example.com", Amount: float64(i + 1),
			Status: OrderStatusCompleted, CreatedAt: base.Add(time.Duration(i) * time.Minute),
			OutTradeNo: fmt.Sprintf("order-%d", i),
		})
	}
	events := detectRechargeOrderRisks(orders)
	require.Len(t, events, 1)
	require.Equal(t, "rapid_orders", events[0].Type)
	require.Equal(t, "success", events[0].Result)
}

func TestRechargeOrderPaidTreatsOnlySettledStatesAsPaid(t *testing.T) {
	require.False(t, rechargeOrderPaid(OrderStatusPending))
	require.False(t, rechargeOrderPaid(OrderStatusExpired))
	require.False(t, rechargeOrderPaid("UNKNOWN"))
	require.True(t, rechargeOrderPaid(OrderStatusPaid))
	require.True(t, rechargeOrderPaid(OrderStatusCompleted))
}
