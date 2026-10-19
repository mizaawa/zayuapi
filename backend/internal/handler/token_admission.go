package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type tokenAdmissionGateway interface {
	QuoteTokenAdmission(context.Context, service.TokenAdmissionInput) ([]byte, *service.TokenBillingQuote, error)
	ReserveTokenBilling(context.Context, *service.APIKey, *service.UserSubscription, *service.TokenBillingQuote) (*service.TokenBillingReservation, error)
	DispatchTokenBilling(context.Context, *service.TokenBillingReservation) error
	FinishTokenBilling(context.Context, *service.TokenBillingReservation) error
	CancelTokenBilling(context.Context, *service.TokenBillingReservation) error
}

const tokenAdmissionOpsStartKey = "token_billing_ops_start"
const tokenAdmissionWrittenStartKey = "token_billing_written_start"

// Admission is per selected account attempt. A failover never reuses a cheaper
// account's quote. The caller defers Finish immediately after this succeeds.
func beginTokenAdmission(c *gin.Context, gateway tokenAdmissionGateway, input service.TokenAdmissionInput) ([]byte, *service.TokenBillingReservation, error) {
	ctx := c.Request.Context()
	body, reservation, err := beginTokenAdmissionContext(ctx, gateway, input)
	if err != nil {
		return nil, nil, err
	}
	c.Request = c.Request.WithContext(service.WithTokenBillingReservation(ctx, reservation))
	events, _ := c.Get(service.OpsUpstreamErrorsKey)
	prior, _ := events.([]*service.OpsUpstreamErrorEvent)
	c.Set(tokenAdmissionOpsStartKey, len(prior))
	c.Set(tokenAdmissionWrittenStartKey, c.Writer.Size())
	return body, reservation, nil
}

