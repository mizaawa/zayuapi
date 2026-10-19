//go:build unit

package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Existing partial-usage and media fixtures now also implement the optional
// token repository contract, preserving their unified receipt assertions.
func (r *partialUsageBillingRepo) ReserveToken(ctx context.Context, hold *service.TokenBillingReservation) error {
	if r.tokenBilling.receipts == nil {
		r.tokenBilling.balance = 10000
	}
	return r.tokenBilling.ReserveToken(ctx, hold)
}

func (r *partialUsageBillingRepo) DispatchToken(ctx context.Context, id string, userID, keyID int64) error {
	return r.tokenBilling.DispatchToken(ctx, id, userID, keyID)
}

func (r *partialUsageBillingRepo) StageTokenOutcome(ctx context.Context, id string, quote *service.TokenBillingQuote) error {
	return r.tokenBilling.StageTokenOutcome(ctx, id, quote)
}

func (r *partialUsageBillingRepo) SettleToken(ctx context.Context, id string) (*service.UsageBillingApplyResult, error) {
	before := len(r.tokenBilling.commands)
	result, err := r.tokenBilling.SettleToken(ctx, id)
	if err == nil && len(r.tokenBilling.commands) > before {
		r.commands = append(r.commands, r.tokenBilling.commands[before:]...)
	}
	return result, err
}

func (r *partialUsageBillingRepo) ReleaseToken(ctx context.Context, id string, onlyUndispatched bool) (*service.TokenBillingReservation, error) {
	return r.tokenBilling.ReleaseToken(ctx, id, onlyUndispatched)
}

func (r *partialUsageBillingRepo) RecoverToken(context.Context, int) ([]int64, error) {
	return nil, nil
}
