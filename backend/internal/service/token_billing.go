package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrTokenBillingOutcomePending = errors.New("token billing outcome is pending")
	ErrTokenBillingInvalidQuote   = errors.New("token billing requires a finite nonnegative maximum quote")
)

// TokenBillingQuote uses precisely the same price and ledger representation as
// ordinary usage. A conservative synthetic result can therefore be priced by
// RecordUsage without maintaining a second pricing implementation.
type TokenBillingQuote struct {
	Command UsageBillingCommand `json:"command"`
	Usage   UsageLog            `json:"usage"`
	// Maximum distinguishes an unreported outcome from observed usage. A
	// subsequently recovered provider receipt may correct it exactly once.
	Maximum bool `json:"maximum,omitempty"`
}

const TokenBillingReservationLease = 6 * time.Hour

type TokenBillingReservation struct {
	ID         string            `json:"id"`
	UserID     int64             `json:"user_id"`
	APIKeyID   int64             `json:"api_key_id"`
	GroupID    int64             `json:"group_id"`
	Quote      TokenBillingQuote `json:"quote"`
	LeaseUntil time.Time         `json:"lease_until"`
}

// This optional interface keeps existing billing implementations and test
// doubles source compatible. Production admission fails closed without it.
type TokenBillingRepository interface {
	ReserveToken(context.Context, *TokenBillingReservation) error
	DispatchToken(context.Context, string, int64, int64) error
	StageTokenOutcome(context.Context, string, *TokenBillingQuote) error
	SettleToken(context.Context, string) (*UsageBillingApplyResult, error)
	ReleaseToken(context.Context, string, bool) (*TokenBillingReservation, error)
	RecoverToken(context.Context, int) ([]int64, error)
}

type tokenBillingContextKey struct{}

func WithTokenBillingReservation(ctx context.Context, reservation *TokenBillingReservation) context.Context {
	return context.WithValue(ctx, tokenBillingContextKey{}, reservation)
}

func TokenBillingReservationFromContext(ctx context.Context) *TokenBillingReservation {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(tokenBillingContextKey{}).(*TokenBillingReservation)
	return r
}

func tokenBillingExempt(cfg *config.Config, key *APIKey, subscription *UserSubscription) bool {
	return (cfg != nil && cfg.RunMode == config.RunModeSimple) || (key != nil &&
		(key.IsManaged() || (subscription != nil && key.Group != nil && key.Group.IsSubscriptionType())))
}

func reserveTokenBilling(ctx context.Context, repo UsageBillingRepository, cache *BillingCacheService, cfg *config.Config, key *APIKey, subscription *UserSubscription, quote *TokenBillingQuote) (*TokenBillingReservation, error) {
	if tokenBillingExempt(cfg, key, subscription) {
		return nil, nil
	}
	tokenRepo, ok := repo.(TokenBillingRepository)
	if !ok {
		return nil, ErrBillingServiceUnavailable
	}
	if key == nil || quote == nil || quote.Command.UserID != key.UserID || quote.Command.APIKeyID != key.ID ||
		quote.Command.BalanceCost < 0 || math.IsNaN(quote.Command.BalanceCost) || math.IsInf(quote.Command.BalanceCost, 0) {
		return nil, ErrTokenBillingInvalidQuote
	}
	r := &TokenBillingReservation{ID: "token:" + uuid.NewString(), UserID: key.UserID, APIKeyID: key.ID, Quote: *quote, LeaseUntil: time.Now().Add(TokenBillingReservationLease)}
	if key.GroupID != nil {
		r.GroupID = *key.GroupID
	}
	r.Quote.Command.RequestID, r.Quote.Usage.RequestID = r.ID, r.ID
	if err := ValidateTokenBillingQuote(&r.Quote); err != nil {
		return nil, err
	}
	NormalizeTokenBillingQuote(&r.Quote)
	if err := tokenRepo.ReserveToken(ctx, r); err != nil {
		return nil, err
	}
	invalidateTokenBalance(ctx, cache, r.UserID)
	return r, nil
}

