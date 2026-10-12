//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderRejectsUnknownTypeBeforeProviderOrDatabaseAccess(t *testing.T) {
	for _, orderType := range []string{"custom", "BALANCE", "balance ", " subscription", "refund", "null"} {
		t.Run(orderType, func(t *testing.T) {
			result, err := (&PaymentService{}).CreateOrder(context.Background(), CreateOrderRequest{
				UserID: 1, OrderType: orderType, Amount: 10, PaymentType: payment.TypeAlipay,
			})
			require.Nil(t, result)
			require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))
		})
	}
}

func TestPaymentFulfillmentRejectsUnknownOrderTypesWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	for _, orderType := range []string{"custom", "BALANCE", "balance ", ""} {
		t.Run(orderType, func(t *testing.T) {
			client := newPaymentConfigServiceTestClient(t)
			order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPaid, time.Now())
			order, err := client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(orderType).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client}

			for _, fulfill := range []func(context.Context, int64) error{
				svc.executeFulfillment, svc.ExecuteBalanceFulfillment, svc.ExecuteSubscriptionFulfillment,
			} {
				err := fulfill(ctx, order.ID)
				require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))
			}
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusPaid, reloaded.Status)
			require.Nil(t, reloaded.CompletedAt)
		})
	}
}

func TestPaymentFulfillmentCannotApplyTheOtherOrderTypesEntitlement(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentService{entClient: client}
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPaid, time.Now())

	err := svc.ExecuteBalanceFulfillment(ctx, order.ID)
	require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))
	_, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).Save(ctx)
	require.NoError(t, err)
	err = svc.ExecuteSubscriptionFulfillment(ctx, order.ID)
	require.Equal(t, "INVALID_ORDER_TYPE", infraerrors.Reason(err))
}
