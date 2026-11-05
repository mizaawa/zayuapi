package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromService_MapsLastUsedAt(t *testing.T) {
	lastUsed := time.Now().UTC().Truncate(time.Second)
	lastUsedIP := "203.0.113.10"
	src := &service.APIKey{
		ID:                 1,
		UserID:             2,
		Key:                "sk-map-last-used",
		Name:               "Mapper",
		Status:             service.StatusActive,
		LastUsedAt:         &lastUsed,
		LastUsedIP:         &lastUsedIP,
		CurrentConcurrency: 3,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.NotNil(t, out.LastUsedAt)
	require.WithinDuration(t, lastUsed, *out.LastUsedAt, time.Second)
	require.NotNil(t, out.LastUsedIP)
	require.Equal(t, lastUsedIP, *out.LastUsedIP)
	require.Equal(t, 3, out.CurrentConcurrency)
	require.Equal(t, "sk-map...used", out.Key, "user-facing reads must expose only a masked API key")
}

func TestAPIKeyFromServiceMasksSecret(t *testing.T) {
	const secret = "sk-0123456789abcdef0123456789abcdef"
	out := APIKeyFromService(&service.APIKey{Key: secret})
	require.Equal(t, "sk-012...cdef", out.Key)
	require.NotContains(t, out.Key, secret)
}

func TestAPIKeyFromService_MapsNilLastUsedAt(t *testing.T) {
	src := &service.APIKey{
		ID:     1,
		UserID: 2,
		Key:    "sk-map-last-used-nil",
		Name:   "MapperNil",
		Status: service.StatusActive,
	}

	out := APIKeyFromService(src)
	require.NotNil(t, out)
	require.Nil(t, out.LastUsedAt)
	require.Nil(t, out.LastUsedIP)
}
