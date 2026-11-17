package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestTOTPAttemptWindowDoesNotExtendOnRejectedRequests(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewTotpCache(client)
	ctx := context.Background()
	count, err := cache.IncrementVerifyAttempts(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	server.FastForward(14 * time.Minute)
	for i := 2; i <= 12; i++ {
		count, err = cache.IncrementVerifyAttempts(ctx, 7)
		require.NoError(t, err)
		require.Equal(t, i, count)
	}
	require.Equal(t, time.Minute, server.TTL("totp:attempts:7"))
	server.FastForward(time.Minute)
	count, err = cache.IncrementVerifyAttempts(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, 15*time.Minute, server.TTL("totp:attempts:7"))
}
