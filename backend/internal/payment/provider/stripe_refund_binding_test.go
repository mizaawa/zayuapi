//go:build unit

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v85"
)

type stripeRefundBindingBackend struct {
	createParams []*stripe.RefundCreateParams
	retrieved    *stripe.Refund
	pages        [][]*stripe.Refund
	listQueries  []url.Values
	err          error
}

func (b *stripeRefundBindingBackend) Call(_ string, _ string, _ string, params stripe.ParamsContainer, result stripe.LastResponseSetter) error {
	if b.err != nil {
		return b.err
	}
	if create, ok := params.(*stripe.RefundCreateParams); ok {
		b.createParams = append(b.createParams, create)
		*result.(*stripe.Refund) = *boundStripeRefund()
		return nil
	}
	if b.retrieved == nil {
		return errors.New("unexpected retrieve")
	}
	*result.(*stripe.Refund) = *b.retrieved
	return nil
}

func (b *stripeRefundBindingBackend) CallRaw(_ string, _ string, _ string, body []byte, _ *stripe.Params, result stripe.LastResponseSetter) error {
	if b.err != nil {
		return b.err
	}
	query, err := url.ParseQuery(string(body))
	if err != nil {
		return err
	}
	page := len(b.listQueries)
	b.listQueries = append(b.listQueries, query)
	var refunds []*stripe.Refund
	if page < len(b.pages) {
		refunds = b.pages[page]
	}
	encoded, err := json.Marshal(map[string]any{"object": "list", "data": refunds, "has_more": page+1 < len(b.pages)})
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

func (*stripeRefundBindingBackend) CallStreaming(string, string, string, stripe.ParamsContainer, stripe.StreamingLastResponseSetter) error {
	return errors.New("unexpected streaming request")
}

func (*stripeRefundBindingBackend) CallMultipart(string, string, string, string, *bytes.Buffer, *stripe.Params, stripe.LastResponseSetter) error {
	return errors.New("unexpected multipart request")
}

func (*stripeRefundBindingBackend) SetMaxNetworkRetries(int64) {}

func boundStripeRefund() *stripe.Refund {
	return &stripe.Refund{
		ID: "re_current", PaymentIntent: &stripe.PaymentIntent{ID: "pi_123"},
		Amount: 4000, Currency: stripe.CurrencyCNY, Status: stripe.RefundStatusSucceeded,
		Metadata: map[string]string{"orderId": "order_456", "refundRequestId": "re-order_456-4000"},
	}
}

func newStripeRefundBindingProvider(backend *stripeRefundBindingBackend) *Stripe {
	return &Stripe{
		config: map[string]string{"currency": "CNY"}, initialized: true,
		sc: stripe.NewClient("sk_test", stripe.WithBackends(&stripe.Backends{API: backend})),
	}
}

func TestStripeRefundMetadataBindsStableRequest(t *testing.T) {
	backend := &stripeRefundBindingBackend{}
	provider := newStripeRefundBindingProvider(backend)
	for _, amount := range []string{"40.00", "40", "41.00"} {
		_, err := provider.Refund(context.Background(), payment.RefundRequest{TradeNo: "pi_123", OrderID: "order_456", Amount: amount})
		require.NoError(t, err)
	}
	require.Len(t, backend.createParams, 3)
	for _, params := range backend.createParams {
		require.Equal(t, "order_456", params.Metadata["orderId"])
		require.Equal(t, *params.IdempotencyKey, params.Metadata["refundRequestId"])
	}
	require.Equal(t, backend.createParams[0].Metadata, backend.createParams[1].Metadata)
	require.NotEqual(t, backend.createParams[0].Metadata["refundRequestId"], backend.createParams[2].Metadata["refundRequestId"])
}

func TestStripeQueryRefundCannotResolveUnrelatedRefund(t *testing.T) {
	for _, tc := range []struct {
		name     string
		knownID  bool
		mutate   func(*stripe.Refund)
		wantErr  bool
		wantCode string
	}{
		{name: "bound_success", wantCode: payment.ProviderStatusSuccess},
		{name: "bound_failure_without_id", mutate: func(r *stripe.Refund) { r.Status = stripe.RefundStatusFailed }, wantErr: true},
		{name: "bound_canceled_without_id", mutate: func(r *stripe.Refund) { r.Status = stripe.RefundStatusCanceled }, wantErr: true},
		{name: "bound_pending", mutate: func(r *stripe.Refund) { r.Status = stripe.RefundStatusPending }, wantCode: payment.ProviderStatusPending},
		{name: "old_failed_without_metadata", mutate: func(r *stripe.Refund) { r.Metadata = nil; r.Status = stripe.RefundStatusFailed }, wantErr: true},
		{name: "old_canceled_without_metadata", mutate: func(r *stripe.Refund) { r.Metadata = nil; r.Status = stripe.RefundStatusCanceled }, wantErr: true},
		{name: "merchant_refund_without_metadata", mutate: func(r *stripe.Refund) { r.Metadata = nil }, wantErr: true},
		{name: "other_order", mutate: func(r *stripe.Refund) { r.Metadata["orderId"] = "other" }, wantErr: true},
		{name: "other_attempt", mutate: func(r *stripe.Refund) { r.Metadata["refundRequestId"] = "re-order_456-4100" }, wantErr: true},
		{name: "wrong_intent", mutate: func(r *stripe.Refund) { r.PaymentIntent.ID = "pi_other" }, wantErr: true},
		{name: "missing_intent", mutate: func(r *stripe.Refund) { r.PaymentIntent = nil }, wantErr: true},
		{name: "wrong_amount", mutate: func(r *stripe.Refund) { r.Amount = 4100 }, wantErr: true},
		{name: "wrong_currency", mutate: func(r *stripe.Refund) { r.Currency = stripe.CurrencyUSD }, wantErr: true},
		{name: "missing_currency", mutate: func(r *stripe.Refund) { r.Currency = "" }, wantErr: true},
		{name: "missing_id", mutate: func(r *stripe.Refund) { r.ID = "" }, wantErr: true},
		{name: "known_legacy_id", knownID: true, mutate: func(r *stripe.Refund) { r.Metadata = nil }, wantCode: payment.ProviderStatusSuccess},
		{name: "known_failure", knownID: true, mutate: func(r *stripe.Refund) { r.Status = stripe.RefundStatusFailed }, wantCode: payment.ProviderStatusFailed},
		{name: "known_canceled", knownID: true, mutate: func(r *stripe.Refund) { r.Status = stripe.RefundStatusCanceled }, wantCode: payment.ProviderStatusFailed},
		{name: "known_wrong_id", knownID: true, mutate: func(r *stripe.Refund) { r.ID = "re_other" }, wantErr: true},
		{name: "known_wrong_intent", knownID: true, mutate: func(r *stripe.Refund) { r.PaymentIntent.ID = "pi_other" }, wantErr: true},
		{name: "known_missing_intent", knownID: true, mutate: func(r *stripe.Refund) { r.PaymentIntent = nil }, wantErr: true},
		{name: "known_wrong_amount", knownID: true, mutate: func(r *stripe.Refund) { r.Amount = 4100 }, wantErr: true},
		{name: "known_wrong_currency", knownID: true, mutate: func(r *stripe.Refund) { r.Currency = stripe.CurrencyUSD }, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refund := boundStripeRefund()
			if tc.mutate != nil {
				tc.mutate(refund)
			}
			backend := &stripeRefundBindingBackend{retrieved: refund, pages: [][]*stripe.Refund{{refund}}}
			request := payment.RefundQueryRequest{TradeNo: "pi_123", OrderID: "order_456", Amount: "40.00"}
			if tc.knownID {
				request.RefundID = "re_current"
			}
			response, err := newStripeRefundBindingProvider(backend).QueryRefund(context.Background(), request)
			if tc.wantErr {
				require.Error(t, err, "uncertain attribution must leave the current refund pending")
				require.Nil(t, response, "must not return a final outcome for an unrelated refund")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "re_current", response.RefundID)
			require.Equal(t, tc.wantCode, response.Status)
		})
	}
}

