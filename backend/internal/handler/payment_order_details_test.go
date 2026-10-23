//go:build unit

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPaymentOrderDetailsExposeOnlyOwnerBoundSafeLinks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:payment_order_details_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	owner, err := client.User.Create().SetEmail("payment-owner@example.com").SetPasswordHash("hash").SetUsername("owner").Save(ctx)
	require.NoError(t, err)
	other, err := client.User.Create().SetEmail("payment-other@example.com").SetPasswordHash("hash").SetUsername("other").Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(owner.ID).SetUserEmail(owner.Email).SetUserName(owner.Username).
		SetAmount(10).SetPayAmount(10).SetFeeRate(0).
		SetRechargeCode("ORDER-DETAILS").SetOutTradeNo("bound-order-42").
		SetPaymentType(payment.TypeWxpay).SetPaymentTradeNo("wx-order-42").SetOrderType(payment.OrderTypeBalance).
		SetStatus(service.OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").SetSrcHost("app.example.com").
		SetPayURL("https://pay.example.com/order/42").SetQrCode("weixin://wxpay/bizpayurl?pr=42").
		SetProviderSnapshot(map[string]any{"config": map[string]any{"pkey": "PRIVATE-TEST-KEY"}}).
		Save(ctx)
	require.NoError(t, err)
	config := service.NewPaymentConfigService(client, repository.NewSettingRepository(client), nil)
	payments := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, config, nil, nil, nil)
	h := NewPaymentHandler(payments, config)
	var subjectID int64
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if subjectID != 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: subjectID})
		}
	})
	router.GET("/payment/orders/my", h.GetMyOrders)
	router.GET("/payment/orders/:id", h.GetOrder)
	router.POST("/payment/public/orders/verify", h.VerifyOrderPublic)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		return recorder
	}
	decodeData := func(recorder *httptest.ResponseRecorder) map[string]any {
		var result struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		return result.Data
	}
	path := fmt.Sprintf("/payment/orders/%d", order.ID)

	subjectID = owner.ID
	owned := request(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, owned.Code, owned.Body.String())
	data := decodeData(owned)
	require.Equal(t, "https://pay.example.com/order/42", data["pay_url"])
	require.Equal(t, "weixin://wxpay/bizpayurl?pr=42", data["qr_code"])
	require.NotContains(t, owned.Body.String(), "PRIVATE-TEST-KEY")
	require.NotContains(t, data, "provider_snapshot")
	t.Logf("owner lookup: HTTP %d pay_url=%q qr_code=%q", owned.Code, data["pay_url"], data["qr_code"])
	listed := request(http.MethodGet, "/payment/orders/my", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.NotContains(t, listed.Body.String(), `"pay_url"`)
	require.NotContains(t, listed.Body.String(), `"qr_code"`)

	subjectID = other.ID
	foreign := request(http.MethodGet, path, "")
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
	foreignMissing := request(http.MethodGet, "/payment/orders/999999", "")
	require.Equal(t, foreignMissing.Body.String(), foreign.Body.String())
	require.NotContains(t, foreign.Body.String(), "pay.example.com")
	require.NotContains(t, foreign.Body.String(), "weixin:")
	t.Logf("other user lookup: HTTP %d", foreign.Code)

	subjectID = 0
	anonymous := request(http.MethodGet, path, "")
	require.Equal(t, http.StatusUnauthorized, anonymous.Code, anonymous.Body.String())
	public := request(http.MethodPost, "/payment/public/orders/verify", `{"out_trade_no":"bound-order-42"}`)
	require.Equal(t, http.StatusOK, public.Code, public.Body.String())
	require.NotContains(t, decodeData(public), "pay_url")
	require.NotContains(t, decodeData(public), "qr_code")
	t.Logf("anonymous lookup: HTTP %d; public verify: HTTP %d without payment contents", anonymous.Code, public.Code)

	subjectID = owner.ID
	missing := request(http.MethodGet, "/payment/orders/999999", "")
	require.Equal(t, http.StatusNotFound, missing.Code, missing.Body.String())
	for _, unsafe := range []string{"javascript:alert(1)", "java\tscript:alert(1)", "data:text/html,pay", "file:///tmp/pay", "intent://pay"} {
		require.NoError(t, client.PaymentOrder.UpdateOneID(order.ID).SetPayURL(unsafe).SetQrCode(unsafe).Exec(ctx))
		legacy := request(http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, legacy.Code, legacy.Body.String())
		require.NotContains(t, decodeData(legacy), "pay_url")
		require.NotContains(t, decodeData(legacy), "qr_code")
	}
	for _, status := range []string{service.OrderStatusCompleted, service.OrderStatusExpired, service.OrderStatusCancelled, service.OrderStatusFailed} {
		require.NoError(t, client.PaymentOrder.UpdateOneID(order.ID).SetStatus(status).SetPayURL("https://pay.example.com/order/42").SetQrCode("wxp://42").Exec(ctx))
		terminal := request(http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, terminal.Code, terminal.Body.String())
		require.NotContains(t, decodeData(terminal), "pay_url")
		require.NotContains(t, decodeData(terminal), "qr_code")
	}
	t.Log("unsafe legacy URLs and terminal orders: payment contents omitted")
}
