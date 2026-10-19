//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type tokenBillingFixture struct {
	repo    *usageBillingRepository
	user    *service.User
	key     *service.APIKey
	account *service.Account
}

func tokenFixture(t *testing.T, balance float64) tokenBillingFixture {
	t.Helper()
	client := testEntClient(t)
	u := mustCreateUser(t, client, &service.User{Email: uuid.NewString() + "@token.test", Balance: balance})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: u.ID, Key: "sk-" + uuid.NewString(), Quota: 10, RateLimit5h: 10})
	a := mustCreateAccount(t, client, &service.Account{Name: "token-" + uuid.NewString(), Type: service.AccountTypeAPIKey, Extra: map[string]any{"quota_limit": 100.0}})
	return tokenBillingFixture{repo: &usageBillingRepository{db: integrationDB}, user: u, key: k, account: a}
}

func (f tokenBillingFixture) reservation(amount float64) *service.TokenBillingReservation {
	id := "token:" + uuid.NewString()
	now := time.Now().UTC()
	mode := string(service.BillingModeToken)
	return &service.TokenBillingReservation{ID: id, UserID: f.user.ID, APIKeyID: f.key.ID, LeaseUntil: now.Add(time.Hour), Quote: service.TokenBillingQuote{
		Command: service.UsageBillingCommand{RequestID: id, UserID: f.user.ID, APIKeyID: f.key.ID, AccountID: f.account.ID, AccountType: service.AccountTypeAPIKey, Model: "token-test", BalanceCost: amount, APIKeyQuotaCost: amount, APIKeyRateLimitCost: amount, AccountQuotaCost: amount, InputTokens: 10, OutputTokens: 100, RequestPayloadHash: "payload-test", ChargedAt: now},
		Usage:   service.UsageLog{RequestID: id, UserID: f.user.ID, APIKeyID: f.key.ID, AccountID: f.account.ID, Model: "token-test", ActualCost: amount, TotalCost: amount, InputTokens: 10, OutputTokens: 100, BillingMode: &mode, CreatedAt: now},
	}}
}

func tokenActual(r *service.TokenBillingReservation, amount float64) *service.TokenBillingQuote {
	quote := r.Quote
	quote.Command.BalanceCost, quote.Command.APIKeyQuotaCost, quote.Command.APIKeyRateLimitCost, quote.Command.AccountQuotaCost = amount, amount, amount, amount
	quote.Command.RequestFingerprint = ""
	quote.Command.OutputTokens = 25
	quote.Usage.OutputTokens = 25
	quote.Usage.ActualCost, quote.Usage.TotalCost = service.QuantizeUsageBillingAmount(amount), service.QuantizeUsageBillingAmount(amount)
	return &quote
}

func tokenMoney(t *testing.T, db *sql.DB, userID int64, wantBalance, wantFrozen string) {
	t.Helper()
	var balance, frozen string
	require.NoError(t, db.QueryRow(`SELECT balance::text,frozen_balance::text FROM users WHERE id=$1`, userID).Scan(&balance, &frozen))
	require.Equal(t, wantBalance, balance)
	require.Equal(t, wantFrozen, frozen)
}

func TestTokenBillingConcurrentReservationsAndIdempotentRefund(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, .10)
	accepted := make(chan *service.TokenBillingReservation, 32)
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := f.reservation(.03)
			if err := f.repo.ReserveToken(ctx, r); err == nil {
				accepted <- r
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(accepted)
	close(errs)
	require.Len(t, accepted, 3)
	for err := range errs {
		require.ErrorIs(t, err, service.ErrInsufficientBalance)
	}
	tokenMoney(t, integrationDB, f.user.ID, "0.01000000", "0.09000000")
	for r := range accepted {
		require.NoError(t, f.repo.ReserveToken(ctx, r))
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				_, err := f.repo.ReleaseToken(ctx, id, true)
				require.NoError(t, err)
			}(r.ID)
		}
	}
	wg.Wait()
	tokenMoney(t, integrationDB, f.user.ID, "0.10000000", "0.00000000")
	t.Log("32 concurrent requests: 3 holds accepted, 29 insufficient; repeated refunds restore exactly 0.10000000")
}

