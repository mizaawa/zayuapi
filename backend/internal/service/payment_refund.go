package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	ok := []string{OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed}
	if !psSliceContains(ok, o.Status) {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}
	if err := s.requireRefundRollbackResolved(ctx, oid); err != nil {
		return nil, nil, err
	}
	// Check provider instance allows admin refund
	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		// Legacy order without provider_instance_id — block refund
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt <= 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	ga := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	rr := strings.TrimSpace(reason)
	if rr == "" && o.RefundRequestReason != nil {
		rr = *o.RefundRequestReason
	}
	if rr == "" {
		rr = fmt.Sprintf("refund order:%d", o.ID)
	}
	p := &RefundPlan{OrderID: oid, Order: o, RefundAmount: amt, GatewayAmount: ga, Reason: rr, Force: force, DeductBalance: deduct, DeductionType: payment.DeductionTypeNone}
	if deduct {
		if er := s.prepDeduct(ctx, o, p, force); er != nil {
			return nil, er, nil
		}
	}
	return p, nil, nil
}

func (s *PaymentService) prepDeduct(ctx context.Context, o *dbent.PaymentOrder, p *RefundPlan, force bool) *RefundResult {
	if o.OrderType == payment.OrderTypeSubscription {
		p.DeductionType = payment.DeductionTypeSubscription
		if o.SubscriptionGroupID != nil && o.SubscriptionDays != nil {
			p.SubDaysToDeduct = *o.SubscriptionDays
			sub, err := s.subscriptionSvc.GetActiveSubscription(ctx, o.UserID, *o.SubscriptionGroupID)
			if err == nil && sub != nil {
				p.SubscriptionID = sub.ID
			} else if !force {
				return &RefundResult{Success: false, Warning: "cannot find active subscription for deduction, use force", RequireForce: true}
			}
		}
		return nil
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		if !force {
			return &RefundResult{Success: false, Warning: "cannot fetch user balance, use force", RequireForce: true}
		}
		return nil
	}
	p.DeductionType = payment.DeductionTypeBalance
	if u.Balance < p.RefundAmount && !force {
		return &RefundResult{Success: false, Warning: "user balance is insufficient for deduction, use force", RequireForce: true}
	}
	p.BalanceToDeduct = math.Max(0, math.Min(p.RefundAmount, u.Balance))
	return nil
}

type availableBalanceDeductor interface {
	DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error)
}

func (s *PaymentService) deductAvailableBalance(ctx context.Context, userID int64, amount float64) (float64, error) {
	repo, ok := s.userRepo.(availableBalanceDeductor)
	if !ok {
		return 0, errors.New("user repository does not support available balance deduction")
	}
	return repo.DeductAvailableBalance(ctx, userID, amount)
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	if err := s.requireRefundRollbackResolved(ctx, p.OrderID); err != nil {
		return nil, err
	}
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusIn(OrderStatusCompleted, OrderStatusRefundRequested, OrderStatusRefundFailed)).SetStatus(OrderStatusRefunding).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if c == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	// A previous in-flight attempt may have failed restoration between the
	// first check and our status claim. Recheck before touching any assets.
	if err := s.requireRefundRollbackResolved(ctx, p.OrderID); err != nil {
		_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(OrderStatusRefundFailed).Save(ctx)
		return nil, err
	}
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		deducted := p.BalanceToDeduct
		var err error
		if p.Force {
			deducted, err = s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		} else {
			_, err = s.userRepo.AdjustBalance(ctx, p.Order.UserID, -p.BalanceToDeduct)
		}
		if err != nil {
			s.restoreStatus(ctx, p)
			if errors.Is(err, ErrBalanceNegative) {
				return &RefundResult{Warning: "user balance is insufficient for deduction, use force", RequireForce: true}, nil
			}
			return nil, fmt.Errorf("deduction: %w", err)
		}
		p.BalanceToDeduct = deducted
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		_, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct)
		if err != nil {
			if errors.Is(err, ErrAdjustWouldExpire) {
				// Deduction would expire the subscription — revoke it entirely
				slog.Info("subscription deduction would expire, revoking", "orderID", p.OrderID, "subID", p.SubscriptionID, "days", p.SubDaysToDeduct)
				if revokeErr := s.subscriptionSvc.RevokeSubscription(ctx, p.SubscriptionID); revokeErr != nil {
					s.restoreStatus(ctx, p)
					return nil, fmt.Errorf("revoke subscription: %w", revokeErr)
				}
				p.SubscriptionRevoked = true
			} else {
				// Other errors (DB failure, not found) — abort refund
				s.restoreStatus(ctx, p)
				return nil, fmt.Errorf("deduct subscription days: %w", err)
			}
		}
	}
	resp, err := s.gwRefund(ctx, p)
	// The gateway may have committed the refund even when the request was
	// cancelled. Persist its outcome and held deduction independently of that
	// request, so an HTTP timeout cannot make the credit spendable again.
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err != nil {
		return s.handleGwFail(finalizeCtx, p, err)
	}
	return s.finishRefund(finalizeCtx, p, resp)
}

