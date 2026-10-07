package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func easyPayPoCProvider() *EasyPay {
	return &EasyPay{config: map[string]string{
		"pid":       "1000",
		"pkey":      "MERCHANT_SECRET_KEY",
		"apiBase":   "https://pay.example.com",
		"notifyUrl": "https://site.example.com/api/v1/payment/webhook/easypay",
		"returnUrl": "https://site.example.com/payment/result",
	}}
}

func TestEasyPayNotifyRejectsForgedSignReuseCallback(t *testing.T) {
	t.Parallel()
	e := easyPayPoCProvider()

	// Before #7881, promoting this embedded pair preserved the order signature.
	prefix := "https://site.example.com/payment/result?order_id=99&out_trade_no=ORDER123&status=success"
	createParams := map[string]string{
		"pid":          "1000",
		"type":         "alipay",
		"out_trade_no": "ORDER123",
		"notify_url":   e.config["notifyUrl"],
		"return_url":   prefix + "&trade_status=TRADE_SUCCESS",
		"name":         "balance recharge",
		"money":        "650.00",
	}
	sign := easyPaySign(createParams, e.config["pkey"])
	forgedParams := cloneStringMap(createParams)
	forgedParams["return_url"] = prefix
	forgedParams["trade_status"] = tradeStatusSuccess
	if !easyPayVerifySign(forgedParams, e.config["pkey"], sign) {
		t.Fatal("regression payload must reuse the order-creation signature")
	}
	q := url.Values{}
	for k, v := range forgedParams {
		q.Set(k, v)
	}
	q.Set("sign", sign)
	q.Set("sign_type", signTypeMD5)
	if _, err := e.VerifyNotification(context.Background(), q.Encode(), nil); err == nil {
		t.Fatal("forged sign-reuse callback must be rejected")
	}
}

func TestEasyPayNotifyRejectsOrderURLReplay(t *testing.T) {
	t.Parallel()
	e := easyPayPoCProvider()
	response, err := e.createRedirectPayment(payment.CreatePaymentRequest{
		OrderID:     "ORDER123",
		PaymentType: "alipay",
		Subject:     "balance recharge",
		Amount:      "650.00",
	})
	if err != nil {
		t.Fatalf("createRedirectPayment returned error: %v", err)
	}
	payURL, err := url.Parse(response.PayURL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}
	if _, err := e.VerifyNotification(context.Background(), payURL.RawQuery, nil); err == nil {
		t.Fatal("replayed order URL must be rejected")
	}
}

func TestEasyPayNotifyAcceptsGenuineCallback(t *testing.T) {
	t.Parallel()
	e := easyPayPoCProvider()
	params := map[string]string{
		"pid":          "1000",
		"trade_no":     "2026100622001400000001",
		"out_trade_no": "ORDER123",
		"type":         "alipay",
		"name":         "balance recharge",
		"money":        "650.00",
		"trade_status": tradeStatusSuccess,
		"param":        "merchant-reference",
	}
	params["sign"] = easyPaySign(params, e.config["pkey"])
	params["sign_type"] = signTypeMD5
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	n, err := e.VerifyNotification(context.Background(), q.Encode(), nil)
	if err != nil {
		t.Fatalf("genuine callback rejected: %v", err)
	}
	if n.Status != payment.ProviderStatusSuccess || n.OrderID != "ORDER123" || n.TradeNo != "2026100622001400000001" || n.Amount != 650.00 || n.Metadata["pid"] != "1000" {
		t.Fatalf("unexpected notification: %+v", n)
	}
}

func TestEasyPayNotifyRejectsUnknownParam(t *testing.T) {
	t.Parallel()
	e := easyPayPoCProvider()
	for _, key := range []string{"notify_url", "return_url", "cid", "device", "clientip", "unknown"} {
		for _, value := range []string{"", "injected"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				t.Parallel()
				params := map[string]string{
					"pid":          "1000",
					"trade_no":     "T1",
					"out_trade_no": "ORDER123",
					"type":         "alipay",
					"name":         "balance recharge",
					"money":        "650.00",
					"trade_status": tradeStatusSuccess,
					key:            value,
				}
				params["sign"] = easyPaySign(params, e.config["pkey"])
				params["sign_type"] = signTypeMD5
				q := url.Values{}
				for k, v := range params {
					q.Set(k, v)
				}
				_, err := e.VerifyNotification(context.Background(), q.Encode(), nil)
				if err == nil || !strings.Contains(err.Error(), "unexpected notify param: "+key) {
					t.Fatalf("callback with unknown param must be rejected, got %v", err)
				}
			})
		}
	}
}
