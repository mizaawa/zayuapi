//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestExecuteRefundRejectsConcurrentSpendBeforeGateway(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "refund-concurrent-spend")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusRefundRequested).SetPaymentTradeNo("").Save(ctx)
	require.NoError(t, err)
	var adjusted float64
	svc := &PaymentService{
		entClient: client,
		userRepo: &mockUserRepo{
			adjustBalanceFn: func(_ context.Context, id int64, delta float64) (BalanceChange, error) {
				require.Equal(t, order.UserID, id)
				adjusted = delta
				return BalanceChange{}, ErrBalanceNegative
			},
			deductAvailableBalanceFn: func(context.Context, int64, float64) (float64, error) {
				t.Fatal("non-forced refund must require the complete balance deduction")
				return 0, nil
			},
		},
	}
	plan := &RefundPlan{
		OrderID: order.ID, Order: order, RefundAmount: 100, GatewayAmount: 100,
		DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 100,
	}
	result, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.True(t, result.RequireForce)
	require.Equal(t, -100.0, adjusted)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundRequested, reloaded.Status)
	count, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)),
			paymentauditlog.ActionIn("REFUND_SUCCESS", "REFUND_NO_TRADE_NO")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestQueryRefundHeldBalanceFinalizesWithoutSecondDeduction(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      string
		wantSuccess bool
		wantBalance float64
		wantStatus  string
	}{
		{name: "success", status: payment.ProviderStatusSuccess, wantSuccess: true, wantBalance: 40, wantStatus: OrderStatusPartiallyRefunded},
		{name: "pending", status: payment.ProviderStatusPending, wantStatus: OrderStatusRefundPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "held-refund-"+tc.name)
			_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefunding).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{}, loadBalancer: &captureLoadBalancer{}}
			plan := &RefundPlan{
				OrderID: order.ID, Order: order, RefundAmount: 40, GatewayAmount: 40,
				DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 40,
			}
			_, err = svc.markRefundPending(ctx, plan, &payment.RefundResponse{RefundID: "held-refund", Status: payment.ProviderStatusPending})
			require.NoError(t, err)
			svc.userRepo = &mockUserRepo{
				deductAvailableBalanceFn: func(context.Context, int64, float64) (float64, error) {
					t.Fatal("held refund credit must not be deducted again")
					return 0, nil
				},
				deductBalanceFn: func(context.Context, int64, float64) error {
					t.Fatal("held refund credit must not be deducted again")
					return nil
				},
				updateBalanceFn: func(context.Context, int64, float64) error {
					t.Fatal("held refund credit must remain unavailable until a definitive failure")
					return nil
				},
			}
			restore := replacePaymentProviderFactoryForTest(t, &refundQueryProviderTestDouble{
				refundResponse: &payment.RefundResponse{Status: tc.status},
			})
			defer restore()
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantSuccess, result.Success)
			require.Equal(t, tc.wantBalance, result.BalanceDeducted)
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, reloaded.Status)
		})
	}
}

func TestFinalizeRefundFailedRestoresHeldBalanceOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "held-refund-failed")
	restored := 0.0
	svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{
		updateBalanceFn: func(ctx context.Context, id int64, amount float64) error {
			require.NotNil(t, dbent.TxFromContext(ctx))
			require.Equal(t, order.UserID, id)
			restored += amount
			return nil
		},
	}}
	detail := refundPendingAuditDetail{BalanceDeductionHeld: true, BalanceDeducted: 40}
	result, err := svc.finalizeRefundFailed(ctx, order, detail, errors.New("provider declined refund"))
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, 40.0, restored)
	result, err = svc.finalizeRefundFailed(ctx, order, detail, errors.New("duplicate provider result"))
	require.Nil(t, result)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))
	require.Equal(t, 40.0, restored)
}

func TestFinalizeRefundFailedRollsBackBalanceRestorationFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "held-refund-restore-failure")
	svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{
		updateBalanceFn: func(ctx context.Context, id int64, amount float64) error {
			tx := dbent.TxFromContext(ctx)
			require.NotNil(t, tx)
			_, err := tx.User.UpdateOneID(id).AddBalance(amount).Save(ctx)
			require.NoError(t, err)
			return errors.New("injected balance restoration failure")
		},
	}}
	result, err := svc.finalizeRefundFailed(ctx, order, refundPendingAuditDetail{
		BalanceDeductionHeld: true, BalanceDeducted: 40,
	}, errors.New("provider declined refund"))
	require.Nil(t, result)
	require.ErrorContains(t, err, "injected balance restoration failure")
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, reloaded.Status)
}

