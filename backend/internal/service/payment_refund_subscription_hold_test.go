//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// Use real transactional rows so failed finalization cannot silently restore
// an entitlement outside the transaction that claims the pending refund.
type refundSubscriptionRepoForTest struct {
	userSubRepoNoop
	client *dbent.Client
}

func (r *refundSubscriptionRepoForTest) inContext(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func refundSubscriptionModelForTest(sub *dbent.UserSubscription) *UserSubscription {
	return &UserSubscription{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID,
		Status: sub.Status, StartsAt: sub.StartsAt, ExpiresAt: sub.ExpiresAt, DeletedAt: sub.DeletedAt}
}

func (r *refundSubscriptionRepoForTest) GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.inContext(ctx).UserSubscription.Get(mixins.SkipSoftDelete(ctx), id)
	if err != nil {
		return nil, err
	}
	return refundSubscriptionModelForTest(sub), nil
}

func (r *refundSubscriptionRepoForTest) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.GetByIDIncludeDeleted(ctx, id)
	if err != nil || sub.DeletedAt != nil {
		return nil, ErrSubscriptionNotFound
	}
	return sub, nil
}

func (r *refundSubscriptionRepoForTest) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.GetByIDIncludeDeleted(ctx, id)
}

func (r *refundSubscriptionRepoForTest) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	sub, err := r.inContext(ctx).UserSubscription.Query().Where(usersubscription.UserIDEQ(userID),
		usersubscription.GroupIDEQ(groupID), usersubscription.DeletedAtIsNil(), usersubscription.ExpiresAtGT(time.Now())).Only(ctx)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}
	return refundSubscriptionModelForTest(sub), nil
}

func (r *refundSubscriptionRepoForTest) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.inContext(ctx).UserSubscription.Query().Where(usersubscription.UserIDEQ(userID),
		usersubscription.GroupIDEQ(groupID), usersubscription.DeletedAtIsNil(), usersubscription.ExpiresAtGT(time.Now())).Exist(ctx)
}

func (r *refundSubscriptionRepoForTest) ExtendExpiry(ctx context.Context, id int64, expiry time.Time) error {
	_, err := r.inContext(ctx).UserSubscription.UpdateOneID(id).SetExpiresAt(expiry).Save(ctx)
	return err
}

func (r *refundSubscriptionRepoForTest) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := r.inContext(ctx).UserSubscription.UpdateOneID(id).SetStatus(status).Save(ctx)
	return err
}

func (r *refundSubscriptionRepoForTest) Delete(ctx context.Context, id int64) error {
	_, err := r.inContext(ctx).UserSubscription.UpdateOneID(id).SetDeletedAt(time.Now()).Save(ctx)
	return err
}

func (r *refundSubscriptionRepoForTest) Restore(ctx context.Context, id int64, status string) (*UserSubscription, error) {
	_, err := r.inContext(ctx).UserSubscription.UpdateOneID(id).ClearDeletedAt().SetStatus(status).Save(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

type heldSubscriptionRefundProviderForTest struct {
	uncertainRefundProviderForTest
	queryStatus string
}

type refundSubscriptionCacheFailureForTest struct {
	billingCacheStub
	calls int
}

func (c *refundSubscriptionCacheFailureForTest) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	c.calls++
	return errors.New("redis unavailable")
}

func (p *heldSubscriptionRefundProviderForTest) QueryRefund(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	return &payment.RefundResponse{RefundID: "rf_sub", Status: p.queryStatus}, nil
}

func TestPendingSubscriptionRefundHoldsDeductionUntilDefinitiveOutcome(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		for _, outcome := range []string{payment.ProviderStatusPending, payment.ProviderStatusSuccess, payment.ProviderStatusFailed} {
			name := outcome + "_shortened"
			if revoked {
				name = outcome + "_revoked"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				client := newPaymentConfigServiceTestClient(t)
				ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
				order := completedRefundOrderForTest(t, ctx, client, name)
				group, err := client.Group.Create().SetName(name).Save(ctx)
				require.NoError(t, err)
				expires := time.Now().AddDate(0, 0, 8).Truncate(time.Millisecond)
				sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
					SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(expires).Save(ctx)
				require.NoError(t, err)
				days := 2
				if revoked {
					days = 30
				}
				order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
					SetSubscriptionGroupID(group.ID).SetSubscriptionDays(days).Save(ctx)
				require.NoError(t, err)
				prov := &heldSubscriptionRefundProviderForTest{uncertainRefundProviderForTest: uncertainRefundProviderForTest{
					response: &payment.RefundResponse{RefundID: "rf_sub", Status: payment.ProviderStatusPending},
				}, queryStatus: outcome}
				defer replacePaymentProviderFactoryForTest(t, prov)()
				svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
					subscriptionSvc: &SubscriptionService{userSubRepo: &refundSubscriptionRepoForTest{client: client}}}
				result, err := svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order,
					RefundAmount: 100, GatewayAmount: 100, DeductionType: payment.DeductionTypeSubscription,
					SubscriptionID: sub.ID, SubDaysToDeduct: days})
				require.NoError(t, err)
				require.False(t, result.Success)
				held, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), sub.ID)
				require.NoError(t, err)
				if revoked {
					require.NotNil(t, held.DeletedAt, "uncertain refund must keep the revoked entitlement unavailable")
				} else {
					require.True(t, expires.AddDate(0, 0, -days).Equal(held.ExpiresAt), "pending refund must keep days deducted")
				}
				cache := &refundSubscriptionCacheFailureForTest{}
				if outcome == payment.ProviderStatusFailed {
					svc.subscriptionSvc.billingCacheService = &BillingCacheService{cache: cache}
				}
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, outcome == payment.ProviderStatusSuccess, result.Success)
				finalSub, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), sub.ID)
				require.NoError(t, err)
				finalOrder, err := client.PaymentOrder.Get(ctx, order.ID)
				require.NoError(t, err)
				t.Logf("outcome=%s revoked=%t order=%s subscription_deleted=%t expiry=%s", outcome, revoked, finalOrder.Status, finalSub.DeletedAt != nil, finalSub.ExpiresAt.Format(time.RFC3339Nano))
				if outcome == payment.ProviderStatusFailed {
					require.Equal(t, 1, cache.calls, "committed restoration invalidates the cache without changing its durable result")
					require.False(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
					require.Nil(t, finalSub.DeletedAt)
					require.True(t, expires.Equal(finalSub.ExpiresAt))
					require.Equal(t, OrderStatusRefundFailed, finalOrder.Status)
					_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
					require.Error(t, err, "a repeated failure result must not restore twice")
				} else {
					require.Equal(t, held.DeletedAt, finalSub.DeletedAt)
					require.True(t, held.ExpiresAt.Equal(finalSub.ExpiresAt), "query must not deduct a second time")
				}
			})
		}
	}
}

