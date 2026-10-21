//go:build unit

package handler

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEasyPayWebhookRejectsOrderSignatureReplayWithoutChangingBalance(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, variant := range []string{"order URL", "promoted status", "allowlisted fields only"} {
			t.Run(method+"/"+variant, func(t *testing.T) {
				h, client, order, ep, key := newEasyPayWebhookSecurityFixture(t)
				prefix := "https://site.example.com/payment/result?order_id=" + strconv.FormatInt(order.ID, 10)
				created, err := ep.CreatePayment(context.Background(), payment.CreatePaymentRequest{
					OrderID: order.OutTradeNo, Amount: "65.00", PaymentType: payment.TypeAlipay,
					Subject: "balance recharge", ReturnURL: prefix + "&trade_no=T1&trade_status=TRADE_SUCCESS",
				})
				require.NoError(t, err)
				payURL, err := url.Parse(created.PayURL)
				require.NoError(t, err)
				payload := payURL.Query()
				if variant != "order URL" {
					payload.Set("return_url", prefix)
					payload.Set("trade_no", "T1")
					payload.Set("trade_status", "TRADE_SUCCESS")
				}
				if variant == "allowlisted fields only" {
					// Absorbing forbidden keys into allowed values also preserves the
					// signature, so merchant identity must be checked independently.
					payload.Set("name", payload.Get("name")+"&notify_url="+payload.Get("notify_url"))
					payload.Del("notify_url")
					payload.Set("pid", payload.Get("pid")+"&return_url="+payload.Get("return_url"))
					payload.Del("return_url")
				}
				require.Equal(t, payload.Get("sign"), signEasyPayWebhookTestValues(payload, key),
					"the attack must reuse a real order signature, not just submit an invalid signature")
				response := sendEasyPayWebhookTestRequest(h, method, payload.Encode())
				require.Equal(t, http.StatusBadRequest, response.Code)
				require.Equal(t, "verify failed", response.Body.String())
				assertEasyPayWebhookTestUnchanged(t, client, order)
			})
		}
	}
}

func TestEasyPayWebhookRejectsInvalidNotificationsBeforeFulfillment(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(url.Values)
		code int
	}{
		{"duplicate order", func(v url.Values) { v.Add("out_trade_no", "another-order") }, http.StatusBadRequest},
		{"duplicate signature", func(v url.Values) { v.Add("sign", v.Get("sign")) }, http.StatusBadRequest},
		{"unknown empty field", func(v url.Values) { v.Set("return_url", "") }, http.StatusBadRequest},
		{"wrong merchant", func(v url.Values) { v.Set("pid", "another-merchant") }, http.StatusBadRequest},
		{"wrong amount", func(v url.Values) { v.Set("money", "64.00") }, http.StatusInternalServerError},
		{"nonfinite amount", func(v url.Values) { v.Set("money", "NaN") }, http.StatusInternalServerError},
		{"invalid amount", func(v url.Values) { v.Set("money", "not-money") }, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, client, order, _, key := newEasyPayWebhookSecurityFixture(t)
			payload := genuineEasyPayWebhookTestValues(order)
			tc.edit(payload)
			payload.Set("sign", signEasyPayWebhookTestValues(payload, key))
			if tc.name == "duplicate signature" {
				payload.Add("sign", payload.Get("sign"))
			}
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				response := sendEasyPayWebhookTestRequest(h, method, payload.Encode())
				require.Equal(t, tc.code, response.Code)
				assertEasyPayWebhookTestUnchanged(t, client, order)
			}
		})
	}
}

func TestEasyPayWebhookAcknowledgesRepeatedCompletedPaymentWithoutCreditingAgain(t *testing.T) {
	h, client, order, ep, key := newEasyPayWebhookSecurityFixture(t)
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(service.OrderStatusCompleted).SetPaymentTradeNo("T1").Save(context.Background())
	require.NoError(t, err)
	payload := genuineEasyPayWebhookTestValues(order)
	payload.Set("sign", signEasyPayWebhookTestValues(payload, key))
	_, err = ep.VerifyNotification(context.Background(), payload.Encode(), nil)
	require.NoError(t, err)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodGet} {
		response := sendEasyPayWebhookTestRequest(h, method, payload.Encode())
		require.Equal(t, http.StatusOK, response.Code)
		require.Equal(t, "success", response.Body.String())
		assertEasyPayWebhookTestUnchanged(t, client, order)
	}
}

