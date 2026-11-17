package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type refreshRotationFailureCache struct {
	RefreshTokenCache
	consumeErr error
	stores     int
	consumes   int
}

func (c *refreshRotationFailureCache) GetRefreshToken(context.Context, string) (*RefreshTokenData, error) {
	return &RefreshTokenData{UserID: 1, TokenVersion: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (c *refreshRotationFailureCache) ConsumeRefreshToken(context.Context, string) (*RefreshTokenData, error) {
	c.consumes++
	return nil, c.consumeErr
}

func (c *refreshRotationFailureCache) DeleteRefreshToken(context.Context, string) error {
	return errors.New("cache write unavailable")
}

func (c *refreshRotationFailureCache) StoreRefreshToken(context.Context, string, *RefreshTokenData, time.Duration) error {
	c.stores++
	return nil
}

func (c *refreshRotationFailureCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}
func (c *refreshRotationFailureCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func TestRefreshTokenPairFailsClosedWhenTokenCannotBeConsumed(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"cache_failure", errors.New("cache write unavailable"), ErrServiceUnavailable},
		{"already_consumed", ErrRefreshTokenNotFound, ErrRefreshTokenInvalid},
		{"missing_data", nil, ErrRefreshTokenInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &refreshRotationFailureCache{consumeErr: tc.err}
			repo := &tokenRevocationUserRepo{user: User{ID: 1, TokenVersion: 1, TokenVersionResolved: true, Status: StatusActive}}
			svc := &AuthService{userRepo: repo, refreshTokenCache: cache, cfg: &config.Config{
				JWT: config.JWTConfig{Secret: "rotation-test-secret", ExpireHour: 1, RefreshTokenExpireDays: 1},
			}}
			pair, err := svc.RefreshTokenPair(context.Background(), refreshTokenPrefix+"fixture")
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, pair)
			require.Equal(t, 1, cache.consumes)
			require.Zero(t, cache.stores, "failure must never mint a replacement credential")
		})
	}
}
