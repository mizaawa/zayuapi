//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPaymentAccessWatchWakesAfterCommittedGrantChange(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("watch@example.com").SetPasswordHash("hash").SetExclusiveRechargeEnabled(true).Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "true", SettingPaymentExclusiveEnabled: "true",
	}}}}
	previous, err := svc.GetPaymentAccess(ctx, user.ID)
	require.NoError(t, err)
	result := make(chan *PaymentAccess, 1)
	errCh := make(chan error, 1)
	go func() {
		access, waitErr := svc.WaitForPaymentAccess(ctx, user.ID, previous.Fingerprint(), 5*time.Second)
		result <- access
		errCh <- waitErr
	}()
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	PaymentAccessChanged()
	select {
	case access := <-result:
		require.NoError(t, <-errCh)
		require.False(t, access.Allowed)
	case <-time.After(time.Second):
		t.Fatal("permission change did not wake the waiting browser")
	}
}

func TestPaymentAccessWatchCancellationAndTimeout(t *testing.T) {
	svc := &PaymentService{configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}}}
	previous, err := svc.GetPaymentAccess(context.Background(), 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.WaitForPaymentAccess(ctx, 1, previous.Fingerprint(), time.Second)
	require.True(t, errors.Is(err, context.Canceled))
	access, err := svc.WaitForPaymentAccess(context.Background(), 1, previous.Fingerprint(), time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, previous, access)
}
