package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type refreshRotationUserRepo struct {
	service.UserRepository
	user service.User
}

func (r *refreshRotationUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	user := r.user
	return &user, nil
}

func TestRefreshTokenRotationAllowsExactlyOneConcurrentConsumer(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRefreshTokenCache(client)
	userRepo := &refreshRotationUserRepo{user: service.User{
		ID: 7, Email: "admin@example.com", Role: service.RoleAdmin, Status: service.StatusActive, TokenVersion: 1,
	}}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "refresh-rotation-test-secret", ExpireHour: 1, RefreshTokenExpireDays: 1}}
	svc := service.NewAuthService(nil, userRepo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	pair, err := svc.GenerateTokenPair(context.Background(), &userRepo.user, "")
	require.NoError(t, err)

	const contenders = 24
	start := make(chan struct{})
	results := make(chan error, contenders)
	replacements := make(chan *service.TokenPairWithUser, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := svc.RefreshTokenPair(context.Background(), pair.RefreshToken)
			results <- err
			if result != nil {
				replacements <- result
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(replacements)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
		}
	}
	require.Equal(t, 1, successes)
	require.Len(t, replacements, 1)
	replacement := <-replacements
	require.NotEqual(t, pair.RefreshToken, replacement.RefreshToken)
	_, err = svc.RefreshTokenPair(context.Background(), replacement.RefreshToken)
	require.NoError(t, err, "the sole replacement remains usable")
}

func TestConsumeRefreshTokenNeverReturnsDataOnFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewRefreshTokenCache(client)
	ctx := context.Background()
	data, err := cache.ConsumeRefreshToken(ctx, "missing")
	require.Nil(t, data)
	require.ErrorIs(t, err, service.ErrRefreshTokenNotFound)

	require.NoError(t, client.Set(ctx, refreshTokenKey("malformed"), "not-json", time.Hour).Err())
	data, err = cache.ConsumeRefreshToken(ctx, "malformed")
	require.Nil(t, data)
	require.Error(t, err)
	require.False(t, server.Exists(refreshTokenKey("malformed")))

	require.NoError(t, cache.StoreRefreshToken(ctx, "cache-error", &service.RefreshTokenData{UserID: 7}, time.Hour))
	server.SetError("ERR simulated failure")
	data, err = cache.ConsumeRefreshToken(ctx, "cache-error")
	require.Nil(t, data)
	require.Error(t, err)
	require.False(t, errors.Is(err, service.ErrRefreshTokenNotFound))
}