func invalidateTokenBalance(ctx context.Context, cache *BillingCacheService, userID int64) {
	if cache != nil {
		if err := cache.InvalidateUserBalance(ctx, userID); err != nil {
			logger.L().Warn("token_billing.cache_invalidation_failed", zap.Int64("user_id", userID), zap.Error(err))
		}
	}
}

func dispatchTokenBilling(ctx context.Context, repo UsageBillingRepository, r *TokenBillingReservation) error {
	if r == nil {
		return nil
	}
	tokenRepo, ok := repo.(TokenBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	return tokenRepo.DispatchToken(ctx, r.ID, r.UserID, r.APIKeyID)
}

func finishTokenBilling(ctx context.Context, repo UsageBillingRepository, cache *BillingCacheService, r *TokenBillingReservation, onlyUndispatched bool) error {
	if r == nil {
		return nil
	}
	tokenRepo, ok := repo.(TokenBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	released, err := tokenRepo.ReleaseToken(billingCtx, r.ID, onlyUndispatched)
	if err == nil && released != nil {
		invalidateTokenBalance(billingCtx, cache, released.UserID)
	}
	return err
}

func (s *OpenAIGatewayService) ReserveTokenBilling(ctx context.Context, key *APIKey, subscription *UserSubscription, quote *TokenBillingQuote) (*TokenBillingReservation, error) {
	return reserveTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, s.cfg, key, subscription, quote)
}
func (s *GatewayService) ReserveTokenBilling(ctx context.Context, key *APIKey, subscription *UserSubscription, quote *TokenBillingQuote) (*TokenBillingReservation, error) {
	return reserveTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, s.cfg, key, subscription, quote)
}
func (s *OpenAIGatewayService) DispatchTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return dispatchTokenBilling(ctx, s.usageBillingRepo, r)
}
func (s *GatewayService) DispatchTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return dispatchTokenBilling(ctx, s.usageBillingRepo, r)
}

// Finish releases only work which never crossed the dispatch boundary. An
// asynchronous usage recorder owns the dispatched request; on crash recovery
// captures its maximum instead of granting an unprovable refund.
func (s *OpenAIGatewayService) FinishTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return finishTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, r, true)
}
func (s *GatewayService) FinishTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return finishTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, r, true)
}

// Cancel requires an upstream-confirmed no-charge failure. Timeouts, partial
// output, missing usage and client disconnects are not proof of no charge.
func (s *OpenAIGatewayService) CancelTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return finishTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, r, false)
}
func (s *GatewayService) CancelTokenBilling(ctx context.Context, r *TokenBillingReservation) error {
	return finishTokenBilling(ctx, s.usageBillingRepo, s.billingCacheService, r, false)
}

func stageAndSettleTokenBilling(ctx context.Context, repo UsageBillingRepository, r *TokenBillingReservation, cmd *UsageBillingCommand, usage *UsageLog) (*UsageBillingApplyResult, error) {
	tokenRepo, ok := repo.(TokenBillingRepository)
	if !ok {
		return nil, ErrBillingServiceUnavailable
	}
	if r.UserID != cmd.UserID || r.APIKeyID != cmd.APIKeyID || usage == nil {
		return nil, ErrUsageBillingRequestConflict
	}
	quote := TokenBillingQuote{Command: *cmd, Usage: *usage}
	quote.Command.RequestID, quote.Usage.RequestID = r.ID, r.ID
	quote.Command.RequestFingerprint = ""
	quote.Command.RequestPayloadHash = r.Quote.Command.RequestPayloadHash
	// A successful response without usage is not an authorization to refund its
	// full reservation. Persist the admitted maximum for deterministic recovery.
	// Observed tokens may legitimately cost zero: an explicit free price, a
	// zero group multiplier, or rounding below the accounting scale. Pricing
	// failures are rejected by RecordUsage before this point, never inferred
	// from the amount. Only absent usage requires the conservative maximum.
	if usage.TotalTokens() <= 0 {
		quote = TokenBillingMaximumQuote(r.Quote)
	}
	NormalizeTokenBillingQuote(&quote)
	*cmd = quote.Command
	*usage = quote.Usage
	if err := tokenRepo.StageTokenOutcome(ctx, r.ID, &quote); err != nil {
		return nil, err
	}
	result, err := tokenRepo.SettleToken(ctx, r.ID)
	if err == nil && result != nil && result.OutstandingBalanceCost > 0 {
		logger.L().Error("token_billing.maximum_exceeded", zap.String("reservation_id", r.ID), zap.Float64("outstanding", result.OutstandingBalanceCost))
	}
	return result, err
}