func (s *PaymentService) gwRefund(ctx context.Context, p *RefundPlan) (*payment.RefundResponse, error) {
	// Use the exact provider instance that created this order, not a random one
	// from the registry. Each instance has its own merchant credentials.
	prov, err := s.getRefundProvider(ctx, p.Order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(p.Order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, p.Order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{
			"detail": err.Error(),
		})
		return nil, err
	}
	if strings.TrimSpace(p.Order.PaymentTradeNo) == "" {
		if prov.ProviderKey() == payment.TypeStripe || prov.ProviderKey() == payment.TypeAirwallex {
			return nil, fmt.Errorf("refund missing payment intent id for %s", prov.ProviderKey())
		}
		if strings.TrimSpace(p.Order.OutTradeNo) == "" {
			return nil, fmt.Errorf("refund missing order identifier")
		}
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: p.Order.PaymentTradeNo,
		OrderID: p.Order.OutTradeNo,
		Amount:  formatGatewayRefundAmount(p.GatewayAmount, p.Order),
		Reason:  p.Reason,
	})
	finishProviderCall()
	// Only an explicit provider rejection proves that no refund was accepted.
	// Transport errors and malformed replies are uncertain once Refund ran.
	if resp != nil && strings.TrimSpace(resp.Status) == payment.ProviderStatusFailed {
		if err != nil {
			return nil, err
		}
		return nil, validateRefundProviderResponse(resp)
	}
	responseErr := validateRefundProviderResponse(resp)
	if err != nil || responseErr != nil {
		if err == nil {
			err = responseErr
		}
		s.writeAuditLog(ctx, p.OrderID, "REFUND_GATEWAY_UNCERTAIN", "admin", map[string]any{"detail": psErrMsg(err)})
		return &payment.RefundResponse{RefundID: refundResponseID(resp), Status: payment.ProviderStatusPending}, nil
	}
	return resp, nil
}

func (s *PaymentService) requireRefundRollbackResolved(ctx context.Context, orderID int64) error {
	exists, err := s.entClient.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(orderID, 10)),
		paymentauditlog.ActionEQ("REFUND_ROLLBACK_FAILED"),
	).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check previous refund restoration: %w", err)
	}
	if exists {
		return infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "previous refund deduction could not be restored; manual reconciliation is required before retry")
	}
	return nil
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func validateRefundProviderResponse(resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("payment refund response missing")
	}
	status := strings.TrimSpace(resp.Status)
	switch status {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded, payment.ProviderStatusPending:
		return nil
	case payment.ProviderStatusFailed:
		return fmt.Errorf("payment refund failed: status %s", status)
	default:
		return fmt.Errorf("payment refund returned unknown status: %s", status)
	}
}

