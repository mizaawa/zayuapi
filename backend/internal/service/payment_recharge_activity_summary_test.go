//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestAdminRechargeActivitySummaryCountsLikeTodayRecharge(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentService{entClient: client}

	dayStart := time.Date(2026, 11, 10, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	dayEnd := dayStart.AddDate(0, 0, 1)
	at := func(d time.Duration) time.Time { return dayStart.Add(d) }

	newUser := func(email, role string) int64 {
		created, err := client.User.Create().SetEmail(email).SetPasswordHash("hash").SetUsername(email).SetRole(role).Save(ctx)
		require.NoError(t, err)
		return created.ID
	}
	alice := newUser("alice@example.com", RoleUser)
	bob := newUser("bob@example.com", RoleUser)
	admin := newUser("admin@example.com", RoleAdmin)

	seq := 0
	order := func(userID int64, email, orderType, status string, amount float64, paidAt *time.Time) string {
		seq++
		code := "PAY-SUMMARY-" + strconv.Itoa(seq)
		create := client.PaymentOrder.Create().
			SetUserID(userID).SetUserEmail(email).SetUserName(email).
			SetAmount(amount).SetPayAmount(amount).SetRechargeCode(code).
			SetOutTradeNo("summary-" + strconv.Itoa(seq)).SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").
			SetOrderType(orderType).SetStatus(status).SetExpiresAt(at(48 * time.Hour)).
			SetClientIP("127.0.0.1").SetSrcHost("summary.test")
		if paidAt != nil {
			create.SetPaidAt(*paidAt)
		}
		_, err := create.Save(ctx)
		require.NoError(t, err)
		return code
	}
	code := func(userID int64, value, codeType string, amount float64, usedAt time.Time) {
		if value == "" {
			seq++
			value = "SUMMARY-" + strconv.Itoa(seq)
		}
		_, err := client.RedeemCode.Create().SetCode(value).SetType(codeType).SetValue(amount).
			SetStatus(StatusUsed).SetUsedBy(userID).SetUsedAt(usedAt).Save(ctx)
		require.NoError(t, err)
	}
	ptr := func(value time.Time) *time.Time { return &value }

	// Counted: a completed order (its PAY- code is not counted again), an
	// order paid but not yet credited, a redeem code and a manual top-up.
	paid := order(alice, "alice@example.com", payment.OrderTypeBalance, OrderStatusCompleted, 100, ptr(at(10*time.Hour)))
	code(alice, paid, RedeemTypeBalance, 100, at(10*time.Hour+time.Minute))
	order(bob, "bob@example.com", payment.OrderTypeBalance, OrderStatusPaid, 15, ptr(at(23*time.Hour)))
	code(alice, "", RedeemTypeBalance, 20, at(11*time.Hour))
	code(bob, "", AdjustmentTypeAdminBalance, 30, at(time.Hour))
	// Not counted: yesterday, refunded, unpaid, subscription, a deduction, an
	// administrator's own top-up and tomorrow.
	order(alice, "alice@example.com", payment.OrderTypeBalance, OrderStatusCompleted, 999, ptr(at(-time.Minute)))
	order(alice, "alice@example.com", payment.OrderTypeBalance, OrderStatusRefunded, 50, ptr(at(2*time.Hour)))
	order(bob, "bob@example.com", payment.OrderTypeBalance, OrderStatusPending, 40, nil)
	order(bob, "bob@example.com", payment.OrderTypeSubscription, OrderStatusCompleted, 70, ptr(at(3*time.Hour)))
	code(alice, "", AdjustmentTypeAdminBalance, -5, at(12*time.Hour))
	code(admin, "", RedeemTypeBalance, 500, at(4*time.Hour))
	code(bob, "", RedeemTypeBalance, 60, at(24*time.Hour))

	summarize := func(filter RechargeActivityFilter) RechargeActivitySummary {
		filter.StartTime, filter.EndTime = dayStart, dayEnd
		summary, err := svc.AdminRechargeActivitySummary(ctx, filter)
		require.NoError(t, err)
		return summary
	}

	all := summarize(RechargeActivityFilter{})
	require.InDelta(t, 165, all.Amount, 1e-9)
	require.Equal(t, 4, all.Count)
	direct := summarize(RechargeActivityFilter{Channel: "direct"})
	require.InDelta(t, 115, direct.Amount, 1e-9)
	require.Equal(t, 2, direct.Count)
	redeem := summarize(RechargeActivityFilter{Channel: "redeem"})
	require.InDelta(t, 20, redeem.Amount, 1e-9)
	require.Equal(t, 1, redeem.Count)
	manual := summarize(RechargeActivityFilter{Channel: "manual"})
	require.InDelta(t, 30, manual.Amount, 1e-9)
	require.Equal(t, 1, manual.Count)

	// The email search narrows the totals; the status filter does not apply.
	aliceOnly := summarize(RechargeActivityFilter{Email: " ALICE@ ", Status: "unpaid"})
	require.InDelta(t, 120, aliceOnly.Amount, 1e-9)
	require.Equal(t, 2, aliceOnly.Count)

	empty := summarize(RechargeActivityFilter{Email: "nobody"})
	require.Zero(t, empty.Amount)
	require.Zero(t, empty.Count)
}
