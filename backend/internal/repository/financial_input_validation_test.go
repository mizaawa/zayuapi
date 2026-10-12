package repository

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinancialBalanceMutationsRejectNonFiniteAmounts(t *testing.T) {
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		// A nil client proves invalid input never reaches the database.
		repo := &userRepository{}
		ctx := context.Background()
		require.Error(t, repo.UpdateBalance(ctx, 1, amount))
		require.Error(t, repo.ApplyRedeemBalanceAdjustment(ctx, 1, amount))
		require.Error(t, repo.DeductBalance(ctx, 1, amount))
		_, err := repo.DeductAvailableBalance(ctx, 1, amount)
		require.Error(t, err)
		_, err = repo.AdjustBalance(ctx, 1, amount)
		require.Error(t, err)
		_, err = repo.SetBalance(ctx, 1, amount)
		require.Error(t, err)
	}
}

func TestFinancialBalanceDeductionsRejectNegativeAmounts(t *testing.T) {
	repo := &userRepository{}
	require.Error(t, repo.DeductBalance(context.Background(), 1, -1))
	_, err := repo.DeductAvailableBalance(context.Background(), 1, -1)
	require.Error(t, err)
}
