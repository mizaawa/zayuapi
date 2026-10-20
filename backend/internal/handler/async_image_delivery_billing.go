package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const asyncImageUsageGateContextKey = "async_image_usage_gate"

// asyncImageUsageGate defers billing of an accepted asynchronous image task
// until its result has been stored for the owner. A provider success alone is
// not a delivery: the upload or task write may still fail, and the owner can
// only retrieve what the task store holds.
type asyncImageUsageGate struct {
	mu      sync.Mutex
	parent  context.Context
	pending service.UsageRecordTask
}

// newAsyncImageTaskContext prepares the detached gateway context of an
// accepted task and installs its usage gate.
func newAsyncImageTaskContext(c *gin.Context, body []byte, timeout time.Duration) (*gin.Context, *httptest.ResponseRecorder, context.CancelFunc) {
	taskCtx, recorder, cancel := newAsyncImageContext(c, body, timeout)
	taskCtx.Set(asyncImageUsageGateContextKey, &asyncImageUsageGate{})
	return taskCtx, recorder, cancel
}

func asyncImageUsageGateFrom(c *gin.Context) *asyncImageUsageGate {
	if c == nil {
		return nil
	}
	value, ok := c.Get(asyncImageUsageGateContextKey)
	if !ok {
		return nil
	}
	gate, _ := value.(*asyncImageUsageGate)
	return gate
}

// holdAsyncImageUsage keeps the usage record of a task context for later
// settlement. It reports false for ordinary requests, which bill immediately.
func holdAsyncImageUsage(c *gin.Context, task service.UsageRecordTask) bool {
	gate := asyncImageUsageGateFrom(c)
	if gate == nil || task == nil {
		return false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.parent, gate.pending = c.Request.Context(), task
	return true
}

func (g *asyncImageUsageGate) take() (context.Context, service.UsageRecordTask) {
	if g == nil {
		return nil, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	parent, task := g.parent, g.pending
	g.parent, g.pending = nil, nil
	return parent, task
}

func (h *AsyncImageHandler) prepareAsyncImageDelivery(c *gin.Context) error {
	r := mediaPrecharge(c)
	if r == nil || r.ImageTaskID == "" {
		return nil
	}
	gate := asyncImageUsageGateFrom(c)
	if gate == nil || h.openAI == nil || h.openAI.gatewayService == nil {
		return service.ErrBillingServiceUnavailable
	}
	gate.mu.Lock()
	parent, record := gate.parent, gate.pending
	gate.mu.Unlock()
	if parent == nil {
		parent = c.Request.Context()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	return h.openAI.gatewayService.PrepareMediaImageDelivery(ctx, r, record)
}

// settleAsyncImageUsage bills a delivered result once, or discards the held
// usage and returns the task prepayment when nothing was delivered.
func (h *AsyncImageHandler) settleAsyncImageUsage(c *gin.Context, delivered bool) {
	parent, task := asyncImageUsageGateFrom(c).take()
	if h == nil || h.openAI == nil {
		return
	}
	if delivered {
		if reservation := mediaPrecharge(c); reservation != nil && reservation.ImageTaskID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := h.openAI.gatewayService.FinalizeMediaImageDelivery(ctx, reservation); err != nil {
				logger.L().Error("image_task.delivery_settlement_pending", zap.String("reservation_id", reservation.ID), zap.Error(err))
			}
			return
		}
		if task != nil {
			h.openAI.submitMandatoryUsageRecordTask(parent, task)
		}
		return
	}
	reservation := mediaPrecharge(c)
	if reservation == nil || h.openAI.gatewayService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := h.openAI.gatewayService.RefundMedia(ctx, reservation.ID); err != nil {
		logger.L().Error("image_task.undelivered_refund_failed", zap.String("reservation_id", reservation.ID), zap.Error(err))
	}
	if key, ok := middleware2.GetAPIKeyFromContext(c); ok && key != nil && h.openAI.apiKeyService != nil {
		h.openAI.apiKeyService.InvalidateAuthCacheByKey(ctx, key.Key)
	}
}
