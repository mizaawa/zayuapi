//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type transientUsageStage struct {
	DurableUsageBilling
	err                error
	calls, settlements int
	commands           []UsageBillingCommand
}

func (s *transientUsageStage) StageUsage(ctx context.Context, cmd *UsageBillingCommand, _ *UsageLog) error {
	s.calls++
	s.commands = append(s.commands, *cmd)
	if s.calls == 1 {
		return s.err
	}
	return ctx.Err()
}
func (s *transientUsageStage) SettleUsage(context.Context, string, int64, string) (*UsageBillingApplyResult, error) {
	s.settlements++
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestInFlightTextPricingRetriesSameReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &UsageBillingCommand{RequestID: "one-delivery", APIKeyID: 2, BalanceCost: 1.25}
	cmd.Normalize()
	r := &transientUsageStage{err: context.DeadlineExceeded}
	_, err := settleUsageReceipt(ctx, r, cmd, &UsageLog{})
	require.NoError(t, err)
	require.Equal(t, []UsageBillingCommand{*cmd, *cmd}, r.commands)
	require.Equal(t, 1, r.settlements)
	r = &transientUsageStage{err: ErrUsageBillingRequestConflict}
	_, err = settleUsageReceipt(ctx, r, cmd, &UsageLog{})
	require.ErrorIs(t, err, ErrUsageBillingRequestConflict)
	require.Equal(t, 1, r.calls)
	require.Zero(t, r.settlements)
}
