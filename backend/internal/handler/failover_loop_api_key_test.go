package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverAttemptOwnsRetryBudget(t *testing.T) {
	ctx := service.WithAPIKeyFailoverAttempt(t.Context())
	fs := NewFailoverState(10, true)
	upstreamError := newTestFailoverErr(http.StatusServiceUnavailable, true, false)
	unscheduler := &mockTempUnscheduler{}
	require.Equal(t, FailoverExhausted, fs.HandleFailoverError(ctx, unscheduler, 1, service.PlatformAnthropic, 10, upstreamError))
	require.Same(t, upstreamError, fs.LastFailoverErr)
	require.Empty(t, fs.SameAccountRetryCount)
	require.Zero(t, fs.SwitchCount)
	require.Empty(t, unscheduler.calls)
	require.Equal(t, FailoverExhausted, fs.HandleSelectionExhausted(ctx), "the outer middleware retries without account-level backoff")
}
