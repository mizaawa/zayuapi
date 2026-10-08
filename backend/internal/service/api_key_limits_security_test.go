//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyServiceRejectsInvalidLimits(t *testing.T) {
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, field := range []string{"quota", "5h", "1d", "7d"} {
			create := CreateAPIKeyRequest{}
			update := UpdateAPIKeyRequest{}
			switch field {
			case "quota":
				create.Quota, update.Quota = invalid, &invalid
			case "5h":
				create.RateLimit5h, update.RateLimit5h = invalid, &invalid
			case "1d":
				create.RateLimit1d, update.RateLimit1d = invalid, &invalid
			case "7d":
				create.RateLimit7d, update.RateLimit7d = invalid, &invalid
			}
			svc := &APIKeyService{}
			_, err := svc.Create(context.Background(), 7, create)
			require.ErrorIs(t, err, ErrAPIKeyInvalidLimit, "create %s=%v", field, invalid)
			_, err = svc.Update(context.Background(), 1, 7, update)
			require.ErrorIs(t, err, ErrAPIKeyInvalidLimit, "update %s=%v", field, invalid)
		}
	}
}

func TestAPIKeyServiceRejectsInvalidExpiryDays(t *testing.T) {
	for _, days := range []int{-1, MaxValidityDays + 1, math.MaxInt} {
		svc := &APIKeyService{}
		_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{ExpiresInDays: &days})
		require.ErrorIs(t, err, ErrAPIKeyInvalidExpiry)
	}
}

func TestAPIKeyServiceZeroLimitsRemainUnlimited(t *testing.T) {
	zero := 0.0
	svc, _ := newUpdateFieldsAPIKeyService(&APIKey{ID: 1, UserID: 7, Key: "key", Status: StatusActive, Quota: 5, RateLimit5h: 2})
	key, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Quota: &zero, RateLimit5h: &zero})
	require.NoError(t, err)
	require.Zero(t, key.Quota)
	require.Zero(t, key.RateLimit5h)
}

func TestAPIKeyStatusUpdatePreservesConflictThrottle(t *testing.T) {
	svc, _ := newUpdateFieldsAPIKeyService(&APIKey{ID: 1, UserID: 7, Key: "key", Status: StatusActive})
	cache := &apiKeyCacheStub{}
	svc.cache = cache
	status := StatusDisabled
	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Status: &status})
	require.NoError(t, err)
	require.Empty(t, cache.invalidated)
	require.Equal(t, []string{svc.authCacheKey("key")}, cache.deleteAuthKeys)
}
