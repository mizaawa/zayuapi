package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) prechargeVideo(c *gin.Context, key *service.APIKey, subscription *service.UserSubscription, account *service.Account, endpoint service.GrokMediaEndpoint, info service.GrokMediaRequestInfo, mapping service.ChannelMappingResult, body []byte) error {
	if existing := mediaPrecharge(c); existing != nil {
		if existing.Quote.Command.AccountID == account.ID {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 15*time.Second)
		defer cancel()
		if err := h.gatewayService.RefundMedia(ctx, existing.ID); err != nil {
			return err
		}
		c.Set(mediaPrechargeContextKey, nil)
	}
	if !endpoint.IsSeedance() {
		adapted, err := service.VideoPrechargeRequestInfo(c.Request.Context(), account, body, c.GetHeader("Content-Type"))
		if err != nil {
			return err
		}
		info = adapted
	}
	if info.N > 1 {
		return fmt.Errorf("video creation accepts one task per request")
	}
	resolution, duration := info.Resolution, info.DurationSeconds
	if account.Platform == service.PlatformCustom {
		if !info.ResolutionValid {
			resolution = ""
		}
		duration = service.NormalizeCustomVideoDurationSeconds(info.RawDurationSeconds)
		if err := h.gatewayService.ValidateCustomVideoPricing(c.Request.Context(), key, info.Model, resolution); err != nil {
			return err
		}
	}
	result := &service.OpenAIForwardResult{Model: info.Model, UpstreamModel: account.GetMappedModel(info.Model), VideoCount: 1, VideoResolution: resolution, VideoDurationSeconds: duration, VideoPriceMultiplier: service.VideoReferencePriceMultiplier(info.Model, info.HasReferenceVideo)}
	reservation, err := h.gatewayService.PrechargeMedia(c.Request.Context(), &service.OpenAIRecordUsageInput{
		Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription,
		InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: GetUpstreamEndpoint(c, account.Platform),
		RequestPayloadHash: service.HashUsageRequestPayload(body), APIKeyService: h.apiKeyService,
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), key),
		ChannelUsageFields: clientRequestedUsageFields(c, mapping, info.Model, result.UpstreamModel),
	})
	if err == nil && reservation != nil {
		c.Set(mediaPrechargeContextKey, reservation)
	}
	return err
}
