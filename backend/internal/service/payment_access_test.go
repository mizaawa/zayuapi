//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestPaymentAccessChangesApplyToExistingUsersImmediately(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("access@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	repo := &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	config := &PaymentConfigService{settingRepo: repo}
	svc := &PaymentService{entClient: client, configService: config}
	access, err := svc.GetPaymentAccess(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, &PaymentAccess{Enabled: true, Allowed: true}, access)

	enabled := true
	require.NoError(t, config.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{PaymentExclusiveEnabled: &enabled}))
	err = svc.RequirePaymentAccess(ctx, user.ID)
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(err))
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	require.NoError(t, svc.RequirePaymentAccess(ctx, user.ID))
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(svc.RequirePaymentAccess(ctx, user.ID)))

	// The global switch changes enforcement without rewriting user grants.
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	enabled = false
	require.NoError(t, config.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{PaymentExclusiveEnabled: &enabled}))
	require.NoError(t, svc.RequirePaymentAccess(ctx, user.ID))
	stored, err := client.User.Get(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, stored.ExclusiveRechargeEnabled)
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	require.NoError(t, svc.RequirePaymentAccess(ctx, user.ID))
	enabled = true
	require.NoError(t, config.UpdatePaymentConfig(ctx, UpdatePaymentConfigRequest{PaymentExclusiveEnabled: &enabled}))
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(svc.RequirePaymentAccess(ctx, user.ID)))
}

func TestPaymentAccessDoesNotExemptAdminRecharge(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	admin, err := client.User.Create().SetEmail("admin-access@example.com").SetPasswordHash("hash").SetRole(RoleAdmin).Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentExclusiveEnabled: "true"}}}}
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(svc.RequirePaymentAccess(ctx, admin.ID)))
	require.NoError(t, client.User.UpdateOneID(admin.ID).SetExclusiveRechargeEnabled(true).Exec(ctx))
	require.NoError(t, svc.RequirePaymentAccess(ctx, admin.ID))
}

func TestPaymentAccessRevocationBlocksExistingResumeAndPublicOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("resume-access@example.com").SetPasswordHash("hash").SetExclusiveRechargeEnabled(true).Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().SetUserID(user.ID).SetUserEmail(user.Email).SetUserName("").
		SetAmount(10).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("ACCESS-RESUME").
		SetOutTradeNo("sub2_access_resume").SetPaymentTradeNo("").SetPaymentType(payment.TypeAlipay).SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).SetClientIP("127.0.0.1").SetSrcHost("api.example.com").Save(ctx)
	require.NoError(t, err)
	resume := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := resume.CreateToken(ResumeTokenClaims{OrderID: order.ID, UserID: user.ID, CanonicalReturnURL: "https://app.example.com/payment/result"})
	require.NoError(t, err)
	provider := &paymentResumeLookupProvider{}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true, resumeService: resume,
		configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{SettingPaymentExclusiveEnabled: "true"}}}}
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	_, err = svc.GetPublicOrderByResumeToken(ctx, token)
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(err))
	_, err = svc.VerifyOrderPublic(ctx, order.OutTradeNo)
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(err))
	require.Zero(t, provider.queryCount)
	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, stored.Status)
}

type paymentAccessFailingSettings struct{ SettingRepository }

func (paymentAccessFailingSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("settings unavailable")
}

func TestPaymentAccessFailsClosedOnSettingsFailure(t *testing.T) {
	svc := &PaymentService{configService: &PaymentConfigService{settingRepo: paymentAccessFailingSettings{}}}
	require.ErrorContains(t, svc.RequirePaymentAccess(context.Background(), 1), "settings unavailable")
}

func TestCreateOrderRejectsUnauthorizedBeforeOrderValidation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("create-access@example.com").SetPasswordHash("hash").Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client, configService: &PaymentConfigService{settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
		SettingPaymentEnabled: "true", SettingPaymentExclusiveEnabled: "true",
	}}}}
	_, err = svc.CreateOrder(ctx, CreateOrderRequest{UserID: user.ID, Amount: 10, PaymentType: payment.TypeAlipay})
	require.Equal(t, "NOT_FOUND", infraerrors.Reason(err))
	count, err := client.PaymentOrder.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestAdminUserRechargeGrantUpdateInvalidatesAuthCache(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "grant@example.com"}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{userRepo: repo, authCacheInvalidator: invalidator}
	enabled := true
	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{ExclusiveRechargeEnabled: &enabled})
	require.NoError(t, err)
	require.True(t, updated.ExclusiveRechargeEnabled)
	require.Equal(t, []int64{42}, invalidator.userIDs)
	enabled = false
	updated, err = svc.UpdateUser(context.Background(), 42, &UpdateUserInput{ExclusiveRechargeEnabled: &enabled})
	require.NoError(t, err)
	require.False(t, updated.ExclusiveRechargeEnabled)
	require.Equal(t, []int64{42, 42}, invalidator.userIDs)
}