func TestQueryRefundWithoutDeductionHonorsOriginalPlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "refund-no-deduction")
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefunding).Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{}, loadBalancer: &captureLoadBalancer{}}
	_, err = svc.markRefundPending(ctx, &RefundPlan{
		OrderID: order.ID, Order: order, RefundAmount: 100, GatewayAmount: 100,
		DeductionType: payment.DeductionTypeNone,
	}, &payment.RefundResponse{Status: payment.ProviderStatusPending})
	require.NoError(t, err)
	svc.userRepo = &mockUserRepo{deductBalanceFn: func(context.Context, int64, float64) error {
		t.Fatal("refund confirmation must honor the administrator's no-deduction choice")
		return nil
	}}
	restore := replacePaymentProviderFactoryForTest(t, &refundQueryProviderTestDouble{
		refundResponse: &payment.RefundResponse{Status: payment.ProviderStatusSuccess},
	})
	defer restore()
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Zero(t, result.BalanceDeducted)
}

func TestPrepareRefundRejectsPendingRetry(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "refund-pending-retry")
	plan, result, err := (&PaymentService{entClient: client}).PrepareRefund(ctx, order.ID, 50, "duplicate refund", false, true)
	require.Nil(t, plan)
	require.Nil(t, result)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
}

func TestQueryRefundUnknownOutcomeKeepsHeldBalancePending(t *testing.T) {
	for index, resp := range []*payment.RefundResponse{nil, {Status: "unrecognized"}} {
		ctx := context.Background()
		client := newPaymentConfigServiceTestClient(t)
		order := createPendingRefundOrderForTest(t, ctx, client, "refund-unknown-outcome-"+strconv.Itoa(index))
		_, err := client.PaymentAuditLog.Update().
			Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
			SetDetail(`{"refundID":"rf_test","balanceDeductionHeld":true,"balanceDeducted":40}`).Save(ctx)
		require.NoError(t, err)
		svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{updateBalanceFn: func(context.Context, int64, float64) error {
			t.Fatal("an unknown gateway outcome must not release held refund credit")
			return nil
		}}, loadBalancer: &captureLoadBalancer{}}
		restore := replacePaymentProviderFactoryForTest(t, &refundQueryProviderTestDouble{refundResponse: resp})
		result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
		restore()
		require.Nil(t, result)
		require.Error(t, err)
		reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
		require.NoError(t, err)
		require.Equal(t, OrderStatusRefundPending, reloaded.Status)
	}
}

func TestRefundFinalizationRejectsResultsFromPreviousAttempt(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(strconv.FormatBool(success), func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			original := createPendingRefundOrderForTest(t, ctx, client, "stale-refund-"+strconv.FormatBool(success))
			_, err := client.PaymentOrder.UpdateOneID(original.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
			require.NoError(t, err)
			current, err := client.PaymentOrder.UpdateOneID(original.ID).
				SetStatus(OrderStatusRefundPending).SetRefundAmount(40).
				SetUpdatedAt(original.UpdatedAt.Add(time.Second)).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client, userRepo: &mockUserRepo{
				updateBalanceFn: func(context.Context, int64, float64) error {
					t.Fatal("a stale refund result must not restore the new attempt's held balance")
					return nil
				},
				deductBalanceFn: func(context.Context, int64, float64) error {
					t.Fatal("a stale refund result must not deduct credit")
					return nil
				},
			}}
			var result *RefundResult
			if success {
				result, err = svc.finalizePendingRefundSuccess(ctx, svc.refundFinalizePlan(original))
			} else {
				result, err = svc.finalizeRefundFailed(ctx, original, refundPendingAuditDetail{
					BalanceDeductionHeld: true, BalanceDeducted: 100,
				}, errors.New("delayed failure of previous attempt"))
			}
			require.Nil(t, result)
			require.Equal(t, "CONFLICT", infraerrors.Reason(err))
			reloaded, err := client.PaymentOrder.Get(ctx, current.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, reloaded.Status)
			require.Equal(t, 40.0, reloaded.RefundAmount)
			require.True(t, current.UpdatedAt.Equal(reloaded.UpdatedAt))
		})
	}
}
