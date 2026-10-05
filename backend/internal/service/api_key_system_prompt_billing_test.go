//go:build unit

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomSystemPrompt_BillingUsesUpstreamUsage(t *testing.T) {
	for _, protocol := range []string{"responses", "compact", "anthropic"} {
		for _, force := range []bool{false, true} {
			for _, subscription := range []bool{false, true} {
				t.Run(protocol+"/force="+boolName(force)+"/subscription="+boolName(subscription), func(t *testing.T) {
					usageRepo := &openAIRecordUsageLogRepoStub{}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					userRepo := &openAIRecordUsageUserRepoStub{}
					subRepo := &openAIRecordUsageSubRepoStub{}
					quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
					key := &APIKey{
						ID: 51, UserID: 61, GroupID: i64p(81), Quota: 100, RateLimit5h: 20,
						Group:                     &Group{ID: 81, RateMultiplier: 1.25},
						CustomSystemPromptEnabled: true,
						CustomSystemPromptForce:   force,
						CustomSystemPrompt:        strings.Repeat("Configured prompt. ", 100),
					}
					var sub *UserSubscription
					if subscription {
						key.Group.SubscriptionType = SubscriptionTypeSubscription
						sub = &UserSubscription{ID: 91}
					}
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					payloadHash := HashUsageRequestPayload([]byte(`{"input":"original request"}`))
					if protocol == "anthropic" {
						svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo)
						err := svc.RecordUsage(ctx, &RecordUsageInput{
							Result: &ForwardResult{
								RequestID: "prompt-billing", Model: "claude-sonnet-4", Stream: true,
								ClientDisconnect: true, Duration: time.Second,
								Usage: ClaudeUsage{InputTokens: 1234, OutputTokens: 56, CacheReadInputTokens: 100, CacheCreationInputTokens: 200},
							},
							APIKey: key, User: &User{ID: 61}, Account: &Account{ID: 71},
							Subscription: sub, RequestPayloadHash: payloadHash, APIKeyService: quotaSvc,
						})
						require.NoError(t, err)
						require.Equal(t, 1234, usageRepo.lastLog.InputTokens)
					} else {
						svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, userRepo, subRepo, nil)
						err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
							Result: &OpenAIForwardResult{
								RequestID: "prompt-billing", Model: "gpt-5.6-sol", Stream: protocol == "responses",
								Duration: time.Second,
								Usage:    OpenAIUsage{InputTokens: 1234, OutputTokens: 56, CacheReadInputTokens: 100, CacheCreationInputTokens: 200},
							},
							APIKey: key, User: &User{ID: 61}, Account: &Account{ID: 71},
							Subscription: sub, RequestPayloadHash: payloadHash, APIKeyService: quotaSvc,
							InboundEndpoint: "/v1/responses" + compactSuffix(protocol),
						})
						require.NoError(t, err)
						require.Equal(t, 934, usageRepo.lastLog.InputTokens)
					}

					require.Equal(t, 1, billingRepo.calls)
					require.NoError(t, billingRepo.lastCtxErr)
					require.Equal(t, 1, usageRepo.calls)
					require.Equal(t, 56, usageRepo.lastLog.OutputTokens)
					require.Equal(t, 100, usageRepo.lastLog.CacheReadTokens)
					require.Equal(t, 200, usageRepo.lastLog.CacheCreationTokens)
					require.Equal(t, 1.25, usageRepo.lastLog.RateMultiplier)
					require.Equal(t, i64p(81), usageRepo.lastLog.GroupID)
					cmd := billingRepo.lastCmd
					require.Equal(t, int64(51), cmd.APIKeyID)
					require.Equal(t, int64(61), cmd.UserID)
					require.Equal(t, int64(71), cmd.AccountID)
					require.Equal(t, payloadHash, cmd.RequestPayloadHash)
					require.Equal(t, usageRepo.lastLog.InputTokens, cmd.InputTokens)
					require.Equal(t, 56, cmd.OutputTokens)
					require.Equal(t, 100, cmd.CacheReadTokens)
					require.Equal(t, 200, cmd.CacheCreationTokens)
					require.Greater(t, usageRepo.lastLog.ActualCost, 0.0)
					expectedTotal := 934*4e-6 + 56*20e-6 + 100*0.4e-6 + 200*5e-6
					if protocol == "anthropic" {
						expectedTotal = 1234*3e-6 + 56*15e-6 + 100*0.3e-6 + 200*3.75e-6
					}
					require.InDelta(t, expectedTotal, usageRepo.lastLog.TotalCost, 1e-12)
					expectedCharge := QuantizeUsageBillingAmount(expectedTotal * 1.25)
					require.Equal(t, expectedCharge, cmd.APIKeyQuotaCost)
					require.Equal(t, cmd.APIKeyQuotaCost, cmd.APIKeyRateLimitCost)
					if subscription {
						require.Zero(t, cmd.BalanceCost)
						require.Equal(t, expectedCharge, cmd.SubscriptionCost)
						require.Equal(t, i64p(91), cmd.SubscriptionID)
					} else {
						require.Zero(t, cmd.SubscriptionCost)
						require.Equal(t, expectedCharge, cmd.BalanceCost)
					}
				})
			}
		}
	}
}

func boolName(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func compactSuffix(protocol string) string {
	if protocol == "compact" {
		return "/compact"
	}
	return ""
}
