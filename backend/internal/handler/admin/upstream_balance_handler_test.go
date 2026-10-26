package admin

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountUpstreamBalanceDistinguishesZeroFromUnknown(t *testing.T) {
	zero := accountUpstreamBalance(&service.Account{ID: 3, Extra: map[string]any{
		upstreamBalanceExtraKey:          float64(0),
		upstreamBalanceUpdatedAtExtraKey: "2026-10-27T00:00:00Z",
	}})
	require.NotNil(t, zero.BalanceUSD)
	require.Zero(t, *zero.BalanceUSD)
	require.Equal(t, "2026-10-27T00:00:00Z", zero.UpdatedAt)

	for _, value := range []any{nil, "42", -1.0, math.Inf(1), math.NaN(), 1e13} {
		snapshot := accountUpstreamBalance(&service.Account{ID: 4, Extra: map[string]any{upstreamBalanceExtraKey: value}})
		require.Nil(t, snapshot.BalanceUSD, "invalid balance: %v", value)
	}

	numeric := accountUpstreamBalance(&service.Account{ID: 5, Extra: map[string]any{upstreamBalanceExtraKey: json.Number("12.34")}})
	require.NotNil(t, numeric.BalanceUSD)
	require.Equal(t, 12.34, *numeric.BalanceUSD)
}
