//go:build unit

package service

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestPublicRedeemRejectsPaymentInternalCodesBeforeSideEffects(t *testing.T) {
	for _, code := range []string{"PAY-123-456", "PAY-ABCDEF0123456789", " pay-123-456 "} {
		result, err := (&RedeemService{}).Redeem(context.Background(), 42, code)
		require.Nil(t, result)
		require.ErrorIs(t, err, ErrRedeemCodeNotFound)
	}
}

func TestPaymentBalanceRecoveryRejectsMismatchedCode(t *testing.T) {
	otherUser := int64(43)
	for _, code := range []*RedeemCode{
		{Type: RedeemTypeBalance, Value: 10, Status: StatusUsed},
		{Type: RedeemTypeBalance, Value: 10, Status: StatusUsed, UsedBy: &otherUser},
		{Type: RedeemTypeBalance, Value: 100, Status: StatusUnused},
		{Type: RedeemTypeConcurrency, Value: 10, Status: StatusUnused},
	} {
		code.Code = "PAY-123-456"
		repo := &redeemCodeRepoStub{codesByCode: map[string]*RedeemCode{code.Code: code}}
		svc := &PaymentService{redeemService: &RedeemService{redeemRepo: repo}}
		err := svc.doBalance(context.Background(), &dbent.PaymentOrder{UserID: 42, Amount: 10, RechargeCode: code.Code}, nil)
		require.Error(t, err)
		require.Empty(t, repo.useCalls)
	}
}