func TestTokenBillingSettlementAllLedgersExactlyOnce(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 10)
	r := f.reservation(2)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	require.ErrorIs(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID), service.ErrUsageBillingRequestConflict)
	quote := tokenActual(r, .75)
	quote.Command.Platform, quote.Command.UserPlatformQuotaCost = service.PlatformOpenAI, .75
	quote.Command.PlatformDailyWindowStart, quote.Command.PlatformWeeklyWindowStart = quote.Command.ChargedAt, quote.Command.ChargedAt
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, quote))
	var applied atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copyQuote := *quote
			require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, &copyQuote))
			result, err := f.repo.SettleToken(ctx, r.ID)
			require.NoError(t, err)
			if result != nil && result.Applied {
				applied.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, applied.Load())
	tokenMoney(t, integrationDB, f.user.ID, "9.25000000", "0.00000000")
	var keyQuota, keyRate, accountQuota, platformQuota float64
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=$1`, f.key.ID).Scan(&keyQuota, &keyRate))
	require.NoError(t, integrationDB.QueryRow(`SELECT (extra->>'quota_used')::numeric FROM accounts WHERE id=$1`, f.account.ID).Scan(&accountQuota))
	require.NoError(t, integrationDB.QueryRow(`SELECT daily_usage_usd FROM user_platform_quotas WHERE user_id=$1 AND platform=$2`, f.user.ID, service.PlatformOpenAI).Scan(&platformQuota))
	for _, amount := range []float64{keyQuota, keyRate, accountQuota, platformQuota} {
		require.Equal(t, .75, amount)
	}
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM usage_logs WHERE request_id=$1`, r.ID).Scan(&count))
	require.Equal(t, 1, count)
	result, err := f.repo.Apply(ctx, &quote.Command)
	require.NoError(t, err)
	require.False(t, result.Applied)
	_, err = f.repo.ReleaseToken(ctx, r.ID, false)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "9.25000000", "0.00000000")
	t.Log("24 repeated settlements: exactly 1 full charge, all quota ledgers 0.75; one usage row")
}

func TestTokenBillingConfirmedFailureAndPendingUsageFence(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 1)
	r := f.reservation(.3)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	released, err := f.repo.ReleaseToken(ctx, r.ID, true)
	require.NoError(t, err)
	require.Nil(t, released)
	tokenMoney(t, integrationDB, f.user.ID, "0.70000000", "0.30000000")
	released, err = f.repo.ReleaseToken(ctx, r.ID, false)
	require.NoError(t, err)
	require.NotNil(t, released)
	released, err = f.repo.ReleaseToken(ctx, r.ID, false)
	require.NoError(t, err)
	require.Nil(t, released)
	tokenMoney(t, integrationDB, f.user.ID, "1.00000000", "0.00000000")
	require.ErrorIs(t, f.repo.StageTokenOutcome(ctx, r.ID, tokenActual(r, .1)), service.ErrUsageBillingRequestConflict)
	r = f.reservation(.3)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, tokenActual(r, .1)))
	_, err = f.repo.ReleaseToken(ctx, r.ID, false)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	_, err = f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.90000000", "0.00000000")
}

