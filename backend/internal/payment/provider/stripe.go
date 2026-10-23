package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

// Stripe constants.
const (
	stripeEventPaymentSuccess = "payment_intent.succeeded"
	stripeEventPaymentFailed  = "payment_intent.payment_failed"
)

// Stripe implements the payment.CancelableProvider interface for Stripe payments.
type Stripe struct {
	instanceID string
	config     map[string]string

	mu          sync.Mutex
	initialized bool
	sc          *stripe.Client
}

// NewStripe creates a new Stripe provider instance.
func NewStripe(instanceID string, config map[string]string) (*Stripe, error) {
	if config["secretKey"] == "" {
		return nil, fmt.Errorf("stripe config missing required key: secretKey")
	}
	cfg := cloneStringMap(config)
	currency, err := payment.NormalizePaymentCurrency(cfg["currency"])
	if err != nil {
		return nil, fmt.Errorf("stripe config currency: %w", err)
	}
	cfg["currency"] = currency
	return &Stripe{
		instanceID: instanceID,
		config:     cfg,
	}, nil
}

func (s *Stripe) ensureInit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		s.sc = stripe.NewClient(s.config["secretKey"])
		s.initialized = true
	}
}

// GetPublishableKey returns the publishable key for frontend use.
func (s *Stripe) GetPublishableKey() string {
	return s.config["publishableKey"]
}

func (s *Stripe) Name() string        { return "Stripe" }
func (s *Stripe) ProviderKey() string { return payment.TypeStripe }
func (s *Stripe) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}

func (s *Stripe) MerchantIdentityMetadata() map[string]string {
	if s == nil {
		return nil
	}
	return map[string]string{"currency": s.currency()}
}

func (s *Stripe) currency() string {
	if s == nil {
		return payment.DefaultPaymentCurrency
	}
	currency, err := payment.NormalizePaymentCurrency(s.config["currency"])
	if err != nil {
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// stripePaymentMethodTypes maps our PaymentType to Stripe payment_method_types.
var stripePaymentMethodTypes = map[string][]string{
	payment.TypeCard:   {"card"},
	payment.TypeAlipay: {"alipay"},
	payment.TypeWxpay:  {"wechat_pay"},
	payment.TypeLink:   {"link"},
}

// CreatePayment creates a Stripe PaymentIntent.
func (s *Stripe) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	s.ensureInit()

	currency := s.currency()
	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, currency)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	// Collect all Stripe payment_method_types from the instance's configured sub-methods
	methods := resolveStripeMethodTypes(req.InstanceSubMethods)

	pmTypes := make([]*string, len(methods))
	for i, m := range methods {
		pmTypes[i] = stripe.String(m)
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:             stripe.Int64(amountInMinorUnit),
		Currency:           stripe.String(strings.ToLower(currency)),
		PaymentMethodTypes: pmTypes,
		Description:        stripe.String(req.Subject),
		Metadata:           map[string]string{"orderId": req.OrderID},
	}

	// WeChat Pay requires payment_method_options with client type
	if hasStripeMethod(methods, "wechat_pay") {
		params.PaymentMethodOptions = &stripe.PaymentIntentCreatePaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentCreatePaymentMethodOptionsWeChatPayParams{
				Client: stripe.String("web"),
			},
		}
	}

	params.SetIdempotencyKey(fmt.Sprintf("pi-%s", req.OrderID))
	params.Context = ctx

	pi, err := s.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	return &payment.CreatePaymentResponse{
		TradeNo:      pi.ID,
		ClientSecret: pi.ClientSecret,
		Currency:     currency,
	}, nil
}

