//go:build unit

package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type uncertainRefundProviderForTest struct {
	refundProviderTestDouble
	response *payment.RefundResponse
	err      error
	calls    int
	onRefund func()
	key      string
	request  payment.RefundRequest
}

func (p *uncertainRefundProviderForTest) ProviderKey() string {
	if p.key != "" {
		return p.key
	}
	return payment.TypeStripe
}

func (p *uncertainRefundProviderForTest) Refund(_ context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	p.calls++
	p.request = req
	if p.onRefund != nil {
		p.onRefund()
	}
	return p.response, p.err
}

func realRefundBalanceRepoForTest(client *dbent.Client) *mockUserRepo {
	return &mockUserRepo{
		adjustBalanceFn: func(ctx context.Context, id int64, delta float64) (BalanceChange, error) {
			_, err := client.User.UpdateOneID(id).AddBalance(delta).Save(ctx)
			return BalanceChange{}, err
		},
		updateBalanceFn: func(ctx context.Context, id int64, delta float64) error {
			c := client
			if tx := dbent.TxFromContext(ctx); tx != nil {
				c = tx.Client()
			}
			_, err := c.User.UpdateOneID(id).AddBalance(delta).Save(ctx)
			return err
		},
	}
}

func completedRefundOrderForTest(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) *dbent.PaymentOrder {
	t.Helper()
	order := createPendingRefundOrderForTest(t, ctx, client, suffix)
	_, err := client.PaymentAuditLog.Delete().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10))).Exec(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusCompleted).Save(ctx)
	require.NoError(t, err)
	return order
}

func TestExecuteRefundUncertainGatewayOutcomeHoldsBalance(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp *payment.RefundResponse
		err  error
	}{
		{name: "accepted_then_connection_lost", err: errors.New("connection reset after gateway accepted refund")},
		{name: "empty_response"},
		{name: "request_cancelled_after_acceptance", err: context.Canceled},
		{name: "unknown_response", resp: &payment.RefundResponse{RefundID: "rf_unknown", Status: "unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, tc.name)
			prov := &uncertainRefundProviderForTest{response: tc.resp, err: tc.err}
			if tc.name == "request_cancelled_after_acceptance" {
				prov.onRefund = cancel
			}
			defer replacePaymentProviderFactoryForTest(t, prov)()
			svc := &PaymentService{entClient: client, userRepo: realRefundBalanceRepoForTest(client), loadBalancer: &captureLoadBalancer{}}
			result, err := svc.ExecuteRefund(ctx, &RefundPlan{
				OrderID: order.ID, Order: order, RefundAmount: 40, GatewayAmount: 40,
				DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 40,
			})
			require.NoError(t, err)
			require.False(t, result.Success)
			checkCtx := context.Background()
			reloaded, err := client.PaymentOrder.Get(checkCtx, order.ID)
			require.NoError(t, err)
			user, err := client.User.Get(checkCtx, order.UserID)
			require.NoError(t, err)
			t.Logf("gateway_calls=%d status=%s balance=%.2f", prov.calls, reloaded.Status, user.Balance)
			require.Equal(t, OrderStatusRefundPending, reloaded.Status)
			require.Equal(t, 60.0, user.Balance)
			detail, err := svc.latestRefundPendingDetail(checkCtx, order.ID)
			require.NoError(t, err)
			require.True(t, detail.BalanceDeductionHeld)
			require.Equal(t, 40.0, detail.BalanceDeducted)
			_, _, err = svc.PrepareRefund(checkCtx, order.ID, 40, "retry", false, true)
			require.Error(t, err)
			require.Equal(t, 1, prov.calls)
		})
	}
}

func TestExecuteRefundExplicitGatewayFailureRestoresBalance(t *testing.T) {
	for _, providerErr := range []error{nil, errors.New("provider definitively rejected refund")} {
		t.Run(strconv.FormatBool(providerErr != nil), func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, "definite-failure")
			defer replacePaymentProviderFactoryForTest(t, &uncertainRefundProviderForTest{
				response: &payment.RefundResponse{Status: payment.ProviderStatusFailed}, err: providerErr,
			})()
			svc := &PaymentService{entClient: client, userRepo: realRefundBalanceRepoForTest(client), loadBalancer: &captureLoadBalancer{}}
			result, err := svc.ExecuteRefund(ctx, &RefundPlan{
				OrderID: order.ID, Order: order, RefundAmount: 40, GatewayAmount: 40,
				DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 40,
			})
			require.NoError(t, err)
			require.False(t, result.Success)
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			t.Logf("status=%s balance=%.2f", reloaded.Status, user.Balance)
			require.Equal(t, OrderStatusCompleted, reloaded.Status)
			require.Equal(t, 100.0, user.Balance)
		})
	}
}