func TestTokenBillingCrashRecoveryAndLateExactUsage(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 10)
	notSent, sent, staged := f.reservation(2), f.reservation(2), f.reservation(2)
	for _, r := range []*service.TokenBillingReservation{notSent, sent, staged} {
		require.NoError(t, f.repo.ReserveToken(ctx, r))
	}
	for _, r := range []*service.TokenBillingReservation{sent, staged} {
		require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	}
	require.NoError(t, f.repo.StageTokenOutcome(ctx, staged.ID, tokenActual(staged, .5)))
	_, err := integrationDB.Exec(`UPDATE token_billing_reservations SET lease_until=NOW()-INTERVAL '1 minute' WHERE user_id=$1`, f.user.ID)
	require.NoError(t, err)
	users, err := f.repo.RecoverToken(ctx, 1000)
	require.NoError(t, err)
	require.Contains(t, users, f.user.ID)
	tokenMoney(t, integrationDB, f.user.ID, "7.50000000", "0.00000000")
	var state, kind string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM token_billing_reservations WHERE id=$1`, notSent.ID).Scan(&state))
	require.Equal(t, "released", state)
	require.NoError(t, integrationDB.QueryRow(`SELECT state,settlement_kind FROM token_billing_reservations WHERE id=$1`, sent.ID).Scan(&state, &kind))
	require.Equal(t, "settled", state)
	require.Equal(t, "maximum", kind)
	// A process that recovered its provider receipt after the sweeper still
	// records the exact charge, rather than permanently charging a guess.
	require.NoError(t, f.repo.StageTokenOutcome(ctx, sent.ID, tokenActual(sent, .75)))
	_, err = f.repo.RecoverToken(ctx, 1000)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "8.75000000", "0.00000000")
	var quota, account float64
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, f.key.ID).Scan(&quota))
	require.Equal(t, 1.25, quota)
	require.NoError(t, integrationDB.QueryRow(`SELECT (extra->>'quota_used')::numeric FROM accounts WHERE id=$1`, f.account.ID).Scan(&account))
	require.Equal(t, 1.25, account)
	_, err = f.repo.RecoverToken(ctx, 1000)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "8.75000000", "0.00000000")
	t.Log("crash recovery: undispatched refunded, staged actual settled, missing usage charged maximum; late actual corrected all ledgers once")
}

func TestTokenBillingOverMaximumKeepsDebtAndCollectsRecharge(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, .1)
	r := f.reservation(.03)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, tokenActual(r, .2)))
	result, err := f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, .1, result.OutstandingBalanceCost)
	tokenMoney(t, integrationDB, f.user.ID, "0.00000000", "0.00000000")
	var actual, debt, keyQuota float64
	require.NoError(t, integrationDB.QueryRow(`SELECT actual_cost FROM usage_logs WHERE request_id=$1`, r.ID).Scan(&actual))
	require.Equal(t, .2, actual)
	require.NoError(t, integrationDB.QueryRow(`SELECT outstanding_amount FROM token_billing_reservations WHERE id=$1`, r.ID).Scan(&debt))
	require.Equal(t, .1, debt)
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, f.key.ID).Scan(&keyQuota))
	require.Equal(t, .2, keyQuota)
	require.ErrorIs(t, f.repo.ReserveToken(ctx, f.reservation(.01)), service.ErrInsufficientBalance)
	_, err = integrationDB.Exec(`UPDATE users SET balance=balance+.15 WHERE id=$1`, f.user.ID)
	require.NoError(t, err)
	next := f.reservation(.03)
	require.NoError(t, f.repo.ReserveToken(ctx, next))
	tokenMoney(t, integrationDB, f.user.ID, "0.02000000", "0.03000000")
	require.NoError(t, integrationDB.QueryRow(`SELECT outstanding_amount FROM token_billing_reservations WHERE id=$1`, r.ID).Scan(&debt))
	require.Zero(t, debt)
	_, err = f.repo.ReleaseToken(ctx, next.ID, true)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.05000000", "0.00000000")
	t.Log("over-maximum 0.20 charge: 0.10 paid, 0.10 durable debt; no negative balance; recharge pays debt before new admission")
}

func TestTokenBillingReportedMissingUsageCanBeCorrectedOnce(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 1)
	r := f.reservation(.3)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	maximum := service.TokenBillingMaximumQuote(r.Quote)
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, &maximum))
	_, err := f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.70000000", "0.00000000")
	_, err = f.repo.RecoverToken(ctx, 1000)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.70000000", "0.00000000")
	var inputTokens, outputTokens int
	require.NoError(t, integrationDB.QueryRow(`SELECT input_tokens,output_tokens FROM usage_logs WHERE request_id=$1`, r.ID).Scan(&inputTokens, &outputTokens))
	require.Zero(t, inputTokens)
	require.Zero(t, outputTokens)
	actual := tokenActual(r, .1)
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, actual))
	_, err = f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, actual))
	result, err := f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.ErrorIs(t, f.repo.StageTokenOutcome(ctx, r.ID, tokenActual(r, .2)), service.ErrUsageBillingRequestConflict)
	tokenMoney(t, integrationDB, f.user.ID, "0.90000000", "0.00000000")
}

func TestTokenBillingStrictLegacyAndUnifiedDebitsUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"legacy", "unified"} {
		t.Run(mode, func(t *testing.T) {
			f := tokenFixture(t, .1)
			var accepted atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 32; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var err error
					if mode == "legacy" {
						err = NewUserRepository(testEntClient(t), integrationDB).DeductBalance(ctx, f.user.ID, .03)
					} else {
						r := f.reservation(.03)
						_, err = f.repo.Apply(ctx, &r.Quote.Command)
					}
					if err == nil {
						accepted.Add(1)
					} else {
						require.ErrorIs(t, err, service.ErrInsufficientBalance)
					}
				}()
			}
			wg.Wait()
			require.EqualValues(t, 3, accepted.Load())
			tokenMoney(t, integrationDB, f.user.ID, "0.01000000", "0.00000000")
		})
	}
}

func TestTokenBillingFiniteAmountsAndNumericPrecision(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 1)
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, f.repo.ReserveToken(ctx, f.reservation(amount)))
	}
	r := f.reservation(.000078125)
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
	actual := tokenActual(r, .000000015)
	require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, actual))
	_, err := f.repo.SettleToken(ctx, r.ID)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.99999998", "0.00000000")
	var quota string
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used::text FROM api_keys WHERE id=$1`, f.key.ID).Scan(&quota))
	require.Equal(t, "0.00000002", quota)
}