// QueryOrder retrieves a PaymentIntent by ID.
func (s *Stripe) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	s.ensureInit()

	pi, err := s.sc.V1PaymentIntents.Retrieve(ctx, tradeNo, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query order: %w", err)
	}

	status := payment.ProviderStatusPending
	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded:
		status = payment.ProviderStatusPaid
	case stripe.PaymentIntentStatusCanceled:
		status = payment.ProviderStatusFailed
	}

	currency := stripeIntentCurrency(pi.Currency, s.currency())
	return &payment.QueryOrderResponse{
		TradeNo: pi.ID,
		Status:  status,
		// A succeeded intent may have been only partially captured.
		Amount: payment.MinorUnitToAmount(pi.AmountReceived, currency),
		Metadata: map[string]string{
			"currency": currency,
		},
	}, nil
}

// VerifyNotification verifies a Stripe webhook event.
func (s *Stripe) VerifyNotification(_ context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	s.ensureInit()

	webhookSecret := s.config["webhookSecret"]
	if webhookSecret == "" {
		return nil, fmt.Errorf("stripe webhookSecret not configured")
	}

	sig := headers["stripe-signature"]
	if sig == "" {
		return nil, fmt.Errorf("stripe notification missing stripe-signature header")
	}

	event, err := webhook.ConstructEvent([]byte(rawBody), sig, webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe verify notification: %w", err)
	}

	switch event.Type {
	case stripeEventPaymentSuccess:
		return parseStripePaymentIntent(&event, payment.ProviderStatusSuccess, rawBody)
	case stripeEventPaymentFailed:
		return parseStripePaymentIntent(&event, payment.ProviderStatusFailed, rawBody)
	}

	return nil, nil
}

func parseStripePaymentIntent(event *stripe.Event, status string, rawBody string) (*payment.PaymentNotification, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return nil, fmt.Errorf("stripe parse payment_intent: %w", err)
	}
	currency := stripeIntentCurrency(pi.Currency, payment.DefaultPaymentCurrency)
	return &payment.PaymentNotification{
		TradeNo: pi.ID,
		OrderID: pi.Metadata["orderId"],
		Amount:  payment.MinorUnitToAmount(pi.AmountReceived, currency),
		Status:  status,
		RawData: rawBody,
		Metadata: map[string]string{
			"currency": currency,
		},
	}, nil
}

// Refund creates a Stripe refund.
func (s *Stripe) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	amountInMinorUnit, err := payment.AmountToMinorUnit(req.Amount, s.currency())
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(req.TradeNo),
		Amount:        stripe.Int64(amountInMinorUnit),
		Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
		Metadata: map[string]string{
			"orderId":         req.OrderID,
			"refundRequestId": stripeRefundRequestID(req.OrderID, amountInMinorUnit),
		},
	}
	params.SetIdempotencyKey(stripeRefundRequestID(req.OrderID, amountInMinorUnit))
	params.Context = ctx

	r, err := s.sc.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	refundStatus := payment.ProviderStatusPending
	if r.Status == stripe.RefundStatusSucceeded {
		refundStatus = payment.ProviderStatusSuccess
	}

	return &payment.RefundResponse{
		RefundID: r.ID,
		Status:   refundStatus,
	}, nil
}

func stripeRefundRequestID(orderID string, amountInMinorUnit int64) string {
	return fmt.Sprintf("re-%s-%d", orderID, amountInMinorUnit)
}

