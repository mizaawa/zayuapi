//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOperationsRechargeRankingMatchesTodayRecharge(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	// The shared integration database keeps other tests' rows around; a day
	// nobody else writes to keeps the sums exact.
	loc := time.FixedZone("UTC+8", 8*3600)
	dayStart := time.Date(2001, 3, 4, 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	at := func(d time.Duration) *time.Time { value := dayStart.Add(d); return &value }

	whale := mustCreateUser(t, client, &service.User{Email: "ranking-whale-" + uniqueNano() + "@example.test"})
	minnow := mustCreateUser(t, client, &service.User{Email: "ranking-minnow-" + uniqueNano() + "@example.test"})
	admin := mustCreateUser(t, client, &service.User{Email: "ranking-admin-" + uniqueNano() + "@example.test", Role: service.RoleAdmin})

	order := func(user *service.User, orderType, status string, amount float64, paidAt *time.Time) string {
		code := "PAY-RANKING-" + uniqueNano()
		create := client.PaymentOrder.Create().
			SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("ranking").
			SetAmount(amount).SetPayAmount(amount).SetRechargeCode(code).
			SetPaymentType("alipay").SetPaymentTradeNo("").SetOrderType(orderType).SetStatus(status).
			SetExpiresAt(dayStart.Add(48 * time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("ranking.test")
		if paidAt != nil {
			create.SetPaidAt(*paidAt)
		}
		_, err := create.Save(ctx)
		require.NoError(t, err)
		return code
	}
	code := func(user *service.User, value, codeType string, amount float64, usedAt *time.Time) {
		usedBy := user.ID
		if value == "" {
			value = "RANKING-" + uniqueNano()
		}
		mustCreateRedeemCode(t, client, &service.RedeemCode{Code: value, Type: codeType, Value: amount, Status: service.StatusUsed, UsedBy: &usedBy, UsedAt: usedAt})
	}

	// A completed order is credited through its PAY- code: it counts once.
	paid := order(whale, "balance", service.OrderStatusCompleted, 100, at(10*time.Hour))
	code(whale, paid, service.RedeemTypeBalance, 100, at(10*time.Hour+time.Minute))
	code(whale, "", service.RedeemTypeBalance, 20, at(11*time.Hour))
	code(whale, "", service.AdjustmentTypeAdminBalance, -5, at(12*time.Hour))
	code(minnow, "", service.AdjustmentTypeAdminBalance, 30, at(time.Hour))
	order(minnow, "balance", service.OrderStatusPaid, 15, at(24*time.Hour-time.Minute))
	// Yesterday 23:59, a refunded order, an unpaid order, a subscription, an
	// administrator's top-up and tomorrow's code stay out.
	order(minnow, "balance", service.OrderStatusCompleted, 999, at(-time.Minute))
	order(minnow, "balance", service.OrderStatusRefunded, 50, at(2*time.Hour))
	order(minnow, "balance", service.OrderStatusPending, 40, nil)
	order(minnow, "subscription", service.OrderStatusCompleted, 70, at(3*time.Hour))
	code(admin, "", service.RedeemTypeBalance, 500, at(4*time.Hour))
	code(minnow, "", service.RedeemTypeBalance, 60, at(24*time.Hour))

	data, err := repo.GetOperationsRechargeRanking(ctx, dayStart, dayEnd, 50)
	require.NoError(t, err)
	require.Len(t, data.Users, 2)
	require.Equal(t, whale.ID, data.Users[0].UserID)
	require.Equal(t, whale.Email, data.Users[0].Email)
	require.InDelta(t, 120, data.Users[0].Amount, 1e-9)
	require.Equal(t, int64(2), data.Users[0].RechargeCount)
	require.Equal(t, minnow.ID, data.Users[1].UserID)
	require.InDelta(t, 45, data.Users[1].Amount, 1e-9)
	require.Equal(t, int64(2), data.Users[1].RechargeCount)
	require.InDelta(t, 165, data.TotalAmount, 1e-9)
	require.Equal(t, int64(2), data.TotalUsers)

	top, err := repo.GetOperationsRechargeRanking(ctx, dayStart, dayEnd, 1)
	require.NoError(t, err)
	require.Len(t, top.Users, 1)
	require.Equal(t, whale.ID, top.Users[0].UserID)
	require.InDelta(t, 165, top.TotalAmount, 1e-9)
	require.Equal(t, int64(2), top.TotalUsers)

	// The today recharge card adds up to the same amount.
	dashboard, err := repo.GetOperationsDashboardData(ctx, dayStart, dayEnd, dayStart, dayEnd, dayStart, dayEnd, "Asia/Shanghai", 0)
	require.NoError(t, err)
	require.InDelta(t, 165, dashboard.Metrics.TodayUserRecharge, 1e-9)
}