func TestTokenBillingZeroHoldAndExactZeroSettlement(t *testing.T) {
	ctx := context.Background()
	for _, hold := range []float64{0, .000000001, .3} {
		t.Run(fmt.Sprintf("hold_%.9f", hold), func(t *testing.T) {
			f := tokenFixture(t, 1)
			r := f.reservation(hold)
			require.NoError(t, f.repo.ReserveToken(ctx, r))
			require.NoError(t, f.repo.ReserveToken(ctx, r))
			require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
			actual := tokenActual(r, 0)
			require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, actual))
			result, err := f.repo.SettleToken(ctx, r.ID)
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.False(t, result.BalanceOverdrafted)
			require.Zero(t, result.OutstandingBalanceCost)
			result, err = f.repo.SettleToken(ctx, r.ID)
			require.NoError(t, err)
			require.False(t, result.Applied)
			tokenMoney(t, integrationDB, f.user.ID, "1.00000000", "0.00000000")
			var amount, keyQuota, keyRate float64
			var kind string
			var input, output int
			require.NoError(t, integrationDB.QueryRow(`SELECT settlement_kind FROM token_billing_reservations WHERE id=$1`, r.ID).Scan(&kind))
			require.Equal(t, "actual", kind)
			require.NoError(t, integrationDB.QueryRow(`SELECT actual_cost,input_tokens,output_tokens FROM usage_logs WHERE request_id=$1`, r.ID).Scan(&amount, &input, &output))
			require.Zero(t, amount)
			require.Equal(t, 10, input)
			require.Equal(t, 25, output)
			require.NoError(t, integrationDB.QueryRow(`SELECT quota_used,usage_5h FROM api_keys WHERE id=$1`, f.key.ID).Scan(&keyQuota, &keyRate))
			require.Zero(t, keyQuota)
			require.Zero(t, keyRate)
		})
	}
}