// QueryRefund only accepts a refund bound to the current request. When a create
// response was lost, metadata recovers that request without trusting list order.
func (s *Stripe) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	tradeNo := strings.TrimSpace(req.TradeNo)
	if tradeNo == "" {
		return nil, fmt.Errorf("stripe query refund: missing payment intent id")
	}
	currency := s.currency()
	amount, err := payment.AmountToMinorUnit(req.Amount, currency)
	if err != nil || amount <= 0 {
		return nil, fmt.Errorf("stripe query refund: invalid refund amount")
	}
	var r *stripe.Refund
	refundID := strings.TrimSpace(req.RefundID)
	if refundID != "" {
		r, err = s.sc.V1Refunds.Retrieve(ctx, refundID, nil)
		if err != nil {
			return nil, fmt.Errorf("stripe query refund: %w", err)
		}
		if r == nil || r.ID != refundID {
			return nil, fmt.Errorf("stripe query refund: refund id mismatch")
		}
	} else {
		if strings.TrimSpace(req.OrderID) == "" {
			return nil, fmt.Errorf("stripe query refund: missing order id")
		}
		requestID := stripeRefundRequestID(req.OrderID, amount)
		params := &stripe.RefundListParams{PaymentIntent: stripe.String(tradeNo)}
		params.Limit = stripe.Int64(100)
		list := s.sc.V1Refunds.List(ctx, params)
		for candidate, listErr := range list.All(ctx) {
			if listErr != nil {
				return nil, fmt.Errorf("stripe query refund: %w", listErr)
			}
			if candidate == nil || candidate.Metadata["orderId"] != req.OrderID || candidate.Metadata["refundRequestId"] != requestID {
				continue
			}
			if r != nil {
				return nil, fmt.Errorf("stripe query refund: ambiguous refund request")
			}
			r = candidate
		}
		if r == nil {
			return nil, fmt.Errorf("stripe query refund: current refund not identified")
		}
	}
	if r.ID == "" || r.PaymentIntent == nil || r.PaymentIntent.ID != tradeNo || r.Amount != amount || !strings.EqualFold(string(r.Currency), currency) {
		return nil, fmt.Errorf("stripe query refund: payment intent, amount or currency mismatch")
	}
	// Stripe may prune an idempotency key before a later same-amount retry.
	// A failed metadata match alone cannot prove that the latest attempt failed.
	if refundID == "" && stripeRefundProviderStatus(r.Status) == payment.ProviderStatusFailed {
		return nil, fmt.Errorf("stripe query refund: failed refund requires its exact refund id")
	}

	return &payment.RefundResponse{RefundID: r.ID, Status: stripeRefundProviderStatus(r.Status)}, nil
}

func stripeRefundProviderStatus(status stripe.RefundStatus) string {
	switch status {
	case stripe.RefundStatusSucceeded:
		return payment.ProviderStatusSuccess
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		return payment.ProviderStatusFailed
	default:
		return payment.ProviderStatusPending
	}
}

func stripeIntentCurrency(raw stripe.Currency, fallback string) string {
	currency, err := payment.NormalizePaymentCurrency(string(raw))
	if err != nil || currency == payment.DefaultPaymentCurrency && strings.TrimSpace(string(raw)) == "" {
		normalizedFallback, fallbackErr := payment.NormalizePaymentCurrency(fallback)
		if fallbackErr == nil {
			return normalizedFallback
		}
		return payment.DefaultPaymentCurrency
	}
	return currency
}

// resolveStripeMethodTypes converts instance supported_types (comma-separated)
// into Stripe API payment_method_types. Falls back to ["card"] if empty.
func resolveStripeMethodTypes(instanceSubMethods string) []string {
	if instanceSubMethods == "" {
		return []string{"card"}
	}
	var methods []string
	for _, t := range strings.Split(instanceSubMethods, ",") {
		t = strings.TrimSpace(t)
		if mapped, ok := stripePaymentMethodTypes[t]; ok {
			methods = append(methods, mapped...)
		}
	}
	if len(methods) == 0 {
		return []string{"card"}
	}
	return methods
}

// hasStripeMethod checks if the given Stripe method list contains the target method.
func hasStripeMethod(methods []string, target string) bool {
	for _, m := range methods {
		if m == target {
			return true
		}
	}
	return false
}

// CancelPayment cancels a pending PaymentIntent.
func (s *Stripe) CancelPayment(ctx context.Context, tradeNo string) error {
	s.ensureInit()

	_, err := s.sc.V1PaymentIntents.Cancel(ctx, tradeNo, nil)
	if err != nil {
		return fmt.Errorf("stripe cancel payment: %w", err)
	}
	return nil
}

// Ensure interface compliance.
var (
	_ payment.Provider                 = (*Stripe)(nil)
	_ payment.CancelableProvider       = (*Stripe)(nil)
	_ payment.MerchantIdentityProvider = (*Stripe)(nil)
)
