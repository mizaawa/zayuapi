package service

import (
	"context"
	"math"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func cyberAutoChargeAPIKey(group *Group) *APIKey {
	return &APIKey{ID: 2, User: &User{ID: 1}, GroupID: &group.ID, Group: group}
}

func cyberAutoChargeGroup(enabled bool, amount float64) *Group {
	return &Group{
		ID:                     7,
		Platform:               PlatformOpenAI,
		RateMultiplier:         1,
		SubscriptionType:       SubscriptionTypeStandard,
		CyberAutoChargeEnabled: enabled,
		CyberAutoChargeAmount:  amount,
	}
}

func TestGroupCyberAutoCharge(t *testing.T) {
	require.Zero(t, (*Group)(nil).CyberAutoCharge())
	require.Zero(t, cyberAutoChargeGroup(false, 0.5).CyberAutoCharge(), "disabled charge must not apply")
	require.Zero(t, cyberAutoChargeGroup(true, 0).CyberAutoCharge())
	require.Zero(t, cyberAutoChargeGroup(true, -1).CyberAutoCharge())
	require.Zero(t, cyberAutoChargeGroup(true, math.NaN()).CyberAutoCharge())
	require.Zero(t, cyberAutoChargeGroup(true, math.Inf(1)).CyberAutoCharge())
	require.Equal(t, 0.5, cyberAutoChargeGroup(true, 0.5).CyberAutoCharge())
}

func TestValidateCyberAutoChargeConfig(t *testing.T) {
	require.NoError(t, ValidateCyberAutoChargeConfig(false, 0))
	require.NoError(t, ValidateCyberAutoChargeConfig(false, 0.5), "a kept amount may stay while the charge is off")
	require.NoError(t, ValidateCyberAutoChargeConfig(true, 0.5))
	for _, tc := range []struct {
		name    string
		enabled bool
		amount  float64
	}{
		{"enabled without amount", true, 0},
		{"negative", false, -0.1},
		{"NaN", true, math.NaN()},
		{"infinite", true, math.Inf(1)},
	} {
		err := ValidateCyberAutoChargeConfig(tc.enabled, tc.amount)
		require.Error(t, err, tc.name)
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err), tc.name)
	}
}

func TestRecordCyberPolicyUsageLog_AddsGroupCyberAutoCharge(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	// A non-stream cyber rejection reports no tokens, so the fixed charge is the whole bill.
	svc.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{
		APIKey:    cyberAutoChargeAPIKey(cyberAutoChargeGroup(true, 0.5)),
		Account:   &Account{ID: 3},
		RequestID: "rid-cyber-charge",
		Model:     "gpt-5.1",
	})

	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, RequestTypeCyberBlocked, usageRepo.lastLog.RequestType)
	require.Zero(t, usageRepo.lastLog.TotalCost, "the charge has no upstream cost")
	require.InDelta(t, 0.5, usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, billingRepo.calls, "one deduction per cyber hit")
	require.InDelta(t, 0.5, billingRepo.lastCmd.BalanceCost, 1e-12)
	require.Zero(t, billingRepo.lastCmd.AccountQuotaCost, "the upstream account consumed nothing")
}

func TestRecordCyberPolicyUsageLog_CyberAutoChargeAddsToTokenCost(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	svc.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{
		APIKey:       cyberAutoChargeAPIKey(cyberAutoChargeGroup(true, 0.5)),
		Account:      &Account{ID: 3},
		RequestID:    "rid-cyber-charge-stream",
		Model:        "gpt-5.1",
		Stream:       true,
		InputTokens:  1200,
		OutputTokens: 300,
	})

	expected := expectedOpenAICost(t, svc, "gpt-5.1", OpenAIUsage{InputTokens: 1200, OutputTokens: 300}, 1)
	require.Greater(t, expected.ActualCost, 0.0)
	require.InDelta(t, expected.TotalCost, usageRepo.lastLog.TotalCost, 1e-12, "token cost is unchanged")
	require.InDelta(t, expected.ActualCost+0.5, usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, expected.ActualCost+0.5, billingRepo.lastCmd.BalanceCost, 1e-12)
}

func TestRecordCyberPolicyUsageLog_CyberAutoChargeSkipped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		apiKey *APIKey
	}{
		{"charge disabled", cyberAutoChargeAPIKey(cyberAutoChargeGroup(false, 0.5))},
		{"channel monitor probe", func() *APIKey {
			key := cyberAutoChargeAPIKey(cyberAutoChargeGroup(true, 0.5))
			key.Purpose = APIKeyPurposeChannelMonitor
			return key
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

			svc.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{
				APIKey:    tc.apiKey,
				Account:   &Account{ID: 3},
				RequestID: "rid-cyber-no-charge",
				Model:     "gpt-5.1",
			})

			require.Equal(t, 1, usageRepo.calls)
			require.Zero(t, usageRepo.lastLog.ActualCost)
			require.Zero(t, billingRepo.lastCmd.BalanceCost)
		})
	}
}

func TestOpenAIRecordUsage_NoCyberAutoChargeWithoutCyberHit(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	apiKey := cyberAutoChargeAPIKey(cyberAutoChargeGroup(true, 0.5))
	usage := OpenAIUsage{InputTokens: 1200, OutputTokens: 300}

	require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result:  &OpenAIForwardResult{RequestID: "rid-normal", Model: "gpt-5.1", Usage: usage},
		APIKey:  apiKey,
		User:    apiKey.User,
		Account: &Account{ID: 3},
	}))

	expected := expectedOpenAICost(t, svc, "gpt-5.1", usage, 1)
	require.InDelta(t, expected.ActualCost, usageRepo.lastLog.ActualCost, 1e-12, "normal requests are never charged the cyber amount")
}

func TestRecordCyberPolicyUsageLog_CyberAutoChargeBillsSubscription(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	group := cyberAutoChargeGroup(true, 0.5)
	group.SubscriptionType = SubscriptionTypeSubscription

	svc.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{
		APIKey:       cyberAutoChargeAPIKey(group),
		Account:      &Account{ID: 3},
		Subscription: &UserSubscription{ID: 9},
		RequestID:    "rid-cyber-charge-sub",
		Model:        "gpt-5.1",
	})

	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd.SubscriptionID)
	require.Equal(t, int64(9), *billingRepo.lastCmd.SubscriptionID)
	require.InDelta(t, 0.5, billingRepo.lastCmd.SubscriptionCost, 1e-12, "subscription users pay the charge from their quota")
	require.Zero(t, billingRepo.lastCmd.BalanceCost, "never from the balance of a subscription user")
}