func TestTokenBillingFreeRequestAtZeroBalanceRecoveryAndRefund(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 0)
	undispatched, dispatched := f.reservation(0), f.reservation(0)
	for _, r := range []*service.TokenBillingReservation{undispatched, dispatched} {
		require.NoError(t, f.repo.ReserveToken(ctx, r))
	}
	require.NoError(t, f.repo.DispatchToken(ctx, dispatched.ID, f.user.ID, f.key.ID))
	_, err := integrationDB.Exec(`UPDATE token_billing_reservations SET lease_until=NOW()-INTERVAL '1 minute' WHERE user_id=$1`, f.user.ID)
	require.NoError(t, err)
	_, err = f.repo.RecoverToken(ctx, 1000)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "0.00000000", "0.00000000")
	var state, kind string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM token_billing_reservations WHERE id=$1`, undispatched.ID).Scan(&state))
	require.Equal(t, "released", state)
	require.NoError(t, integrationDB.QueryRow(`SELECT state,settlement_kind FROM token_billing_reservations WHERE id=$1`, dispatched.ID).Scan(&state, &kind))
	require.Equal(t, "settled", state)
	require.Equal(t, "maximum", kind)
	require.NoError(t, f.repo.StageTokenOutcome(ctx, dispatched.ID, tokenActual(dispatched, 0)))
	result, err := f.repo.SettleToken(ctx, dispatched.ID)
	require.NoError(t, err)
	require.True(t, result.Applied)
	tokenMoney(t, integrationDB, f.user.ID, "0.00000000", "0.00000000")
	cancelled := f.reservation(0)
	require.NoError(t, f.repo.ReserveToken(ctx, cancelled))
	released, err := f.repo.ReleaseToken(ctx, cancelled.ID, true)
	require.NoError(t, err)
	require.NotNil(t, released)
	tokenMoney(t, integrationDB, f.user.ID, "0.00000000", "0.00000000")
}

func TestTokenBillingConfiguredFailoverUsesEffectiveGroup(t *testing.T) {
	ctx := context.Background()
	for _, cooldown := range []string{"before_cooldown", "during_cooldown", "after_cooldown"} {
		t.Run(cooldown, func(t *testing.T) {
			f := tokenFixture(t, 1)
			client := testEntClient(t)
			primary := mustCreateGroup(t, client, &service.Group{Name: "token-primary-" + uuid.NewString(), Platform: service.PlatformOpenAI, RateMultiplier: 2})
			fallback := mustCreateGroup(t, client, &service.Group{Name: "token-fallback-" + uuid.NewString(), Platform: service.PlatformOpenAI, RateMultiplier: .5})
			var until any
			if cooldown == "during_cooldown" {
				until = time.Now().Add(time.Hour)
			} else if cooldown == "after_cooldown" {
				until = time.Now().Add(-time.Hour)
			}
			_, err := integrationDB.Exec(`UPDATE api_keys SET group_id=$2,failover_enabled=TRUE,failover_group_id=$3,failover_cooldown_until=$4 WHERE id=$1`, f.key.ID, primary.ID, fallback.ID, until)
			require.NoError(t, err)
			billing := service.NewBillingService(nil, nil)
			cost, err := billing.CalculateCost("gpt-5.4", service.UsageTokens{InputTokens: 10, OutputTokens: 100}, fallback.RateMultiplier)
			require.NoError(t, err)
			primaryCost, err := billing.CalculateCost("gpt-5.4", service.UsageTokens{InputTokens: 10, OutputTokens: 100}, primary.RateMultiplier)
			require.NoError(t, err)
			require.Less(t, cost.ActualCost, primaryCost.ActualCost)
			r := f.reservation(cost.ActualCost)
			r.GroupID = fallback.ID
			r.Quote.Usage.GroupID, r.Quote.Usage.RateMultiplier = &fallback.ID, fallback.RateMultiplier
			require.NoError(t, f.repo.ReserveToken(ctx, r))
			require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
			actual := r.Quote
			require.NoError(t, f.repo.StageTokenOutcome(ctx, r.ID, &actual))
			_, err = f.repo.SettleToken(ctx, r.ID)
			require.NoError(t, err)
			var storedPrimary, reservedGroup, usageGroup int64
			var rate, amount float64
			require.NoError(t, integrationDB.QueryRow(`SELECT group_id FROM api_keys WHERE id=$1`, f.key.ID).Scan(&storedPrimary))
			require.NoError(t, integrationDB.QueryRow(`SELECT group_id FROM token_billing_reservations WHERE id=$1`, r.ID).Scan(&reservedGroup))
			require.NoError(t, integrationDB.QueryRow(`SELECT group_id,rate_multiplier,actual_cost FROM usage_logs WHERE request_id=$1`, r.ID).Scan(&usageGroup, &rate, &amount))
			require.Equal(t, primary.ID, storedPrimary)
			require.Equal(t, fallback.ID, reservedGroup)
			require.Equal(t, fallback.ID, usageGroup)
			require.Equal(t, fallback.RateMultiplier, rate)
			require.Equal(t, service.QuantizeUsageBillingAmount(cost.ActualCost), amount)
			tokenMoney(t, integrationDB, f.user.ID, fmt.Sprintf("%.8f", 1-amount), "0.00000000")
		})
	}
}

func TestTokenBillingRejectsUnconfiguredOrInvalidEffectiveGroup(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"unrelated_group", "disabled_failover", "inactive_group", "deleted_group", "different_platform", "changed_configuration"} {
		t.Run(kind, func(t *testing.T) {
			f := tokenFixture(t, 1)
			client := testEntClient(t)
			primary := mustCreateGroup(t, client, &service.Group{Name: "token-primary-" + uuid.NewString(), Platform: service.PlatformOpenAI})
			fallback := mustCreateGroup(t, client, &service.Group{Name: "token-fallback-" + uuid.NewString(), Platform: service.PlatformOpenAI})
			unrelated := mustCreateGroup(t, client, &service.Group{Name: "token-unrelated-" + uuid.NewString(), Platform: service.PlatformOpenAI})
			_, err := integrationDB.Exec(`UPDATE api_keys SET group_id=$2,failover_enabled=TRUE,failover_group_id=$3 WHERE id=$1`, f.key.ID, primary.ID, fallback.ID)
			require.NoError(t, err)
			r := f.reservation(.1)
			r.GroupID = fallback.ID
			switch kind {
			case "unrelated_group":
				r.GroupID = unrelated.ID
			case "disabled_failover":
				_, err = integrationDB.Exec(`UPDATE api_keys SET failover_enabled=FALSE WHERE id=$1`, f.key.ID)
			case "inactive_group":
				_, err = integrationDB.Exec(`UPDATE groups SET status='disabled' WHERE id=$1`, fallback.ID)
			case "deleted_group":
				_, err = integrationDB.Exec(`UPDATE groups SET deleted_at=NOW() WHERE id=$1`, fallback.ID)
			case "different_platform":
				_, err = integrationDB.Exec(`UPDATE groups SET platform='anthropic' WHERE id=$1`, fallback.ID)
			case "changed_configuration":
				_, err = integrationDB.Exec(`UPDATE api_keys SET failover_group_id=$2 WHERE id=$1`, f.key.ID, unrelated.ID)
			}
			require.NoError(t, err)
			require.ErrorIs(t, f.repo.ReserveToken(ctx, r), service.ErrUsageBillingRequestConflict)
			tokenMoney(t, integrationDB, f.user.ID, "1.00000000", "0.00000000")
		})
	}
}

func TestTokenBillingConfiguredInvalidRequestFallbackAndCompositeGroup(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 1)
	client := testEntClient(t)
	primary := mustCreateGroup(t, client, &service.Group{Name: "token-antigravity-" + uuid.NewString(), Platform: service.PlatformAntigravity})
	fallback := mustCreateGroup(t, client, &service.Group{Name: "token-anthropic-" + uuid.NewString(), Platform: service.PlatformAnthropic})
	_, err := integrationDB.Exec(`UPDATE groups SET fallback_group_id_on_invalid_request=$2 WHERE id=$1`, primary.ID, fallback.ID)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE api_keys SET group_id=$2 WHERE id=$1`, f.key.ID, primary.ID)
	require.NoError(t, err)
	r := f.reservation(.1)
	r.GroupID = fallback.ID
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	_, err = f.repo.ReleaseToken(ctx, r.ID, true)
	require.NoError(t, err)
	composite := mustCreateGroup(t, client, &service.Group{Name: "token-composite-" + uuid.NewString(), Platform: service.PlatformComposite})
	_, err = integrationDB.Exec(`UPDATE api_keys SET group_id=$2 WHERE id=$1`, f.key.ID, composite.ID)
	require.NoError(t, err)
	r = f.reservation(.1)
	r.GroupID = composite.ID
	r.Quote.Command.Platform = service.PlatformOpenAI // routing/quota platform may differ from the billing group's platform.
	require.NoError(t, f.repo.ReserveToken(ctx, r))
	_, err = f.repo.ReleaseToken(ctx, r.ID, true)
	require.NoError(t, err)
	tokenMoney(t, integrationDB, f.user.ID, "1.00000000", "0.00000000")
}

