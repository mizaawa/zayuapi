//go:build integration

package testutil

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// ExternalRedis uses the disposable integration database when one is configured.
// Refuse to overwrite an existing key if the URL does not point to a fresh DB.
func ExternalRedis(t *testing.T, ctx context.Context, key string) *redis.Client {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("SUB2API_TEST_REDIS_URL"))
	if url == "" {
		return nil
	}
	opts, err := redis.ParseURL(url)
	require.NoError(t, err)
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	existing, err := client.Exists(ctx, key).Result()
	require.NoError(t, err)
	require.Zero(t, existing, "external test Redis already contains %q", key)
	t.Cleanup(func() { require.NoError(t, client.Del(ctx, key).Err()) })
	return client
}
