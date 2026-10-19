//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type tokenBillingRepoStub struct {
	UsageBillingRepository
	reserved    *TokenBillingReservation
	staged      *TokenBillingQuote
	stageCtxErr error
	settles     int
	releases    []bool
}

func (r *tokenBillingRepoStub) ReserveToken(_ context.Context, reservation *TokenBillingReservation) error {
	r.reserved = reservation
	return nil
}
func (r *tokenBillingRepoStub) DispatchToken(context.Context, string, int64, int64) error { return nil }
func (r *tokenBillingRepoStub) StageTokenOutcome(ctx context.Context, _ string, q *TokenBillingQuote) error {
	r.staged = q
	r.stageCtxErr = ctx.Err()
	return nil
}
func (r *tokenBillingRepoStub) SettleToken(context.Context, string) (*UsageBillingApplyResult, error) {
	r.settles++
	return &UsageBillingApplyResult{Applied: true, BalanceReserved: true}, nil
}
func (r *tokenBillingRepoStub) ReleaseToken(_ context.Context, _ string, onlyUndispatched bool) (*TokenBillingReservation, error) {
	r.releases = append(r.releases, onlyUndispatched)
	return r.reserved, nil
}
func (r *tokenBillingRepoStub) RecoverToken(context.Context, int) ([]int64, error) { return nil, nil }

func TestTokenBillingQuoteAndDetachedActualUsage(t *testing.T) {
	for _, kind := range []string{"openai", "gateway", "long_context"} {
		t.Run(kind, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{}
			repo := &tokenBillingRepoStub{}
			openai := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, repo, nil, nil, nil)
			gateway := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, repo, nil, nil)
			key := &APIKey{ID: 11, UserID: 12, Quota: 10}
			user := &User{ID: 12}
			account := &Account{ID: 13, Type: AccountTypeAPIKey}
			var quote *TokenBillingQuote
			var err error
			switch kind {
			case "openai":
				err = openai.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: user, Account: account, TokenQuote: &quote})
			case "gateway":
				err = gateway.RecordUsage(context.Background(), &RecordUsageInput{Result: &ForwardResult{Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: user, Account: account, TokenQuote: &quote})
			case "long_context":
				err = gateway.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{Result: &ForwardResult{Model: "gemini-3.1-pro", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 1000}}, APIKey: key, User: user, Account: account, LongContextThreshold: 200, LongContextMultiplier: 2, TokenQuote: &quote})
			}
			require.NoError(t, err)
			require.NotNil(t, quote)
			require.Positive(t, quote.Command.BalanceCost)
			require.Zero(t, usageRepo.calls)
			require.Nil(t, repo.staged)
			require.Zero(t, repo.settles)
			r, err := gateway.ReserveTokenBilling(context.Background(), key, nil, quote)
			require.NoError(t, err)
			require.NotNil(t, r)
			require.Equal(t, r.ID, r.Quote.Command.RequestID)
			require.NoError(t, gateway.DispatchTokenBilling(context.Background(), r))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			switch kind {
			case "openai":
				err = openai.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "untrusted-provider-id", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10, OutputTokens: 5}}, APIKey: key, User: user, Account: account, TokenReservation: r})
			case "gateway":
				err = gateway.RecordUsage(ctx, &RecordUsageInput{Result: &ForwardResult{RequestID: "untrusted-provider-id", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}}, APIKey: key, User: user, Account: account, TokenReservation: r})
			case "long_context":
				err = gateway.RecordUsageWithLongContext(ctx, &RecordUsageLongContextInput{Result: &ForwardResult{RequestID: "untrusted-provider-id", Model: "gemini-3.1-pro", Usage: ClaudeUsage{InputTokens: 10, OutputTokens: 5}}, APIKey: key, User: user, Account: account, TokenReservation: r, LongContextThreshold: 200, LongContextMultiplier: 2})
			}
			require.NoError(t, err)
			require.NoError(t, repo.stageCtxErr)
			require.Equal(t, 1, repo.settles)
			require.Equal(t, r.ID, repo.staged.Command.RequestID)
			require.Equal(t, r.ID, repo.staged.Usage.RequestID)
			require.Positive(t, repo.staged.Command.BalanceCost)
			require.Less(t, repo.staged.Command.BalanceCost, r.Quote.Command.BalanceCost)
			require.Equal(t, 1, usageRepo.calls)
		})
	}
}

func TestTokenBillingMissingUsageUsesAdmittedMaximum(t *testing.T) {
	repo := &tokenBillingRepoStub{}
	usageRepo := &openAIRecordUsageLogRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, repo, nil, nil, nil)
	r := &TokenBillingReservation{ID: "token:internal-only", UserID: 12, APIKeyID: 11, Quote: TokenBillingQuote{Command: UsageBillingCommand{RequestID: "token:internal-only", UserID: 12, APIKeyID: 11, BalanceCost: .03, APIKeyQuotaCost: .03}, Usage: UsageLog{RequestID: "token:internal-only", UserID: 12, APIKeyID: 11, ActualCost: .03}}}
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "gpt-5.1"}, APIKey: &APIKey{ID: 11, UserID: 12}, User: &User{ID: 12}, Account: &Account{ID: 13}, TokenReservation: r})
	require.NoError(t, err)
	require.Equal(t, .03, repo.staged.Command.BalanceCost)
	require.Equal(t, .03, repo.staged.Usage.ActualCost)
	require.Equal(t, .03, usageRepo.lastLog.ActualCost)
	require.True(t, repo.staged.Maximum)
	require.Zero(t, repo.staged.Usage.TotalTokens())
}

func TestTokenBillingExemptionsAndReleaseIntent(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"simple", "managed", "subscription"} {
		t.Run(kind, func(t *testing.T) {
			svc := &GatewayService{cfg: &config.Config{}}
			key := &APIKey{}
			var sub *UserSubscription
			switch kind {
			case "simple":
				svc.cfg.RunMode = config.RunModeSimple
			case "managed":
				key.Purpose = APIKeyPurposeChannelMonitor
			case "subscription":
				key.Group = &Group{SubscriptionType: SubscriptionTypeSubscription}
				sub = &UserSubscription{}
			}
			r, err := svc.ReserveTokenBilling(ctx, key, sub, nil)
			require.NoError(t, err)
			require.Nil(t, r)
		})
	}
	repo := &tokenBillingRepoStub{}
	svc := &GatewayService{usageBillingRepo: repo}
	r := &TokenBillingReservation{ID: "token:internal-only"}
	require.NoError(t, svc.FinishTokenBilling(ctx, r))
	require.NoError(t, svc.CancelTokenBilling(ctx, r))
	require.Equal(t, []bool{true, false}, repo.releases)
	_, err := (&GatewayService{}).ReserveTokenBilling(ctx, &APIKey{}, nil, nil)
	require.ErrorIs(t, err, ErrBillingServiceUnavailable)
}

func TestTokenBillingCacheNeverDeductsReservationTwice(t *testing.T) {
	cache := &balanceEligibilityCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{User: &User{ID: 12}, Cost: &CostBreakdown{ActualCost: 1}}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{BalanceReserved: true})
	require.EqualValues(t, 1, cache.invalidateCalls.Load())
	require.Zero(t, cache.deductCalls.Load())
}
