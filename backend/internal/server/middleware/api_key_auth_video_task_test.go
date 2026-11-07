package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoTaskReadSkipsNewGenerationBalanceCheck(t *testing.T) {
	for _, path := range []string{"/v1/videos/id", "/v1/videos/id/content", "/videos/id", "/v1/sora/videos/id", "/v1/sora/videos/id/content", "/sora/videos/id", "/api/v3/contents/generations/tasks/id"} {
		require.True(t, isAsyncVideoTaskRead(http.MethodGet, path), path)
		require.False(t, isAsyncVideoTaskRead(http.MethodPost, path), path)
	}
	for _, path := range []string{"/v1/videos", "/v1/videos/generations", "/v1/videos/edits", "/v1/videos/extensions", "/v1/sora/videos", "/sora/videos"} {
		require.False(t, isAsyncVideoTaskRead(http.MethodGet, path), path)
	}
}