func beginTokenAdmissionContext(ctx context.Context, gateway tokenAdmissionGateway, input service.TokenAdmissionInput) ([]byte, *service.TokenBillingReservation, error) {
	body, quote, err := gateway.QuoteTokenAdmission(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	reservation, err := gateway.ReserveTokenBilling(ctx, input.APIKey, input.Subscription, quote)
	if err != nil {
		return nil, nil, err
	}
	if err = gateway.DispatchTokenBilling(ctx, reservation); err != nil {
		_ = gateway.FinishTokenBilling(ctx, reservation)
		return nil, nil, err
	}
	return body, reservation, nil
}

func tokenAdmissionErrorDetails(err error) (int, string, string) {
	if errors.Is(err, service.ErrTokenAdmissionUnbounded) || errors.Is(err, service.ErrTokenBillingInvalidQuote) {
		return http.StatusBadRequest, "invalid_request_error", "Unable to establish a finite request cost: use a supported model and bounded output parameters"
	}
	status, code, message, _ := billingErrorDetails(err)
	return status, code, message
}

// HTTP status failure with a provider response is evidence of a rejected
// inference. A synthetic 502 after a transport timeout, partial delivery, or
// any observed usage is deliberately not refundable.
func tokenAttemptConfirmedUncharged(err error, hasUsage bool) bool {
	if err == nil || hasUsage {
		return false
	}
	var failover *service.UpstreamFailoverError
	// A failed WebSocket turn can lack usage despite delivering earlier frames.
	// Only pre-inference credential acquisition proves no inference occurred.
	// HTTP provider rejection is checked separately using its observed event.
	return errors.As(err, &failover) && failover.IsCredentialFailure()
}

func tokenHTTPAttemptConfirmedUncharged(c *gin.Context, err error, hasUsage bool) bool {
	if err == nil || hasUsage {
		return false
	}
	// Stream output may precede a later failure. An HTTP error envelope is
	// not inference delivery; any bytes on a successful/SSE response are.
	if c.Writer.Size() > c.GetInt(tokenAdmissionWrittenStartKey) &&
		(c.Writer.Status() < 400 || strings.Contains(c.Writer.Header().Get("Content-Type"), "text/event-stream")) {
		return false
	}
	raw, _ := c.Get(service.OpsUpstreamErrorsKey)
	events, _ := raw.([]*service.OpsUpstreamErrorEvent)
	start := c.GetInt(tokenAdmissionOpsStartKey)
	if start < 0 || start >= len(events) {
		var failover *service.UpstreamFailoverError
		return errors.As(err, &failover) && failover.IsCredentialFailure()
	}
	last := events[len(events)-1]
	// Network errors can carry a synthetic failover 502 body. They do not
	// prove rejection by the upstream and must retain the dispatched hold.
	if last == nil || last.Kind == "request_error" || last.UpstreamStatusCode == 0 {
		return false
	}
	return last != nil && last.UpstreamStatusCode >= 400 && last.UpstreamStatusCode <= 599 &&
		(last.Kind == "http_error" || last.Kind == "failover" || last.Kind == "retry_exhausted")
}

func finishOpenAITokenAttempt(c *gin.Context, gateway tokenAdmissionGateway, reservation *service.TokenBillingReservation, result *service.OpenAIForwardResult, err error) {
	hasUsage := result != nil && (result.ImageCount > 0 || result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 || result.Usage.CacheCreationInputTokens > 0 || result.Usage.CacheReadInputTokens > 0)
	if cyber := service.GetOpsCyberPolicy(c); cyber != nil && (cyber.UpstreamInTok > 0 || cyber.UpstreamOutTok > 0) {
		hasUsage = true
	}
	if tokenHTTPAttemptConfirmedUncharged(c, err, hasUsage) {
		if cancelErr := gateway.CancelTokenBilling(c.Request.Context(), reservation); cancelErr != nil {
			logger.L().Error("token_billing.cancel_failed", zap.Error(cancelErr))
		} else if service.GetOpsCyberPolicy(c) != nil {
			// The cyber log is still recorded asynchronously, but a confirmed
			// zero-usage rejection no longer owns a chargeable reservation.
			c.Request = c.Request.WithContext(service.WithTokenBillingReservation(c.Request.Context(), nil))
		}
	}
}

func finishGatewayTokenAttempt(c *gin.Context, gateway tokenAdmissionGateway, reservation *service.TokenBillingReservation, result *service.ForwardResult, err error) {
	hasUsage := tokenGatewayHasUsage(result)
	if tokenHTTPAttemptConfirmedUncharged(c, err, hasUsage) {
		if cancelErr := gateway.CancelTokenBilling(c.Request.Context(), reservation); cancelErr != nil {
			logger.L().Error("token_billing.cancel_failed", zap.Error(cancelErr))
		}
	}
}

func tokenGatewayHasUsage(result *service.ForwardResult) bool {
	return result != nil && (result.ImageCount > 0 || result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 || result.Usage.CacheCreationInputTokens > 0 || result.Usage.CacheReadInputTokens > 0 || result.Usage.ImageOutputTokens > 0)
}

type tokenWSTurnAdmissions struct {
	mu           sync.Mutex
	reservations map[int]*service.TokenBillingReservation
}

func (t *tokenWSTurnAdmissions) admit(ctx context.Context, gateway tokenAdmissionGateway, turn int, input service.TokenAdmissionInput) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reservations == nil {
		t.reservations = make(map[int]*service.TokenBillingReservation)
	}
	if existing, ok := t.reservations[turn]; ok {
		body, quote, err := gateway.QuoteTokenAdmission(ctx, input)
		if err != nil {
			return nil, err
		}
		if existing != nil && (quote == nil || quote.Command.BalanceCost > existing.Quote.Command.BalanceCost) {
			return nil, service.ErrTokenAdmissionUnbounded
		}
		return body, nil
	}
	body, reservation, err := beginTokenAdmissionContext(ctx, gateway, input)
	if err != nil {
		return nil, err
	}
	t.reservations[turn] = reservation
	return body, nil
}

func (t *tokenWSTurnAdmissions) forTurn(turn int) *service.TokenBillingReservation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reservations[turn]
}

func (t *tokenWSTurnAdmissions) finish(ctx context.Context, gateway tokenAdmissionGateway, turn int) {
	t.mu.Lock()
	r := t.reservations[turn]
	delete(t.reservations, turn)
	t.mu.Unlock()
	_ = gateway.FinishTokenBilling(ctx, r)
}

func (t *tokenWSTurnAdmissions) finishAll(ctx context.Context, gateway tokenAdmissionGateway) {
	t.mu.Lock()
	reservations := t.reservations
	t.reservations = nil
	t.mu.Unlock()
	for _, r := range reservations {
		_ = gateway.FinishTokenBilling(ctx, r)
	}
}
