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

func TestPaymentOrdersOfAnotherUserAreIndistinguishableFromMissingOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
	other, err := client.User.Create().SetEmail("other-order-owner@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client}
	missingID := order.ID + 10000

	for _, operation := range []struct {
		name string
		run  func(bool) error
	}{
		{name: "get", run: func(missing bool) error {
			id := order.ID
			if missing {
				id = missingID
			}
			_, err := svc.GetOrder(ctx, id, other.ID)
			return err
		}},
		{name: "cancel", run: func(missing bool) error {
			id := order.ID
			if missing {
				id = missingID
			}
			_, err := svc.CancelOrder(ctx, id, other.ID)
			return err
		}},
		{name: "verify", run: func(missing bool) error {
			tradeNo := order.OutTradeNo
			if missing {
				tradeNo = "sub2_missing_order"
			}
			_, err := svc.VerifyOrderByOutTradeNo(ctx, tradeNo, other.ID)
			return err
		}},
		{name: "refund", run: func(missing bool) error {
			id := order.ID
			if missing {
				id = missingID
			}
			return svc.RequestRefund(ctx, id, other.ID, "unauthorized")
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			denied := operation.run(false)
			missing := operation.run(true)
			require.Error(t, denied)
			require.Equal(t, missing.Error(), denied.Error())
			require.Equal(t, "NOT_FOUND", infraerrors.Reason(denied))
		})
	}
	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, stored.Status)
	ownerOrder, err := svc.GetOrder(ctx, order.ID, order.UserID)
	require.NoError(t, err)
	require.Equal(t, order.ID, ownerOrder.ID)
}

func TestPaymentAccessProbeDoesNotDiscloseExclusivePolicyToUnapprovedUsers(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("hidden-policy@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "true", SettingPaymentExclusiveEnabled: "true",
	}}}}
	for _, id := range []int64{0, user.ID, user.ID + 10000} {
		access, err := svc.GetPaymentAccess(ctx, id)
		require.NoError(t, err)
		require.Equal(t, &PaymentAccess{}, access)
		require.ErrorIs(t, svc.RequirePaymentAccess(ctx, id), ErrPaymentAccessDenied)
	}
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	access, err := svc.GetPaymentAccess(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, &PaymentAccess{Enabled: true, Allowed: true, ExclusiveEnabled: true}, access)
	require.NoError(t, client.User.UpdateOneID(user.ID).SetStatus("disabled").Exec(ctx))
	access, err = svc.GetPaymentAccess(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, &PaymentAccess{}, access)
}

func TestPaymentCallbackStillFulfillsPaidOrderAfterAccessRevocation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
	require.NoError(t, client.User.UpdateOneID(order.UserID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	require.NoError(t, client.User.UpdateOneID(order.UserID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	subRepo := newSubscriptionUserSubRepoStub()
	groupRepo := &subscriptionGroupRepoStub{group: &Group{
		ID: *order.SubscriptionGroupID, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription,
	}}
	svc := &PaymentService{
		entClient: client, registry: payment.NewRegistry(), groupRepo: groupRepo,
		subscriptionSvc: NewSubscriptionService(groupRepo, subRepo, nil, nil, nil),
		configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
			SettingPaymentEnabled: "false", SettingPaymentExclusiveEnabled: "true",
		}}},
	}
	svc.registry.Register(&paymentOrderLifecycleQueryProvider{})
	require.ErrorIs(t, svc.RequirePaymentAccess(ctx, order.UserID), ErrPaymentAccessDenied)
	notification := &payment.PaymentNotification{
		TradeNo: "trade-after-revocation", OrderID: order.OutTradeNo, Amount: order.PayAmount,
		Status: payment.NotificationStatusSuccess, Metadata: map[string]string{"app_id": "test-alipay-app"},
	}
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification, payment.TypeAlipay))
	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, stored.Status)
	require.Len(t, subRepo.byID, 1)
	require.Equal(t, 1, subRepo.createCalls)
	sub, err := subRepo.GetByUserIDAndGroupID(ctx, order.UserID, *order.SubscriptionGroupID)
	require.NoError(t, err)
	expiresAt := sub.ExpiresAt
	require.NoError(t, svc.HandlePaymentNotification(ctx, notification, payment.TypeAlipay))
	require.Equal(t, 1, subRepo.createCalls)
	assertPaymentSubscriptionExpiry(t, subRepo, order, expiresAt)
}