func TestTokenBillingSettlementVersusRefundRace(t *testing.T) {
	ctx := context.Background()
	f := tokenFixture(t, 1)
	for i := 0; i < 20; i++ {
		r := f.reservation(.03)
		require.NoError(t, f.repo.ReserveToken(ctx, r))
		require.NoError(t, f.repo.DispatchToken(ctx, r.ID, f.user.ID, f.key.ID))
		var wg sync.WaitGroup
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(settle bool) {
				defer wg.Done()
				if settle {
					err := f.repo.StageTokenOutcome(ctx, r.ID, tokenActual(r, .01))
					if err == nil {
						_, err = f.repo.SettleToken(ctx, r.ID)
					}
					require.True(t, err == nil || errors.Is(err, service.ErrUsageBillingRequestConflict))
				} else {
					_, err := f.repo.ReleaseToken(ctx, r.ID, false)
					require.True(t, err == nil || errors.Is(err, service.ErrUsageBillingRequestConflict))
				}
			}(j == 0)
		}
		wg.Wait()
	}
	var balance, quota, frozen float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=$1`, f.user.ID).Scan(&balance, &frozen))
	require.NoError(t, integrationDB.QueryRow(`SELECT quota_used FROM api_keys WHERE id=$1`, f.key.ID).Scan(&quota))
	require.Zero(t, frozen)
	require.InDelta(t, 1, balance+quota, 1e-8)
	require.GreaterOrEqual(t, balance, 0.0)
}
