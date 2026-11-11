//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// geminiQuotaUsageRepoMustNotBeCalled embeds a nil UsageLogRepository: any
// call into it panics, which proves that API-key accounts skip the simulated
// quota lookups entirely.
type geminiQuotaUsageRepoMustNotBeCalled struct {
	UsageLogRepository
}

func geminiRelayAPIKeyAccount(id int64, tierID string) *Account {
	credentials := map[string]any{"api_key": "relay-key", "base_url": "https://relay.example"}
	if tierID != "" {
		credentials["tier_id"] = tierID
	}
	return &Account{ID: id, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: credentials}
}

func TestGeminiQuotaTierKeyForAccount_APIKeyHasNoTier(t *testing.T) {
	t.Parallel()

	quotaSvc := NewGeminiQuotaService(&config.Config{}, nil)
	for _, tierID := range []string{"", GeminiTierAIStudioFree, GeminiTierAIStudioPaid, GeminiTierGoogleAIPro} {
		account := geminiRelayAPIKeyAccount(1, tierID)
		require.Empty(t, geminiQuotaTierKeyForAccount(account), tierID)
		_, ok := quotaSvc.QuotaForAccount(context.Background(), account)
		require.False(t, ok, tierID)
	}
}

func TestGeminiQuotaTierKeyForAccount_OAuthTiersUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		credentials map[string]any
		want        string
	}{
		{name: "google one without tier", credentials: map[string]any{"oauth_type": "google_one"}, want: GeminiTierGoogleOneFree},
		{name: "legacy code assist", credentials: map[string]any{"project_id": "p-1"}, want: GeminiTierGCPStandard},
		{name: "ai studio oauth without tier", credentials: map[string]any{"oauth_type": "ai_studio"}, want: GeminiTierAIStudioFree},
		{name: "ai studio oauth paid", credentials: map[string]any{"oauth_type": "ai_studio", "tier_id": GeminiTierAIStudioPaid}, want: GeminiTierAIStudioPaid},
	}
	for _, tc := range cases {
		account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: tc.credentials}
		require.Equal(t, tc.want, geminiQuotaTierKeyForAccount(account), tc.name)
	}
}

func TestRateLimitService_PreCheckUsage_GeminiAPIKeySkipsLocalQuota(t *testing.T) {
	t.Parallel()

	quotaSvc := NewGeminiQuotaService(&config.Config{}, nil)
	svc := NewRateLimitService(nil, geminiQuotaUsageRepoMustNotBeCalled{}, &config.Config{}, quotaSvc, nil)
	account := geminiRelayAPIKeyAccount(7, GeminiTierAIStudioFree)

	ok, err := svc.PreCheckUsage(context.Background(), account, "gemini-2.5-pro")
	require.NoError(t, err)
	require.True(t, ok)

	result, err := svc.PreCheckUsageBatch(context.Background(), []*Account{account}, "gemini-2.5-pro")
	require.NoError(t, err)
	require.Equal(t, map[int64]bool{7: true}, result)
}

func TestAccountUsageService_GeminiAPIKeyHasNoSimulatedWindows(t *testing.T) {
	t.Parallel()

	svc := &AccountUsageService{
		usageLogRepo:       geminiQuotaUsageRepoMustNotBeCalled{},
		geminiQuotaService: NewGeminiQuotaService(&config.Config{}, nil),
	}
	usage, err := svc.getGeminiUsage(context.Background(), geminiRelayAPIKeyAccount(8, GeminiTierAIStudioFree))
	require.NoError(t, err)
	require.Nil(t, usage.GeminiProDaily)
	require.Nil(t, usage.GeminiFlashDaily)
	require.Nil(t, usage.GeminiSharedDaily)
	require.Nil(t, usage.GeminiProMinute)
	require.Nil(t, usage.GeminiFlashMinute)
	require.Nil(t, usage.GeminiSharedMinute)
}

func TestHandleGeminiUpstreamError_APIKey429UsesShortFallbackCooldown(t *testing.T) {
	repo := &rateLimit429AccountRepoStub{}
	rlSvc := NewRateLimitService(repo, nil, &config.Config{}, NewGeminiQuotaService(&config.Config{}, nil), nil)
	svc := &GeminiMessagesCompatService{accountRepo: repo, rateLimitService: rlSvc}

	before := time.Now()
	svc.handleGeminiUpstreamError(context.Background(), geminiRelayAPIKeyAccount(512, GeminiTierAIStudioFree), http.StatusTooManyRequests, http.Header{},
		[]byte(`{"error":{"code":429,"message":"concurrency limit reached for this token","status":"RESOURCE_EXHAUSTED"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(512), repo.lastRateLimitID)
	require.WithinDuration(t, before.Add(defaultRateLimit429CooldownSeconds*time.Second), repo.lastRateLimitReset, 2*time.Second)
}

func TestHandleGeminiUpstreamError_APIKeyDailyQuota429StillWaitsForReset(t *testing.T) {
	repo := &rateLimit429AccountRepoStub{}
	rlSvc := NewRateLimitService(repo, nil, &config.Config{}, NewGeminiQuotaService(&config.Config{}, nil), nil)
	svc := &GeminiMessagesCompatService{accountRepo: repo, rateLimitService: rlSvc}

	svc.handleGeminiUpstreamError(context.Background(), geminiRelayAPIKeyAccount(513, ""), http.StatusTooManyRequests, http.Header{},
		[]byte(`{"error":{"code":429,"message":"You exceeded your current quota: requests per day limit","status":"RESOURCE_EXHAUSTED"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.WithinDuration(t, geminiDailyResetTime(time.Now()), repo.lastRateLimitReset, 2*time.Second)
}
