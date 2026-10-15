//go:build unit

package provider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestAlipayNotificationRequiresValidSignatureAndAppID(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewAlipay("test", map[string]string{
		"appId":      "merchant-app",
		"privateKey": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})),
		"publicKey":  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKey})),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := provider.getClient()
	if err != nil {
		t.Fatal(err)
	}

	signedValues := func(appID string) url.Values {
		values := url.Values{
			"app_id":       {appID},
			"trade_no":     {"2026101500001"},
			"out_trade_no": {"ORDER123"},
			"trade_status": {"TRADE_SUCCESS"},
			"total_amount": {"65.00"},
		}
		signature, err := client.SignValues(values)
		if err != nil {
			t.Fatal(err)
		}
		values.Set("sign", base64.StdEncoding.EncodeToString(signature))
		values.Set("sign_type", "RSA2")
		return values
	}

	valid := signedValues("merchant-app")
	notification, err := provider.VerifyNotification(context.Background(), valid.Encode(), nil)
	if err != nil {
		t.Fatalf("valid callback rejected: %v", err)
	}
	if notification.Status != payment.ProviderStatusSuccess || notification.OrderID != "ORDER123" || notification.Amount != 65 {
		t.Fatalf("unexpected notification: %+v", notification)
	}

	unsigned := signedValues("merchant-app")
	unsigned.Del("sign")
	if _, err := provider.VerifyNotification(context.Background(), unsigned.Encode(), nil); err == nil {
		t.Fatal("unsigned callback accepted")
	}

	tampered := signedValues("merchant-app")
	tampered.Set("total_amount", "650.00")
	if _, err := provider.VerifyNotification(context.Background(), tampered.Encode(), nil); err == nil {
		t.Fatal("tampered callback accepted")
	}

	foreignApp := signedValues("foreign-app")
	if _, err := provider.VerifyNotification(context.Background(), foreignApp.Encode(), nil); err == nil {
		t.Fatal("callback for another app accepted")
	}
}
