package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverRPMCountsLogicalRequestOncePerGroup(t *testing.T) {
	ctx := WithAPIKeyFailoverRequest(context.Background())
	calls := make(map[int64]int)
	for _, groupID := range []int64{1, 1, 1, 2, 2, 2, 0, 0} {
		count, err := incrementAPIKeyFailoverRPM(ctx, 10, groupID, func() (int, error) { calls[groupID]++; return 7, nil })
		require.NoError(t, err)
		require.Equal(t, 7, count)
	}
	require.Equal(t, map[int64]int{0: 1, 1: 1, 2: 1}, calls)
	_, err := incrementAPIKeyFailoverRPM(WithAPIKeyFailoverRequest(context.Background()), 10, 1, func() (int, error) { calls[1]++; return 8, nil })
	require.NoError(t, err)
	require.Equal(t, 2, calls[1])
}

func TestAPIKeyFailoverRPMKeepsNormalRequestsIndependent(t *testing.T) {
	calls := 0
	for range 3 {
		_, err := incrementAPIKeyFailoverRPM(context.Background(), 10, 1, func() (int, error) { calls++; return calls, nil })
		require.NoError(t, err)
	}
	require.Equal(t, 3, calls)
}
