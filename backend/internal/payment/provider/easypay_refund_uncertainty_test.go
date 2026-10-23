package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestEasyPayRefundOnlyParsedRejectionIsDefinitive(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantFailed bool
	}{
		{name: "definite_failure", body: `{"code":0,"msg":"insufficient funds"}`, wantFailed: true},
		{name: "string_failure", body: `{"code":"-1","msg":"rejected"}`, wantFailed: true},
		{name: "missing_code", body: `{"msg":"unknown"}`},
		{name: "null_code", body: `{"code":null}`},
		{name: "fractional_success", body: `{"code":1.9}`},
		{name: "fractional_failure", body: `{"code":0.5}`},
		{name: "html", body: `<html>error</html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			resp, err := newTestEasyPay(t, server.URL).Refund(context.Background(), payment.RefundRequest{OrderID: "order", Amount: "40.00"})
			require.Error(t, err)
			if tc.wantFailed {
				require.NotNil(t, resp)
				require.Equal(t, payment.ProviderStatusFailed, resp.Status)
			} else {
				require.Nil(t, resp)
			}
		})
	}
}

func TestEasyPayRefundDoesNotRetryAmbiguousOrderNotFoundResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{name: "html", status: http.StatusOK, body: `<html>order not found</html>`},
		{name: "bad_gateway", status: http.StatusBadGateway, body: `order not found`},
		{name: "missing_code", status: http.StatusOK, body: `{"msg":"order not found"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls > 1 {
					_, _ = w.Write([]byte(`{"code":1,"msg":"ok"}`))
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newTestEasyPay(t, server.URL).Refund(context.Background(), payment.RefundRequest{OrderID: "order", TradeNo: "trade", Amount: "40.00"})
			require.Error(t, err)
			require.Equal(t, 1, calls, "an ambiguous reply must not issue a second refund under another identifier")
		})
	}
}