func (s *PaymentService) finishRefund(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.handleGwFail(ctx, p, err)
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.markRefundOk(ctx, p)
	case payment.ProviderStatusPending:
		return s.markRefundPending(ctx, p, resp)
	default:
		return s.handleGwFail(ctx, p, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.Status != OrderStatusRefundPending {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only refund pending orders can be finalized")
	}

	prov, err := s.getRefundProvider(ctx, o)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.BadRequest("REFUND_QUERY_UNSUPPORTED", "this payment provider does not support refund status query; please verify manually")
	}

	pendingDetail, err := s.latestRefundPendingDetail(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("load pending refund deduction: %w", err)
	}
	plan := s.refundFinalizePlan(o)
	if pendingDetail.BalanceDeductionHeld {
		plan.BalanceToDeduct = pendingDetail.BalanceDeducted
		plan.DeductionAlreadyApplied = true
	} else if pendingDetail.SubscriptionDeductionHeld {
		plan.DeductionType = payment.DeductionTypeSubscription
		plan.SubscriptionID = pendingDetail.SubscriptionID
		plan.SubDaysToDeduct = pendingDetail.SubDaysDeducted
		plan.SubscriptionRevoked = pendingDetail.SubscriptionRevoked
		plan.DeductionAlreadyApplied = true
	} else if pendingDetail.DeductionType == payment.DeductionTypeNone {
		plan.DeductionType = payment.DeductionTypeNone
		plan.BalanceToDeduct = 0
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:  o.PaymentTradeNo,
		OrderID:  o.OutTradeNo,
		RefundID: pendingDetail.RefundID,
		Amount:   formatGatewayRefundAmount(plan.GatewayAmount, o),
	})
	finishProviderCall()
	if err != nil {
		return nil, fmt.Errorf("query refund: %w", err)
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		if resp != nil && strings.TrimSpace(resp.Status) == payment.ProviderStatusFailed {
			return s.finalizeRefundFailed(ctx, o, pendingDetail, err)
		}
		return nil, err
	}

	if !plan.DeductionAlreadyApplied && !pendingDetail.DeductionRollbackOK {
		plan.BalanceToDeduct = 0
		plan.SubDaysToDeduct = 0
	} else if o.OrderType == payment.OrderTypeSubscription && !plan.DeductionAlreadyApplied && plan.DeductionType != payment.DeductionTypeNone {
		if early := s.prepDeduct(ctx, o, plan, true); early != nil {
			return early, nil
		}
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.finalizePendingRefundSuccess(ctx, plan)
	case payment.ProviderStatusPending:
		s.writeAuditLog(ctx, oid, "REFUND_QUERY_PENDING", "admin", map[string]any{"refundID": resp.RefundID})
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	default:
		return nil, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status))
	}
}

func (s *PaymentService) finalizePendingRefundSuccess(ctx context.Context, p *RefundPlan) (_ *RefundResult, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund finalization: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)

	if err := lockPendingRefundVersion(txCtx, tx.Client(), p.Order); err != nil {
		return nil, err
	}
	claimed, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(p.OrderID), paymentorder.StatusEQ(OrderStatusRefundPending)).
		SetStatus(OrderStatusRefunding).
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("claim pending refund: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}

	if err := s.applyRefundFinalDeduction(txCtx, p); err != nil {
		return nil, err
	}
	result, err := s.markRefundOkTx(txCtx, tx.Client(), p)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund finalization: %w", err)
	}
	return result, nil
}

