//go:build unit

package provider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

type stripeReceivedAmountBackend struct {
	stripeRefundBackend
	intent stripe.PaymentIntent
}

func (b *stripeReceivedAmountBackend) Call(_ string, _ string, _ string, _ stripe.ParamsContainer, result stripe.LastResponseSetter) error {
	*result.(*stripe.PaymentIntent) = b.intent
	return nil
}

func TestStripeUsesReceivedAmountForSuccessfulNotificationsAndQueries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		currency string
		received int64
		want     float64
	}{
		{name: "full payment", currency: "CNY", received: 6500, want: 65},
		{name: "partial capture", currency: "CNY", received: 1200, want: 12},
		{name: "no received funds", currency: "CNY", received: 0, want: 0},
		{name: "zero decimal currency", currency: "JPY", received: 1200, want: 1200},
		{name: "three decimal currency", currency: "KWD", received: 1234, want: 1.234},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &stripeReceivedAmountBackend{intent: stripe.PaymentIntent{
				ID: "pi_123", Amount: 6500, AmountReceived: tc.received,
				Status: stripe.PaymentIntentStatusSucceeded, Currency: stripe.Currency(tc.currency),
			}}
			provider := &Stripe{config: map[string]string{"webhookSecret": "whsec_test"}, initialized: true,
				sc: stripe.NewClient("sk_test", stripe.WithBackends(&stripe.Backends{API: backend}))}
			result, err := provider.QueryOrder(context.Background(), "pi_123")
			require.NoError(t, err)
			require.Equal(t, payment.ProviderStatusPaid, result.Status)
			require.Equal(t, tc.want, result.Amount)

			raw := fmt.Sprintf(`{"object":"event","api_version":%q,"type":"payment_intent.succeeded","data":{"object":{"id":"pi_123","amount":6500,"amount_received":%d,"currency":%q,"metadata":{"orderId":"ORDER123"}}}}`, stripe.APIVersion, tc.received, tc.currency)
			signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
				Payload: []byte(raw), Secret: "whsec_test", Timestamp: time.Now(),
			})
			notification, err := provider.VerifyNotification(context.Background(), raw, map[string]string{"stripe-signature": signed.Header})
			require.NoError(t, err)
			require.Equal(t, payment.NotificationStatusSuccess, notification.Status)
			require.Equal(t, tc.want, notification.Amount)
		})
	}
}
