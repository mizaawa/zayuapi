package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *PaymentService) GetPublicOrderByResumeToken(ctx context.Context, token string) (*dbent.PaymentOrder, error) {
	claims, err := s.paymentResume().ParseToken(strings.TrimSpace(token))
	if err != nil {
		return nil, err
	}

	order, err := s.entClient.PaymentOrder.Get(ctx, claims.OrderID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
		}
		return nil, fmt.Errorf("get order by resume token: %w", err)
	}
	if claims.UserID > 0 && order.UserID != claims.UserID {
		return nil, invalidResumeTokenMatchError()
	}
	snapshot := psOrderProviderSnapshot(order)
	orderProviderInstanceID := strings.TrimSpace(psStringValue(order.ProviderInstanceID))
	orderProviderKey := strings.TrimSpace(psStringValue(order.ProviderKey))
	if snapshot != nil {
		if snapshot.ProviderInstanceID != "" {
			orderProviderInstanceID = snapshot.ProviderInstanceID
		}
		if snapshot.ProviderKey != "" {
			orderProviderKey = snapshot.ProviderKey
		}
	}
	if claims.ProviderInstanceID != "" && orderProviderInstanceID != claims.ProviderInstanceID {
		return nil, invalidResumeTokenMatchError()
	}
	if claims.ProviderKey != "" && !strings.EqualFold(orderProviderKey, claims.ProviderKey) {
		return nil, invalidResumeTokenMatchError()
	}
	if claims.PaymentType != "" && NormalizeVisibleMethod(order.PaymentType) != NormalizeVisibleMethod(claims.PaymentType) {
		return nil, invalidResumeTokenMatchError()
	}
	if err := s.RequirePaymentAccess(ctx, order.UserID); err != nil {
		return nil, err
	}
	if (order.Status == OrderStatusPending || order.Status == OrderStatusExpired) && s.beginPublicPaymentQuery(order.ID) {
		defer s.finishPublicPaymentQuery(order.ID)
		result := s.reconcilePaid(ctx, order)
		if result == checkPaidResultAlreadyPaid {
			order, err = s.entClient.PaymentOrder.Get(ctx, order.ID)
			if err != nil {
				return nil, fmt.Errorf("reload order by resume token: %w", err)
			}
		}
	}

	return order, nil
}

const publicPaymentQueryCooldown = 2 * time.Second
const maxPublicPaymentQueries = 4096

type publicPaymentQuery struct {
	inFlight bool
	after    time.Time
}

// Signed recovery tokens are reusable; they must not grant unlimited upstream
// requests. Other callers still receive the current persisted order state.
func (s *PaymentService) beginPublicPaymentQuery(orderID int64) bool {
	s.publicQueryMu.Lock()
	defer s.publicQueryMu.Unlock()
	now := time.Now()
	entry, exists := s.publicQueries[orderID]
	if exists && (entry.inFlight || now.Before(entry.after)) {
		return false
	}
	if s.publicQueries == nil {
		s.publicQueries = make(map[int64]publicPaymentQuery)
	}
	if !exists && len(s.publicQueries) >= maxPublicPaymentQueries {
		for id, old := range s.publicQueries {
			if !old.inFlight && !now.Before(old.after) {
				delete(s.publicQueries, id)
			}
		}
		if len(s.publicQueries) >= maxPublicPaymentQueries {
			return false
		}
	}
	s.publicQueries[orderID] = publicPaymentQuery{inFlight: true}
	return true
}

func (s *PaymentService) finishPublicPaymentQuery(orderID int64) {
	s.publicQueryMu.Lock()
	defer s.publicQueryMu.Unlock()
	s.publicQueries[orderID] = publicPaymentQuery{after: time.Now().Add(publicPaymentQueryCooldown)}
}

func invalidResumeTokenMatchError() error {
	return infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token does not match the payment order")
}

func (s *PaymentService) ParseWeChatPaymentResumeToken(token string) (*WeChatPaymentResumeClaims, error) {
	return s.paymentResume().ParseWeChatPaymentResumeToken(strings.TrimSpace(token))
}
