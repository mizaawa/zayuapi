//go:build unit

package provider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

func TestStripeNotificationRequiresValidSignature(t *testing.T) {
	t.Parallel()
	provider := &Stripe{config: map[string]string{"webhookSecret": "whsec_test"}}
	raw := fmt.Sprintf(`{"object":"event","api_version":%q,"type":"payment_intent.succeeded","data":{"object":{"id":"pi_123","amount":6500,"currency":"cny","metadata":{"orderId":"ORDER123"}}}}`, stripe.APIVersion)
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: []byte(raw), Secret: "whsec_test", Timestamp: time.Now(),
	})
	n, err := provider.VerifyNotification(context.Background(), raw, map[string]string{"stripe-signature": signed.Header})
	if err != nil {
		t.Fatalf("valid callback rejected: %v", err)
	}
	if n.Status != payment.ProviderStatusSuccess || n.OrderID != "ORDER123" || n.TradeNo != "pi_123" || n.Amount != 65 {
		t.Fatalf("unexpected notification: %+v", n)
	}

	if _, err := provider.VerifyNotification(context.Background(), raw, nil); err == nil {
		t.Fatal("unsigned callback accepted")
	}
	if _, err := provider.VerifyNotification(context.Background(), raw+" ", map[string]string{"stripe-signature": signed.Header}); err == nil {
		t.Fatal("tampered callback accepted")
	}
	stale := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: []byte(raw), Secret: "whsec_test", Timestamp: time.Now().Add(-webhook.DefaultTolerance - time.Minute),
	})
	if _, err := provider.VerifyNotification(context.Background(), raw, map[string]string{"stripe-signature": stale.Header}); err == nil {
		t.Fatal("stale callback accepted")
	}
}