func (s *OpenAIGatewayService) startTokenBillingReconciler() {
	repo, ok := s.usageBillingRepo.(TokenBillingRepository)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.tokenBillingCancel = cancel
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			checkCtx, checkCancel := context.WithTimeout(ctx, 25*time.Second)
			users, err := repo.RecoverToken(checkCtx, 100)
			for _, id := range users {
				invalidateTokenBalance(checkCtx, s.billingCacheService, id)
			}
			checkCancel()
			if err != nil && ctx.Err() == nil {
				logger.L().Warn("token_billing.recovery_retry", zap.Error(err))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func validateTokenBillingQuote(q *TokenBillingQuote) error {
	if q == nil {
		return ErrTokenBillingInvalidQuote
	}
	for _, v := range []float64{q.Command.BalanceCost, q.Command.SubscriptionCost, q.Command.APIKeyQuotaCost, q.Command.APIKeyRateLimitCost, q.Command.AccountQuotaCost, q.Command.UserPlatformQuotaCost} {
		if v < 0 || v >= 1e12 || math.IsNaN(v) || math.IsInf(v, 0) {
			return ErrTokenBillingInvalidQuote
		}
	}
	if q.Command.UserID <= 0 || q.Command.APIKeyID <= 0 || strings.TrimSpace(q.Command.RequestID) == "" || q.Command.SubscriptionCost != 0 || q.Command.SubscriptionID != nil {
		return fmt.Errorf("%w: invalid token billing identity", ErrTokenBillingInvalidQuote)
	}
	return nil
}

// ValidateTokenBillingQuote is also enforced at the repository boundary so
// alternate callers cannot inject NaN, negative fees or another billing mode.
func ValidateTokenBillingQuote(q *TokenBillingQuote) error { return validateTokenBillingQuote(q) }

// New token receipts have no legacy-fingerprint compatibility requirement.
// Bind the normalized database amounts and identity even when a caller supplied
// a stale or fabricated fingerprint; repeated stage operations remain stable at
// the 1e-8 accounting boundary.
func NormalizeTokenBillingQuote(q *TokenBillingQuote) {
	q.Command.quantizeMonetaryFields()
	q.Command.RequestFingerprint = ""
	q.Command.Normalize()
	q.Usage.UserID, q.Usage.APIKeyID, q.Usage.RequestID = q.Command.UserID, q.Command.APIKeyID, q.Command.RequestID
	q.Usage.ActualCost = q.Command.BalanceCost
}

// Missing usage has a known authorized fee ceiling, not an observed token
// count. Keep the maximum price receipt but do not invent provider usage in
// statistics. The reservation's immutable admission quote retains the estimate.
func TokenBillingMaximumQuote(quote TokenBillingQuote) TokenBillingQuote {
	quote.Maximum = true
	quote.Usage.InputTokens, quote.Usage.OutputTokens = 0, 0
	quote.Usage.CacheCreationTokens, quote.Usage.CacheReadTokens = 0, 0
	quote.Usage.CacheCreation5mTokens, quote.Usage.CacheCreation1hTokens = 0, 0
	quote.Usage.ImageInputTokens, quote.Usage.ImageOutputTokens = 0, 0
	return quote
}
