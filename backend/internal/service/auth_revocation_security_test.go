package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type tokenRevocationUserRepo struct {
	UserRepository
	user User
	err  error
}

func (r *tokenRevocationUserRepo) GetByID(context.Context, int64) (*User, error) {
	u := r.user
	return &u, nil
}

func (r *tokenRevocationUserRepo) IncrementTokenVersion(context.Context, int64) error {
	if r.err != nil {
		return r.err
	}
	r.user.TokenVersion++
	return nil
}

type tokenRevocationRefreshCache struct {
	RefreshTokenCache
	err error
}

func (c tokenRevocationRefreshCache) DeleteUserRefreshTokens(context.Context, int64) error {
	return c.err
}

func TestRevokeAllUserTokensPersistsInvalidationWhenRefreshCacheFails(t *testing.T) {
	repo := &tokenRevocationUserRepo{user: User{ID: 1, Email: "user@example.com", PasswordHash: "hash", Status: StatusActive}}
	svc := &AuthService{
		userRepo:          repo,
		refreshTokenCache: tokenRevocationRefreshCache{err: errors.New("cache unavailable")},
		cfg:               &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}},
	}
	oldToken, err := svc.GenerateToken(context.Background(), &repo.user)
	require.NoError(t, err)
	require.NoError(t, svc.RevokeAllUserTokens(context.Background(), 1))
	require.Equal(t, int64(1), repo.user.TokenVersion)
	_, err = svc.RefreshToken(context.Background(), oldToken)
	require.ErrorIs(t, err, ErrTokenRevoked)
	newToken, err := svc.GenerateToken(context.Background(), &repo.user)
	require.NoError(t, err)
	_, err = svc.RefreshToken(context.Background(), newToken)
	require.NoError(t, err)
}

func TestRevokeAllUserTokensReportsPersistenceFailure(t *testing.T) {
	errDB := errors.New("database unavailable")
	svc := &AuthService{userRepo: &tokenRevocationUserRepo{err: errDB}}
	require.ErrorIs(t, svc.RevokeAllUserTokens(context.Background(), 1), errDB)
}