func TestStripeQueryRefundFindsCurrentRequestAcrossPages(t *testing.T) {
	old := boundStripeRefund()
	old.ID, old.Status, old.Metadata = "re_unrelated", stripe.RefundStatusFailed, nil
	backend := &stripeRefundBindingBackend{pages: [][]*stripe.Refund{{old}, {boundStripeRefund()}}}
	response, err := newStripeRefundBindingProvider(backend).QueryRefund(context.Background(), payment.RefundQueryRequest{TradeNo: "pi_123", OrderID: "order_456", Amount: "40.00"})
	require.NoError(t, err)
	require.Equal(t, "re_current", response.RefundID)
	require.Equal(t, payment.ProviderStatusSuccess, response.Status)
	require.Len(t, backend.listQueries, 2)
	require.Equal(t, "pi_123", backend.listQueries[0].Get("payment_intent"))
	require.Equal(t, "re_unrelated", backend.listQueries[1].Get("starting_after"))
}

func TestStripeQueryRefundAmbiguousOrUnavailableStaysUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages [][]*stripe.Refund
		err   error
	}{
		{name: "no_refunds"},
		{name: "transport_error", err: errors.New("connection reset")},
		{name: "multiple_matches", pages: [][]*stripe.Refund{{boundStripeRefund(), boundStripeRefund()}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &stripeRefundBindingBackend{pages: tc.pages, err: tc.err}
			response, err := newStripeRefundBindingProvider(backend).QueryRefund(context.Background(), payment.RefundQueryRequest{TradeNo: "pi_123", OrderID: "order_456", Amount: "40.00"})
			require.Error(t, err)
			require.Nil(t, response)
		})
	}
}