func TestEasyPayWebhookRejectsOversizedPayloadWithoutTruncatingParameters(t *testing.T) {
	h, client, order, _, key := newEasyPayWebhookSecurityFixture(t)
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(service.OrderStatusCompleted).SetPaymentTradeNo("T1").Save(context.Background())
	require.NoError(t, err)
	payload := genuineEasyPayWebhookTestValues(order)
	payload.Set("sign", signEasyPayWebhookTestValues(payload, key))
	validPrefix := payload.Encode()
	// Empty query separators do not alter the signature. The forbidden field
	// starts after the old read limit and must never be silently discarded.
	oversized := validPrefix + strings.Repeat("&", maxWebhookBodySize-len(validPrefix)) + "return_url=forged"
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := sendEasyPayWebhookTestRequest(h, method, oversized)
		require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
		assertEasyPayWebhookTestUnchanged(t, client, order)
	}
}

func newEasyPayWebhookSecurityFixture(t *testing.T) (*PaymentWebhookHandler, *dbent.Client, *dbent.PaymentOrder, *provider.EasyPay, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:easypay_webhook_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	user, err := client.User.Create().SetEmail("payer@example.com").SetPasswordHash("hash").SetBalance(12).Save(ctx)
	require.NoError(t, err)
	config := map[string]string{
		"pid": "1000", "pkey": "test-merchant-key", "apiBase": "https://pay.example.com",
		"notifyUrl": "https://site.example.com/api/v1/payment/webhook/easypay",
		"returnUrl": "https://site.example.com/payment/result", "paymentMode": "popup",
	}
	encryptionKey := []byte("0123456789abcdef0123456789abcdef")
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	encrypted, err := payment.Encrypt(string(rawConfig), encryptionKey)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().SetName("EasyPay").SetProviderKey(payment.TypeEasyPay).
		SetSupportedTypes(payment.TypeAlipay).SetConfig(encrypted).SetEnabled(true).Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("payer").
		SetAmount(65).SetPayAmount(65).SetRechargeCode("test-recharge").SetOutTradeNo("sub2_easypay_security").
		SetPaymentType(payment.TypeAlipay).SetPaymentTradeNo("").SetOrderType(payment.OrderTypeBalance).
		SetProviderKey(payment.TypeEasyPay).SetProviderInstanceID(strconv.FormatInt(instance.ID, 10)).
		SetStatus(service.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").SetSrcHost("site.example.com").Save(ctx)
	require.NoError(t, err)
	ep, err := provider.NewEasyPay(strconv.FormatInt(instance.ID, 10), config)
	require.NoError(t, err)
	registry := payment.NewRegistry()
	svc := service.NewPaymentService(client, registry, payment.NewDefaultLoadBalancer(client, encryptionKey), nil, nil, nil, nil, nil, nil)
	providers, err := svc.GetWebhookProviders(ctx, payment.TypeEasyPay, order.OutTradeNo)
	require.NoError(t, err)
	require.Len(t, providers, 1)
	require.Equal(t, payment.TypeEasyPay, providers[0].ProviderKey())
	return NewPaymentWebhookHandler(svc, registry), client, order, ep, config["pkey"]
}

func genuineEasyPayWebhookTestValues(order *dbent.PaymentOrder) url.Values {
	return url.Values{
		"pid": {"1000"}, "trade_no": {"T1"}, "out_trade_no": {order.OutTradeNo},
		"type": {payment.TypeAlipay}, "name": {"balance recharge"}, "money": {"65.00"},
		"trade_status": {"TRADE_SUCCESS"}, "sign_type": {"MD5"},
	}
}

func signEasyPayWebhookTestValues(values url.Values, key string) string {
	keys := make([]string, 0, len(values))
	for name := range values {
		if name != "sign" && name != "sign_type" && values.Get(name) != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, name := range keys {
		pairs = append(pairs, name+"="+values.Get(name))
	}
	hash := md5.Sum([]byte(strings.Join(pairs, "&") + key))
	return hex.EncodeToString(hash[:])
}

func sendEasyPayWebhookTestRequest(h *PaymentWebhookHandler, method, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	path := "/api/v1/payment/webhook/easypay"
	if method == http.MethodGet {
		path += "?" + body
		body = ""
	}
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.EasyPayNotify(c)
	return recorder
}

func assertEasyPayWebhookTestUnchanged(t *testing.T, client *dbent.Client, order *dbent.PaymentOrder) {
	t.Helper()
	reloaded, err := client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Status, reloaded.Status)
	require.Equal(t, order.PaymentTradeNo, reloaded.PaymentTradeNo)
	require.Equal(t, order.PaidAt, reloaded.PaidAt)
	user, err := client.User.Get(context.Background(), order.UserID)
	require.NoError(t, err)
	require.Equal(t, 12.0, user.Balance)
}
