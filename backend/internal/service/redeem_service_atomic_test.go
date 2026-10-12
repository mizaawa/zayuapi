//go:build unit

package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type redeemConcurrentEditRepo struct {
	redeemRejectRepo
	onUse func()
}

func (r *redeemConcurrentEditRepo) Use(ctx context.Context, id, userID int64) error {
	if err := r.redeemRejectRepo.Use(ctx, id, userID); err != nil {
		return err
	}
	r.onUse()
	return nil
}

func TestRedeemUsesBenefitsFromLockedCodeAfterConcurrentEdit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	repo := &redeemConcurrentEditRepo{redeemRejectRepo: redeemRejectRepo{code: RedeemCode{
		ID: 1, Code: "concurrent-edit", Type: RedeemTypeBalance, Value: 100, Status: StatusUnused,
	}}}
	repo.onUse = func() { repo.code.Value = 10 }
	credited := 0.0
	svc := NewRedeemService(repo, &mockUserRepo{updateBalanceFn: func(ctx context.Context, id int64, amount float64) error {
		require.NotNil(t, dbent.TxFromContext(ctx))
		credited += amount
		return nil
	}}, nil, nil, nil, client, nil, nil)
	result, err := svc.Redeem(ctx, 2, repo.code.Code)
	require.NoError(t, err)
	require.Equal(t, 10.0, credited)
	require.Equal(t, 10.0, result.Value)
}
