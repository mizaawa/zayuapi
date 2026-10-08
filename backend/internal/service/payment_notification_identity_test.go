//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentNotificationValidatesLegacyMerchantIdentity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		appID      string
		merchantID string
		noMetadata bool
		ambiguous  bool
		snapshot   bool
		incomplete bool
		wantError  string
	}{
		{name: "matching legacy merchant", appID: "wx-app", merchantID: "mch"},
		{name: "incomplete legacy snapshot", appID: "wx-app", merchantID: "mch", incomplete: true},
		{name: "configured MP app", appID: "wx-mp-app", merchantID: "mch"},
		{name: "foreign app", appID: "wx-foreign", merchantID: "mch", wantError: "appid mismatch"},
		{name: "foreign merchant", appID: "wx-app", merchantID: "mch-foreign", wantError: "mchid mismatch"},
		{name: "missing metadata", noMetadata: true, wantError: "missing provider metadata"},
		{name: "ambiguous legacy merchant", appID: "wx-app", merchantID: "mch", ambiguous: true, wantError: "identity is ambiguous"},
		{name: "original snapshot takes precedence", appID: "wx-original", merchantID: "mch-original", snapshot: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "notification-identity")
			instanceID, err := strconv.ParseInt(*order.ProviderInstanceID, 10, 64)
			require.NoError(t, err)
			config := encryptWebhookProviderConfig(t, map[string]string{
				"appId": "wx-app", "mpAppId": "wx-mp-app", "mchId": "mch",
			})
			_, err = client.PaymentProviderInstance.UpdateOneID(instanceID).
				SetProviderKey(payment.TypeWxpay).SetSupportedTypes(payment.TypeWxpay).SetConfig(config).Save(ctx)
			require.NoError(t, err)
			update := client.PaymentOrder.UpdateOneID(order.ID).
				SetPaymentType(payment.TypeWxpay).SetStatus(OrderStatusCompleted)
			if tc.snapshot {
				update.SetProviderSnapshot(map[string]any{
					"schema_version": 2, "provider_instance_id": *order.ProviderInstanceID,
					"provider_key": payment.TypeWxpay, "merchant_app_id": "wx-original",
					"merchant_id": "mch-original", "currency": "CNY",
				})
			}
			if tc.incomplete {
				update.SetProviderSnapshot(map[string]any{
					"schema_version": 1, "provider_instance_id": *order.ProviderInstanceID,
					"provider_key": payment.TypeWxpay,
				})
			}
			if tc.ambiguous {
				update.ClearProviderInstanceID()
				_, err = client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeWxpay).
					SetName("second merchant").SetConfig(config).SetSupportedTypes(payment.TypeWxpay).
					SetEnabled(false).Save(ctx)
				require.NoError(t, err)
			}
			order, err = update.Save(ctx)
			require.NoError(t, err)
			metadata := map[string]string{"appid": tc.appID, "mchid": tc.merchantID, "currency": "CNY", "trade_state": "SUCCESS"}
			if tc.noMetadata {
				metadata = nil
			}
			svc := &PaymentService{entClient: client, loadBalancer: newWebhookProviderTestLoadBalancer(client), registry: payment.NewRegistry()}
			err = svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{
				OrderID: order.OutTradeNo, TradeNo: order.PaymentTradeNo, Amount: order.PayAmount,
				Status: payment.NotificationStatusSuccess, Metadata: metadata,
			}, payment.TypeWxpay)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCompleted, reloaded.Status)
		})
	}
}

func TestPaymentNotificationRejectsMissingSnapshotMetadata(t *testing.T) {
	order := &dbent.PaymentOrder{ProviderSnapshot: map[string]any{"schema_version": 2, "currency": "CNY"}}
	require.ErrorContains(t, validateProviderNotificationMetadata(order, payment.TypeStripe, nil), "missing provider metadata")
}

func TestPaymentNotificationAirwallexOptionalAccountIdentity(t *testing.T) {
	for _, tc := range []struct {
		name              string
		configuredAccount string
		snapshotAccount   string
		actualAccount     string
		currency          string
		snapshot          bool
		wantError         string
	}{
		{name: "legacy without optional account", currency: "CNY"},
		{name: "snapshot without optional account", currency: "CNY", snapshot: true},
		{name: "legacy configured account matches", configuredAccount: "acct-1", actualAccount: "acct-1", currency: "CNY"},
		{name: "legacy foreign account rejected", configuredAccount: "acct-1", actualAccount: "foreign", currency: "CNY", wantError: "account_id mismatch"},
		{name: "legacy missing configured account rejected", configuredAccount: "acct-1", currency: "CNY", wantError: "account_id missing"},
		{name: "incomplete snapshot retains configured identity check", configuredAccount: "acct-1", actualAccount: "foreign", currency: "CNY", snapshot: true, wantError: "account_id mismatch"},
		{name: "snapshot identity takes precedence", configuredAccount: "acct-new", snapshotAccount: "acct-original", actualAccount: "acct-original", currency: "CNY", snapshot: true},
		{name: "currency remains required", wantError: "missing currency"},
		{name: "currency mismatch rejected without account", currency: "USD", wantError: "currency mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderForTest(t, ctx, client, "airwallex-notification")
			instanceID, err := strconv.ParseInt(*order.ProviderInstanceID, 10, 64)
			require.NoError(t, err)
			config := encryptWebhookProviderConfig(t, map[string]string{"accountId": tc.configuredAccount, "currency": "CNY"})
			_, err = client.PaymentProviderInstance.UpdateOneID(instanceID).
				SetProviderKey(payment.TypeAirwallex).SetSupportedTypes(payment.TypeAirwallex).SetConfig(config).Save(ctx)
			require.NoError(t, err)
			update := client.PaymentOrder.UpdateOneID(order.ID).SetPaymentType(payment.TypeAirwallex).SetStatus(OrderStatusCompleted)
			if tc.snapshot {
				update.SetProviderSnapshot(map[string]any{
					"schema_version": 2, "provider_instance_id": *order.ProviderInstanceID,
					"provider_key": payment.TypeAirwallex, "currency": "CNY", "merchant_id": tc.snapshotAccount,
				})
			}
			order, err = update.Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client, loadBalancer: newWebhookProviderTestLoadBalancer(client), registry: payment.NewRegistry()}
			err = svc.HandlePaymentNotification(ctx, &payment.PaymentNotification{
				OrderID: order.OutTradeNo, TradeNo: order.PaymentTradeNo, Amount: order.PayAmount,
				Status:   payment.NotificationStatusSuccess,
				Metadata: map[string]string{"account_id": tc.actualAccount, "currency": tc.currency, "status": "SUCCEEDED"},
			}, payment.TypeAirwallex)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
