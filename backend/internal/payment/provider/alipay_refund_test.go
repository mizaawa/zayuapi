//go:build unit

package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/require"
)

func TestAlipayRefundUsesStableRequestID(t *testing.T) {
	original := alipayTradeRefund
	t.Cleanup(func() { alipayTradeRefund = original })
	var ids []string
	alipayTradeRefund = func(_ context.Context, _ *alipay.Client, req alipay.TradeRefund) (*alipay.TradeRefundRsp, error) {
		ids = append(ids, req.OutRequestNo)
		return &alipay.TradeRefundRsp{Error: alipay.Error{Code: alipay.CodeSuccess}, FundChange: "N"}, nil
	}
	prov := &Alipay{client: &alipay.Client{}}
	for _, amount := range []string{"40.00", "40", "41.00"} {
		resp, err := prov.Refund(context.Background(), payment.RefundRequest{OrderID: strings.Repeat("o", 64), Amount: amount})
		require.NoError(t, err)
		require.Equal(t, payment.ProviderStatusSuccess, resp.Status)
		require.Equal(t, ids[len(ids)-1], resp.RefundID)
	}
	require.Equal(t, ids[0], ids[1], "retry of the same partial refund must not move money twice")
	require.NotEqual(t, ids[0], ids[2])
	require.LessOrEqual(t, len(ids[0]), 64)
}

func TestAlipayRefundSeparatesDefiniteRejectionFromUncertainty(t *testing.T) {
	original := alipayTradeRefund
	t.Cleanup(func() { alipayTradeRefund = original })
	for _, tc := range []struct {
		name string
		resp *alipay.TradeRefundRsp
		err  error
		want string
	}{
		{name: "transport", err: errors.New("connection reset"), want: payment.ProviderStatusPending},
		{name: "empty_response", want: payment.ProviderStatusPending},
		{name: "system_error", err: &alipay.Error{Code: alipay.CodeUnknowError}, want: payment.ProviderStatusPending},
		{name: "business_system_error", resp: &alipay.TradeRefundRsp{Error: alipay.Error{Code: alipay.CodeBusinessFailed, SubCode: "ACQ.SYSTEM_ERROR"}}, want: payment.ProviderStatusPending},
		{name: "invalid_param", err: &alipay.Error{Code: alipay.CodeInvalidParam}, want: payment.ProviderStatusFailed},
		{name: "order_not_found", resp: &alipay.TradeRefundRsp{Error: alipay.Error{Code: alipay.CodeBusinessFailed, SubCode: alipayErrTradeNotExist}}, want: payment.ProviderStatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alipayTradeRefund = func(context.Context, *alipay.Client, alipay.TradeRefund) (*alipay.TradeRefundRsp, error) {
				return tc.resp, tc.err
			}
			resp, err := (&Alipay{client: &alipay.Client{}}).Refund(context.Background(), payment.RefundRequest{OrderID: "order", Amount: "40.00"})
			require.Error(t, err)
			require.NotEmpty(t, resp.RefundID)
			require.Equal(t, tc.want, resp.Status)
		})
	}
}

func TestAlipayQueryRefundRequiresMatchingSuccessfulRefund(t *testing.T) {
	original := alipayTradeRefundQuery
	t.Cleanup(func() { alipayTradeRefundQuery = original })
	requestID, err := alipayRefundRequestID("order", "40.00")
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		status  string
		amount  string
		orderID string
		want    string
		wantErr bool
	}{
		{name: "success", status: "REFUND_SUCCESS", amount: "40.00", orderID: "order", want: payment.ProviderStatusSuccess},
		{name: "not_received_or_failed", orderID: "order", want: payment.ProviderStatusPending},
		{name: "amount_mismatch", status: "REFUND_SUCCESS", amount: "80.00", orderID: "order", wantErr: true},
		{name: "order_mismatch", status: "REFUND_SUCCESS", amount: "40.00", orderID: "other", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alipayTradeRefundQuery = func(_ context.Context, _ *alipay.Client, req alipay.TradeFastPayRefundQuery) (*alipay.TradeFastPayRefundQueryRsp, error) {
				require.Equal(t, "order", req.OutTradeNo)
				require.Equal(t, requestID, req.OutRequestNo)
				return &alipay.TradeFastPayRefundQueryRsp{Error: alipay.Error{Code: alipay.CodeSuccess}, OutTradeNo: tc.orderID,
					OutRequestNo: requestID, RefundAmount: tc.amount, RefundStatus: tc.status}, nil
			}
			resp, err := (&Alipay{client: &alipay.Client{}}).QueryRefund(context.Background(), payment.RefundQueryRequest{OrderID: "order", Amount: "40.00"})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.Status)
		})
	}
}