// Bind provider results to the queried attempt while holding the order row lock.
func lockPendingRefundVersion(ctx context.Context, client *dbent.Client, expected *dbent.PaymentOrder) error {
	query := client.PaymentOrder.Query().
		Where(paymentorder.IDEQ(expected.ID), paymentorder.StatusEQ(OrderStatusRefundPending))
	// SQLite test transactions use database locks instead of FOR UPDATE.
	if client.Driver().Dialect() != dialect.SQLite {
		query.ForUpdate()
	}
	current, err := query.Only(ctx)
	if dbent.IsNotFound(err) {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	if err != nil {
		return fmt.Errorf("lock pending refund: %w", err)
	}
	if !current.UpdatedAt.Equal(expected.UpdatedAt) {
		return infraerrors.Conflict("CONFLICT", "refund attempt changed")
	}
	return nil
}

func (s *PaymentService) refundFinalizePlan(o *dbent.PaymentOrder) *RefundPlan {
	refundAmount := o.RefundAmount
	reason := strings.TrimSpace(psStringValue(o.RefundReason))
	if reason == "" {
		reason = fmt.Sprintf("refund order:%d", o.ID)
	}
	return &RefundPlan{
		OrderID:       o.ID,
		Order:         o,
		RefundAmount:  refundAmount,
		GatewayAmount: calculateGatewayRefundAmount(o.Amount, o.PayAmount, refundAmount, PaymentOrderCurrency(o)),
		Reason:        reason,
		Force:         o.ForceRefund,
		DeductBalance: true,
		DeductionType: payment.DeductionTypeBalance,
		BalanceToDeduct: func() float64 {
			if o.OrderType == payment.OrderTypeBalance {
				return refundAmount
			}
			return 0
		}(),
	}
}

func (s *PaymentService) applyRefundFinalDeduction(ctx context.Context, p *RefundPlan) error {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 && !p.DeductionAlreadyApplied {
		deducted := p.BalanceToDeduct
		var err error
		if p.Force {
			deducted, err = s.deductAvailableBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		} else {
			// Legacy pending refunds already returned their credit to the user.
			// Recover all refunded credit even if it was spent before confirmation.
			err = s.userRepo.DeductBalance(ctx, p.Order.UserID, p.BalanceToDeduct)
		}
		if err != nil {
			return fmt.Errorf("deduction: %w", err)
		}
		p.BalanceToDeduct = deducted
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 && !p.DeductionAlreadyApplied {
		if _, err := s.subscriptionSvc.ExtendSubscription(ctx, p.SubscriptionID, -p.SubDaysToDeduct); err != nil {
			if errors.Is(err, ErrAdjustWouldExpire) {
				if revokeErr := s.subscriptionSvc.RevokeSubscription(ctx, p.SubscriptionID); revokeErr != nil {
					return fmt.Errorf("revoke subscription: %w", revokeErr)
				}
			} else {
				return fmt.Errorf("deduct subscription days: %w", err)
			}
		}
	}
	return nil
}

func (s *PaymentService) finalizeRefundFailed(ctx context.Context, o *dbent.PaymentOrder, detail refundPendingAuditDetail, gErr error) (_ *RefundResult, err error) {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin failed refund finalization: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := lockPendingRefundVersion(txCtx, tx.Client(), o); err != nil {
		return nil, err
	}
	now := time.Now()
	claimed, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(o.ID), paymentorder.StatusEQ(OrderStatusRefundPending)).
		SetStatus(OrderStatusRefundFailed).SetFailedAt(now).SetFailedReason(psErrMsg(gErr)).Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("mark refund failed: %w", err)
	}
	if claimed == 0 {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed")
	}
	if detail.BalanceDeductionHeld && detail.BalanceDeducted > 0 {
		if err := s.userRepo.UpdateBalance(txCtx, o.UserID, detail.BalanceDeducted); err != nil {
			return nil, fmt.Errorf("restore pending refund balance: %w", err)
		}
	}
	var restoredSubscription *UserSubscription
	if detail.SubscriptionDeductionHeld && detail.SubDaysDeducted > 0 && detail.SubscriptionID > 0 {
		restoredSubscription, err = s.restoreRefundSubscription(txCtx, detail.SubscriptionID, detail.SubDaysDeducted, detail.SubscriptionRevoked)
		if err != nil {
			return nil, fmt.Errorf("restore pending refund subscription: %w", err)
		}
	}
	auditDetail, err := json.Marshal(map[string]any{"detail": psErrMsg(gErr), "balanceRestored": detail.BalanceDeducted})
	if err != nil {
		return nil, err
	}
	if err := writeRefundAuditMarker(txCtx, tx.Client(), o.ID, "REFUND_FAILED", string(auditDetail)); err != nil {
		return nil, fmt.Errorf("write failed refund audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit failed refund finalization: %w", err)
	}
	s.invalidateRefundSubscriptionCaches(restoredSubscription)
	return &RefundResult{Success: false, Warning: "gateway refund failed: " + psErrMsg(gErr)}, nil
}

