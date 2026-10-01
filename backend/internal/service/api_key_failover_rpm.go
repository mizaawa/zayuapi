package service

import (
	"context"
	"sync"
)

type apiKeyFailoverRequestContextKey struct{}
type apiKeyFailoverRPMResult struct {
	count int
	err   error
}
type apiKeyFailoverRequestState struct {
	mu  sync.Mutex
	rpm map[[2]int64]apiKeyFailoverRPMResult
}

// Retries share one logical request. Count user RPM once, and group RPM once
// for each group actually visited, preserving the original counter result.
func WithAPIKeyFailoverRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, apiKeyFailoverRequestContextKey{}, &apiKeyFailoverRequestState{rpm: make(map[[2]int64]apiKeyFailoverRPMResult)})
}

func incrementAPIKeyFailoverRPM(ctx context.Context, userID, groupID int64, increment func() (int, error)) (int, error) {
	state, _ := ctx.Value(apiKeyFailoverRequestContextKey{}).(*apiKeyFailoverRequestState)
	if state == nil {
		return increment()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	key := [2]int64{userID, groupID}
	if result, ok := state.rpm[key]; ok {
		return result.count, result.err
	}
	count, err := increment()
	state.rpm[key] = apiKeyFailoverRPMResult{count: count, err: err}
	return count, err
}

func (s *BillingCacheService) incrementFailoverUserGroupRPM(ctx context.Context, userID, groupID int64) (int, error) {
	return incrementAPIKeyFailoverRPM(ctx, userID, groupID, func() (int, error) { return s.userRPMCache.IncrementUserGroupRPM(ctx, userID, groupID) })
}

func (s *BillingCacheService) incrementFailoverUserRPM(ctx context.Context, userID int64) (int, error) {
	return incrementAPIKeyFailoverRPM(ctx, userID, 0, func() (int, error) { return s.userRPMCache.IncrementUserRPM(ctx, userID) })
}