func TestRefundRetryRespectsProductionAuditUniqueness(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	order := completedRefundOrderForTest(t, ctx, client, "production-audit-retry")
	defer replacePaymentProviderFactoryForTest(t, &uncertainRefundProviderForTest{
		response: &payment.RefundResponse{RefundID: "rf_pending", Status: payment.ProviderStatusPending},
	})()
	svc := &PaymentService{entClient: client, userRepo: realRefundBalanceRepoForTest(client), loadBalancer: &captureLoadBalancer{}}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := svc.ExecuteRefund(ctx, &RefundPlan{
			OrderID: order.ID, Order: order, RefundAmount: 40, GatewayAmount: 40,
			DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 40,
		})
		require.NoError(t, err, "pending attempt %d", attempt)
		require.False(t, result.Success)
		order, err = client.PaymentOrder.Get(ctx, order.ID)
		require.NoError(t, err)
		detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
		require.NoError(t, err)
		_, err = svc.finalizeRefundFailed(ctx, order, detail, errors.New("provider definitively rejected refund"))
		require.NoError(t, err, "failed attempt %d", attempt)
		order, err = client.PaymentOrder.Get(ctx, order.ID)
		require.NoError(t, err)
		user, err := client.User.Get(ctx, order.UserID)
		require.NoError(t, err)
		t.Logf("attempt=%d status=%s balance=%.2f", attempt, order.Status, user.Balance)
		require.Equal(t, OrderStatusRefundFailed, order.Status)
		require.Equal(t, 100.0, user.Balance)
	}
}

func TestRefundWithoutTradeNumberRequiresActualGatewayResult(t *testing.T) {
	for _, key := range []string{payment.TypeEasyPay, payment.TypeStripe} {
		t.Run(key, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, "no-trade-"+key)
			order, err := client.PaymentOrder.UpdateOneID(order.ID).SetPaymentTradeNo("").Save(ctx)
			require.NoError(t, err)
			prov := &uncertainRefundProviderForTest{key: key, response: &payment.RefundResponse{Status: payment.ProviderStatusSuccess}}
			defer replacePaymentProviderFactoryForTest(t, prov)()
			svc := &PaymentService{entClient: client, userRepo: realRefundBalanceRepoForTest(client), loadBalancer: &captureLoadBalancer{}}
			result, err := svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order,
				RefundAmount: 40, GatewayAmount: 40, DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: 40})
			require.NoError(t, err)
			if key == payment.TypeEasyPay {
				require.True(t, result.Success)
				require.Equal(t, 1, prov.calls, "EasyPay must actually refund using its merchant order number")
				require.Equal(t, order.OutTradeNo, prov.request.OrderID)
				require.Empty(t, prov.request.TradeNo)
			} else {
				require.False(t, result.Success, "Stripe cannot refund without its payment intent")
				require.Zero(t, prov.calls)
				user, err := client.User.Get(ctx, order.UserID)
				require.NoError(t, err)
				require.Equal(t, 100.0, user.Balance)
			}
			t.Logf("provider=%s gateway_calls=%d success=%t", key, prov.calls, result.Success)
		})
	}
}

func TestRefundUnresolvedRollbackBlocksChangedAmountRetry(t *testing.T) {
	for _, amount := range []float64{40, 100} {
		t.Run(strconv.FormatFloat(amount, 'f', 0, 64), func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, "rollback-failed")
			order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
			require.NoError(t, err)
			_, err = client.User.UpdateOneID(order.UserID).SetBalance(60).Save(ctx)
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).
				SetAction("REFUND_ROLLBACK_FAILED").SetOperator("admin").SetDetail(`{"balanceDeducted":40}`).Save(ctx)
			require.NoError(t, err)
			prov := &uncertainRefundProviderForTest{response: &payment.RefundResponse{Status: payment.ProviderStatusSuccess}}
			defer replacePaymentProviderFactoryForTest(t, prov)()
			svc := &PaymentService{entClient: client, userRepo: realRefundBalanceRepoForTest(client), loadBalancer: &captureLoadBalancer{}}
			_, _, err = svc.PrepareRefund(ctx, order.ID, amount, "retry", false, true)
			require.ErrorContains(t, err, "manual reconciliation")
			_, err = svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order,
				RefundAmount: amount, GatewayAmount: amount, DeductionType: payment.DeductionTypeBalance, BalanceToDeduct: amount})
			require.ErrorContains(t, err, "manual reconciliation", "stale prepared plans must also be blocked")
			require.Zero(t, prov.calls)
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Equal(t, 60.0, user.Balance)
			t.Logf("retry_amount=%.2f gateway_calls=%d balance=%.2f manual_review_required=true", amount, prov.calls, user.Balance)
		})
	}
}