type refundPendingAuditDetail struct {
	RefundID                  string  `json:"refundID"`
	DeductionRollbackOK       bool    `json:"deductionRollbackOK"`
	DeductionType             string  `json:"deductionType"`
	BalanceDeductionHeld      bool    `json:"balanceDeductionHeld"`
	BalanceDeducted           float64 `json:"balanceDeducted"`
	SubscriptionDeductionHeld bool    `json:"subscriptionDeductionHeld"`
	SubscriptionID            int64   `json:"subscriptionID"`
	SubDaysDeducted           int     `json:"subDaysDeducted"`
	SubscriptionRevoked       bool    `json:"subscriptionRevoked"`
}

func (s *PaymentService) latestRefundPendingDetail(ctx context.Context, oid int64) (refundPendingAuditDetail, error) {
	logEntry, err := s.entClient.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(oid, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
		Order(paymentauditlog.ByCreatedAt(sql.OrderDesc()), paymentauditlog.ByID(sql.OrderDesc())).
		First(ctx)
	if dbent.IsNotFound(err) {
		return refundPendingAuditDetail{DeductionRollbackOK: true}, nil
	}
	if err != nil {
		return refundPendingAuditDetail{}, err
	}
	detail := refundPendingAuditDetail{DeductionRollbackOK: true}
	if err := json.Unmarshal([]byte(logEntry.Detail), &detail); err != nil {
		return refundPendingAuditDetail{}, err
	}
	detail.RefundID = strings.TrimSpace(detail.RefundID)
	return detail, nil
}

// getRefundProvider creates a provider using the order's original instance config.
// Delegates to getOrderProvider which handles instance lookup and fallback.
func (s *PaymentService) getRefundProvider(ctx context.Context, o *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", o.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) handleGwFail(ctx context.Context, p *RefundPlan, gErr error) (*RefundResult, error) {
	if s.RollbackRefund(ctx, p, gErr) {
		s.restoreStatus(ctx, p)
		s.writeAuditLog(ctx, p.OrderID, "REFUND_GATEWAY_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
		return &RefundResult{Success: false, Warning: "gateway failed: " + psErrMsg(gErr) + ", rolled back"}, nil
	}
	now := time.Now()
	_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(OrderStatusRefundFailed).SetFailedAt(now).SetFailedReason(psErrMsg(gErr)).Save(ctx)
	s.writeAuditLog(ctx, p.OrderID, "REFUND_FAILED", "admin", map[string]any{"detail": psErrMsg(gErr)})
	return nil, infraerrors.InternalServer("REFUND_FAILED", psErrMsg(gErr))
}

func (s *PaymentService) markRefundOk(ctx context.Context, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	s.writeAuditLog(ctx, p.OrderID, "REFUND_SUCCESS", "admin", map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func (s *PaymentService) markRefundOkTx(ctx context.Context, client *dbent.Client, p *RefundPlan) (*RefundResult, error) {
	fs := OrderStatusRefunded
	if p.RefundAmount < p.Order.Amount {
		fs = OrderStatusPartiallyRefunded
	}
	now := time.Now()
	_, err := client.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(fs).SetRefundAmount(p.RefundAmount).SetRefundReason(p.Reason).SetRefundAt(now).SetForceRefund(p.Force).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark refund: %w", err)
	}
	detail, err := json.Marshal(map[string]any{"refundAmount": p.RefundAmount, "reason": p.Reason, "balanceDeducted": p.BalanceToDeduct, "force": p.Force})
	if err != nil {
		return nil, fmt.Errorf("marshal refund audit: %w", err)
	}
	if _, err := client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(p.OrderID, 10)).
		SetAction("REFUND_SUCCESS").
		SetDetail(string(detail)).
		SetOperator("admin").
		Save(ctx); err != nil {
		return nil, fmt.Errorf("write refund audit: %w", err)
	}
	return &RefundResult{Success: true, BalanceDeducted: p.BalanceToDeduct, SubDaysDeducted: p.SubDaysToDeduct}, nil
}

func (s *PaymentService) markRefundPending(ctx context.Context, p *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	balanceDeducted := p.BalanceToDeduct
	subDaysDeducted := p.SubDaysToDeduct
	// Keep refunded credit unavailable until the gateway confirms its outcome.
	balanceHeld := p.DeductionType == payment.DeductionTypeBalance
	subscriptionHeld := p.DeductionType == payment.DeductionTypeSubscription
	deductionHeld := balanceHeld || subscriptionHeld
	rollbackOK := true
	if !deductionHeld {
		rollbackOK = s.RollbackRefund(ctx, p, nil)
	}
	if rollbackOK && !deductionHeld {
		p.BalanceToDeduct = 0
		p.SubDaysToDeduct = 0
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin pending refund: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err = tx.PaymentOrder.UpdateOneID(p.OrderID).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(p.RefundAmount).
		SetRefundReason(p.Reason).
		ClearRefundAt().
		SetForceRefund(p.Force).
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx)
	if err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}

	detail := map[string]any{
		"refundID":                  refundResponseID(resp),
		"refundAmount":              p.RefundAmount,
		"reason":                    p.Reason,
		"force":                     p.Force,
		"balanceDeducted":           p.BalanceToDeduct,
		"subDaysDeducted":           p.SubDaysToDeduct,
		"balanceRolledBack":         balanceDeducted,
		"subDaysRolledBack":         subDaysDeducted,
		"deductionRollbackOK":       rollbackOK,
		"deductionType":             p.DeductionType,
		"balanceDeductionHeld":      balanceHeld,
		"subscriptionDeductionHeld": subscriptionHeld,
		"subscriptionID":            p.SubscriptionID,
		"subscriptionRevoked":       p.SubscriptionRevoked,
	}
	if balanceHeld {
		detail["balanceRolledBack"] = 0
	}
	if subscriptionHeld {
		detail["subDaysRolledBack"] = 0
	}
	auditDetail, err := json.Marshal(detail)
	if err != nil {
		return nil, err
	}
	if err := writeRefundAuditMarker(txCtx, tx.Client(), p.OrderID, "REFUND_PENDING", string(auditDetail)); err != nil {
		return nil, fmt.Errorf("write pending refund audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit pending refund: %w", err)
	}

	warning := "gateway refund is pending confirmation"
	if !rollbackOK {
		warning += "; refund deduction rollback failed"
	}
	return &RefundResult{Success: false, Warning: warning}, nil
}

// Refund attempts can be retried after a definitive failure. The production
// schema has one audit row per order/action, so these state markers must carry
// the latest attempt's deduction details instead of rejecting a second insert.
// Callers hold the order's refund state/version lock in the same transaction.
func writeRefundAuditMarker(ctx context.Context, client *dbent.Client, orderID int64, action, detail string) error {
	orderKey := strconv.FormatInt(orderID, 10)
	updated, err := client.PaymentAuditLog.Update().
		Where(paymentauditlog.OrderIDEQ(orderKey), paymentauditlog.ActionEQ(action)).
		SetOperator("admin").SetDetail(detail).Save(ctx)
	if err != nil || updated > 0 {
		return err
	}
	_, err = client.PaymentAuditLog.Create().SetOrderID(orderKey).
		SetAction(action).SetOperator("admin").SetDetail(detail).Save(ctx)
	return err
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}

func (s *PaymentService) RollbackRefund(ctx context.Context, p *RefundPlan, gErr error) bool {
	if p.DeductionType == payment.DeductionTypeBalance && p.BalanceToDeduct > 0 {
		if err := s.userRepo.UpdateBalance(ctx, p.Order.UserID, p.BalanceToDeduct); err != nil {
			slog.Error("[CRITICAL] rollback failed", "orderID", p.OrderID, "amount", p.BalanceToDeduct, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{"gatewayError": psErrMsg(gErr), "rollbackError": psErrMsg(err), "balanceDeducted": p.BalanceToDeduct})
			return false
		}
	}
	if p.DeductionType == payment.DeductionTypeSubscription && p.SubDaysToDeduct > 0 && p.SubscriptionID > 0 {
		restored, err := s.restoreRefundSubscription(ctx, p.SubscriptionID, p.SubDaysToDeduct, p.SubscriptionRevoked)
		if err != nil {
			slog.Error("[CRITICAL] subscription rollback failed", "orderID", p.OrderID, "subID", p.SubscriptionID, "days", p.SubDaysToDeduct, "error", err)
			s.writeAuditLog(ctx, p.OrderID, "REFUND_ROLLBACK_FAILED", "admin", map[string]any{"gatewayError": psErrMsg(gErr), "rollbackError": psErrMsg(err), "subDaysDeducted": p.SubDaysToDeduct})
			return false
		}
		s.invalidateRefundSubscriptionCaches(restored)
	}
	return true
}

func (s *PaymentService) restoreRefundSubscription(ctx context.Context, subscriptionID int64, days int, revoked bool) (*UserSubscription, error) {
	// This reverses an earlier deduction, rather than purchasing a renewal.
	// Always add back to the persisted expiry: a pending refund may outlive
	// the shortened subscription and must not grant the waiting period too.
	txCtx := ctx
	var ownedTx *dbent.Tx
	if dbent.TxFromContext(ctx) == nil {
		var err error
		ownedTx, err = s.entClient.Tx(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = ownedTx.Rollback() }()
		txCtx = dbent.NewTxContext(ctx, ownedTx)
	}
	repo := s.subscriptionSvc.userSubRepo
	sub, err := repo.GetByIDForUpdate(mixins.SkipSoftDelete(txCtx), subscriptionID)
	if err != nil {
		return nil, err
	}
	if revoked {
		// Read the stored status after acquiring the row lock; normal lookups
		// map a deleted row to "revoked" instead of preserving that status.
		sub, err = repo.GetByIDIncludeDeleted(txCtx, subscriptionID)
		if err != nil {
			return nil, err
		}
		if sub.DeletedAt == nil {
			return nil, ErrSubscriptionNotRevoked
		}
		exists, err := repo.ExistsActiveByUserIDAndGroupID(txCtx, sub.UserID, sub.GroupID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrSubscriptionRestoreConflict
		}
		status := sub.Status
		if status == SubscriptionStatusActive && !sub.ExpiresAt.After(time.Now()) {
			status = SubscriptionStatusExpired
		}
		sub, err = repo.Restore(txCtx, subscriptionID, status)
		if err != nil {
			return nil, err
		}
	} else {
		if sub.DeletedAt != nil {
			return nil, ErrSubscriptionNotFound
		}
		expiresAt := sub.ExpiresAt.AddDate(0, 0, days)
		if err := repo.ExtendExpiry(txCtx, subscriptionID, expiresAt); err != nil {
			return nil, err
		}
		status := sub.Status
		if status == SubscriptionStatusExpired && expiresAt.After(time.Now()) {
			status = SubscriptionStatusActive
		} else if status == SubscriptionStatusActive && !expiresAt.After(time.Now()) {
			status = SubscriptionStatusExpired
		}
		if status != sub.Status {
			if err := repo.UpdateStatus(txCtx, subscriptionID, status); err != nil {
				return nil, err
			}
		}
		sub.ExpiresAt, sub.Status = expiresAt, status
	}
	if ownedTx != nil {
		if err := ownedTx.Commit(); err != nil {
			return nil, err
		}
	}
	return sub, nil
}

// The restoration is already committed. A cache outage must not be reported as
// an asset rollback failure, which could otherwise change later refund logic.
func (s *PaymentService) invalidateRefundSubscriptionCaches(sub *UserSubscription) {
	if sub == nil {
		return
	}
	if err := s.subscriptionSvc.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		slog.Warn("refund subscription restored but cache invalidation failed", "subID", sub.ID, "error", err)
	}
}

func (s *PaymentService) restoreStatus(ctx context.Context, p *RefundPlan) {
	rs := OrderStatusCompleted
	if p.Order.Status == OrderStatusRefundRequested {
		rs = OrderStatusRefundRequested
	}
	_, _ = s.entClient.PaymentOrder.UpdateOneID(p.OrderID).SetStatus(rs).Save(ctx)
}
