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

type runtimeBlockRecorder struct {
	accounts   []*Account
	until      []time.Time
	reasons    []string
	clearedIDs []int64
}

func (r *runtimeBlockRecorder) BlockAccountScheduling(account *Account, until time.Time, reason string) {
	r.accounts = append(r.accounts, account)
	r.until = append(r.until, until)
	r.reasons = append(r.reasons, reason)
}

func (r *runtimeBlockRecorder) ClearAccountSchedulingBlock(accountID int64) {
	r.clearedIDs = append(r.clearedIDs, accountID)
}

func TestRateLimitService_HandleUpstreamError_OpenAI403FirstHitTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{1}}
	blocker := &runtimeBlockRecorder{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	service.SetAccountRuntimeBlocker(blocker)
	account := &Account{
		ID:       301,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary edge rejection"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "temporary edge rejection")
	require.Contains(t, repo.lastTempReason, "(1/3)")
	require.Len(t, blocker.accounts, 1)
	require.Equal(t, account.ID, blocker.accounts[0].ID)
	require.Equal(t, "openai_403_temp", blocker.reasons[0])
	require.True(t, blocker.until[0].After(time.Now()))
}

func TestRateLimitService_HandleUpstreamError_OpenAI403ThresholdDisables(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{3}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       302,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"workspace forbidden by policy"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Contains(t, repo.lastErrorMsg, "workspace forbidden by policy")
	require.Contains(t, repo.lastErrorMsg, "consecutive_403=3/3")
}

func TestRateLimitService_HandleUpstreamError_APIKey403KeepsAccountActiveWhenTransientBlocksDisabled(t *testing.T) {
	previous := IsDisableTempUnschedulableEnabled()
	SetDisableTempUnschedulableRuntime(true)
	t.Cleanup(func() { SetDisableTempUnschedulableRuntime(previous) })

	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 303, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	account.Credentials = map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(403)}}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary edge rejection"}}`))

	require.True(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls, "an API Key 403 without a counter must not persist status=error")
	require.Zero(t, repo.tempCalls)
	require.True(t, account.IsSchedulable())
}

func TestRateLimitService_HandleUpstreamError_APIKey403ThresholdKeepsAccountActiveWhenTransientBlocksDisabled(t *testing.T) {
	previous := IsDisableTempUnschedulableEnabled()
	SetDisableTempUnschedulableRuntime(true)
	t.Cleanup(func() { SetDisableTempUnschedulableRuntime(previous) })

	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{openAI403DisableThreshold}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{ID: 304, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	account.Credentials = map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(403)}}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary edge rejection"}}`))

	require.True(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls, "the transient-block switch must prevent a generic 403 threshold from poisoning an API Key")
	require.True(t, account.IsSchedulable())
}

func TestRateLimitService_HandleUpstreamError_NonOpenAIAPIKey403KeepsAccountActiveWhenTransientBlocksDisabled(t *testing.T) {
	previous := IsDisableTempUnschedulableEnabled()
	SetDisableTempUnschedulableRuntime(true)
	t.Cleanup(func() { SetDisableTempUnschedulableRuntime(previous) })

	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 305, Platform: PlatformGemini, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	account.Credentials = map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(403)}}

	shouldDisable := service.HandleUpstreamError(context.Background(), account, http.StatusForbidden, http.Header{}, []byte(`{"error":{"message":"temporary permission edge"}}`))

	require.True(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls)
	require.True(t, account.IsSchedulable())
}