func TestExecuteRefundSubscriptionRestorationIgnoresCacheFailure(t *testing.T) {
	for _, days := range []int{2, 30} {
		name := "shortened"
		if days == 30 {
			name = "revoked"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, name)
			group, err := client.Group.Create().SetName(name).Save(ctx)
			require.NoError(t, err)
			expiry := time.Now().AddDate(0, 0, 8).Truncate(time.Millisecond)
			sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
				SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(expiry).Save(ctx)
			require.NoError(t, err)
			order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
				SetSubscriptionGroupID(group.ID).SetSubscriptionDays(days).Save(ctx)
			require.NoError(t, err)
			cache := &refundSubscriptionCacheFailureForTest{}
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
				subscriptionSvc: &SubscriptionService{userSubRepo: &refundSubscriptionRepoForTest{client: client}}}
			prov := &uncertainRefundProviderForTest{
				response: &payment.RefundResponse{Status: payment.ProviderStatusFailed},
				onRefund: func() { svc.subscriptionSvc.billingCacheService = &BillingCacheService{cache: cache} },
			}
			defer replacePaymentProviderFactoryForTest(t, prov)()
			result, err := svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order,
				RefundAmount: 100, GatewayAmount: 100, DeductionType: payment.DeductionTypeSubscription,
				SubscriptionID: sub.ID, SubDaysToDeduct: days})
			require.NoError(t, err, "a cache outage must not report an already committed asset restoration as failed")
			require.False(t, result.Success)
			restored, err := client.UserSubscription.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Nil(t, restored.DeletedAt)
			require.True(t, expiry.Equal(restored.ExpiresAt))
			require.Equal(t, 1, cache.calls)
			require.False(t, svc.hasAuditLog(ctx, order.ID, "REFUND_ROLLBACK_FAILED"))
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusCompleted, reloaded.Status)
			t.Logf("revoked=%t status=%s subscription_restored=true cache_failed=true rollback_failed_marker=false", days == 30, reloaded.Status)
		})
	}
}

func TestPendingSubscriptionRefundFailureRestoresElapsedExpiryExactly(t *testing.T) {
	for _, elapsedDays := range []int{1, 4} {
		name := "original_expiry_still_valid"
		if elapsedDays > 2 {
			name = "original_expiry_also_elapsed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := completedRefundOrderForTest(t, ctx, client, name)
			group, err := client.Group.Create().SetName(name).Save(ctx)
			require.NoError(t, err)
			sub, err := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
				SetStartsAt(time.Now().Add(-time.Hour)).SetExpiresAt(time.Now().AddDate(0, 0, 8)).Save(ctx)
			require.NoError(t, err)
			order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
				SetSubscriptionGroupID(group.ID).SetSubscriptionDays(2).Save(ctx)
			require.NoError(t, err)
			prov := &heldSubscriptionRefundProviderForTest{uncertainRefundProviderForTest: uncertainRefundProviderForTest{
				response: &payment.RefundResponse{RefundID: "rf_sub", Status: payment.ProviderStatusPending},
			}, queryStatus: payment.ProviderStatusFailed}
			defer replacePaymentProviderFactoryForTest(t, prov)()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
				subscriptionSvc: &SubscriptionService{userSubRepo: &refundSubscriptionRepoForTest{client: client}}}
			_, err = svc.ExecuteRefund(ctx, &RefundPlan{OrderID: order.ID, Order: order,
				RefundAmount: 100, GatewayAmount: 100, DeductionType: payment.DeductionTypeSubscription,
				SubscriptionID: sub.ID, SubDaysToDeduct: 2})
			require.NoError(t, err)
			// Model a pending refund whose shortened expiry has now elapsed.
			// Restoration must reverse only those two days, even if both the
			// shortened and original expiry are already in the past.
			heldExpiry := time.Now().AddDate(0, 0, -elapsedDays).Truncate(time.Millisecond)
			_, err = client.UserSubscription.UpdateOneID(sub.ID).SetExpiresAt(heldExpiry).
				SetStatus(SubscriptionStatusExpired).Save(ctx)
			require.NoError(t, err)
			_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			restored, err := client.UserSubscription.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.True(t, heldExpiry.AddDate(0, 0, 2).Equal(restored.ExpiresAt),
				"a refund failure must not grant the time spent waiting for its gateway result")
			expectedStatus := SubscriptionStatusActive
			if elapsedDays > 2 {
				expectedStatus = SubscriptionStatusExpired
			}
			require.Equal(t, expectedStatus, restored.Status)
			t.Logf("elapsed_days=%d restored_expiry=%s status=%s", elapsedDays, restored.ExpiresAt.Format(time.RFC3339Nano), restored.Status)
		})
	}
}
