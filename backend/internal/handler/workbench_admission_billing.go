package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) prechargeWorkbench(c *gin.Context, body []byte, platform, kind string) error {
	if h == nil || h.gatewayService == nil {
		return service.ErrBillingServiceUnavailable
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil {
		return service.ErrImageTaskForbidden
	}
	subscription, _ := middleware.GetSubscriptionFromContext(c)
	c.Set(workbenchPrechargeKindContextKey, kind)
	if kind == "video" {
		info := service.ParseGrokMediaRequest("application/json", body)
		account, err := h.gatewayService.SelectMediaPrechargeAccount(c.Request.Context(), key.GroupID, platform, info.Model, mediaRequiredCapability(service.GrokMediaEndpointVideosGenerations, platform), "")
		if err != nil {
			return err
		}
		return h.prechargeVideo(c, key, subscription, account, service.GrokMediaEndpointVideosGenerations, info, service.ChannelMappingResult{MappedModel: info.Model}, body)
	}
	var model, size string
	var count int
	capability := service.OpenAIImagesCapability("")
	ctx := c.Request.Context()
	if platform == service.PlatformGrok {
		info := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		model, size, count = service.NormalizeGrokMediaModelForEndpoint(service.GrokMediaEndpointImagesGenerations, info.Model, info.HasInputImage()), info.Size, info.N
	} else {
		parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			return err
		}
		model, size, count, capability = parsed.Model, parsed.Size, parsed.N, parsed.RequiredCapability
		if parsed.Resolution != "" {
			size = parsed.SizeTier
		}
		ctx = service.WithOpenAIImagesEndpoint(service.WithOpenAIImageGenerationIntent(ctx))
	}
	routingModel := model
	if resolved, ok := service.ResolvedUpstreamModelFromContext(ctx); ok {
		routingModel = resolved
	}
	mapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, key.GroupID, routingModel)
	if mapping.MappedModel != "" {
		routingModel = mapping.MappedModel
	}
	account, err := h.gatewayService.SelectMediaPrechargeAccount(ctx, key.GroupID, platform, routingModel, "", capability)
	if err != nil {
		return err
	}
	billing := newImageWorkbenchFailureBilling(h, c, key, subscription, model, size, count, mapping, body)
	return billing.attempt(account)
}

func (h *OpenAIGatewayHandler) keepWorkbenchPrepayment(c *gin.Context) func() {
	r := mediaPrecharge(c)
	if h == nil || h.gatewayService == nil || r == nil || r.WorkbenchKind == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(ctx, 5*time.Second)
				_ = h.gatewayService.RenewMediaQueue(renewCtx, r)
				renewCancel()
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (h *OpenAIGatewayHandler) refundUndispatchedWorkbench(c *gin.Context) {
	if h == nil || h.gatewayService == nil {
		return
	}
	r := mediaPrecharge(c)
	if r == nil || r.WorkbenchKind == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := h.gatewayService.RefundMediaQueue(ctx, r.ID, false); err != nil {
		logger.L().Error("media_billing.queue_refund_failed", zap.String("reservation_id", r.ID), zap.Error(err))
	}
	if h.apiKeyService != nil {
		key, _ := middleware.GetAPIKeyFromContext(c)
		if key != nil {
			h.apiKeyService.InvalidateAuthCacheByKey(ctx, key.Key)
		}
	}
}

func (h *OpenAIGatewayHandler) dispatchWorkbenchMedia(c *gin.Context, accountID int64) error {
	return h.gatewayService.DispatchMediaQueue(c.Request.Context(), mediaPrecharge(c), accountID)
}

func (h *OpenAIGatewayHandler) checkMediaBillingEligibility(c *gin.Context, key *service.APIKey, subscription *service.UserSubscription) error {
	if service.MediaWorkbenchPrepaid(c.Request.Context(), key) {
		return nil
	}
	return h.billingCacheService.CheckBillingEligibility(c.Request.Context(), key.User, key, key.Group, subscription, service.QuotaPlatform(c.Request.Context(), key))
}

func workbenchBillingErrorDetails(err error) (int, string, string) {
	if errors.Is(err, service.ErrNoAvailableAccounts) {
		return http.StatusServiceUnavailable, "channel_unavailable", "No eligible media channel is currently available"
	}
	if errors.Is(err, service.ErrInsufficientBalance) {
		return http.StatusPaymentRequired, "insufficient_balance", "Insufficient balance or API key quota for task prepayment"
	}
	if errors.Is(err, service.ErrBillingServiceUnavailable) {
		return http.StatusServiceUnavailable, "service_unavailable", "Task prepayment is temporarily unavailable"
	}
	if errors.Is(err, service.ErrAPIKeyRateLimit5hExceeded) || errors.Is(err, service.ErrAPIKeyRateLimit1dExceeded) || errors.Is(err, service.ErrAPIKeyRateLimit7dExceeded) || errors.Is(err, service.ErrDailyLimitExceeded) || errors.Is(err, service.ErrWeeklyLimitExceeded) || errors.Is(err, service.ErrMonthlyLimitExceeded) {
		return http.StatusTooManyRequests, "rate_limited", "The current spending limit cannot cover task prepayment"
	}
	return http.StatusBadRequest, "invalid_parameters", "Media request parameters or pricing are unavailable"
}
