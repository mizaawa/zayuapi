package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const imageWorkbenchTaskIDContextKey = "image_workbench_task_id"
const mediaPrechargeContextKey = "media_precharge"
const workbenchPrechargeKindContextKey = "workbench_precharge_kind"

func mediaPrecharge(c *gin.Context) *service.MediaBillingReservation {
	value, _ := c.Get(mediaPrechargeContextKey)
	reservation, _ := value.(*service.MediaBillingReservation)
	return reservation
}

func mediaReservationID(c *gin.Context) string {
	if reservation := mediaPrecharge(c); reservation != nil {
		return reservation.ID
	}
	return ""
}

func imageWorkbenchBillingRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if id := mediaReservationID(c); id != "" {
		return id
	}
	return c.GetString(imageWorkbenchTaskIDContextKey)
}

// Image workbench and direct API requests share the same precharge lifecycle.
type imageWorkbenchFailureBilling struct {
	handler  *OpenAIGatewayHandler
	c        *gin.Context
	input    service.OpenAIRecordUsageInput
	model    string
	size     string
	count    int
	mapping  service.ChannelMappingResult
	started  time.Time
	recorded bool
}

func newImageWorkbenchFailureBilling(h *OpenAIGatewayHandler, c *gin.Context, apiKey *service.APIKey, subscription *service.UserSubscription, model, size string, count int, mapping service.ChannelMappingResult, body []byte) *imageWorkbenchFailureBilling {
	return &imageWorkbenchFailureBilling{
		handler: h, c: c, model: model, size: size, count: max(1, count), mapping: mapping,
		input: service.OpenAIRecordUsageInput{
			WorkbenchKind: c.GetString(workbenchPrechargeKindContextKey),
			APIKey:        apiKey, User: apiKey.User, Subscription: subscription,
			InboundEndpoint: GetInboundEndpoint(c), UserAgent: c.GetHeader("User-Agent"),
			IPAddress: ip.GetClientIP(c), RequestPayloadHash: service.HashUsageRequestPayload(body),
			APIKeyService: h.apiKeyService, QuotaPlatform: service.QuotaPlatform(c.Request.Context(), apiKey),
			SessionID: service.ExtractClientSessionID(c), BillingRequestID: imageWorkbenchBillingRequestID(c),
		},
	}
}

func (b *imageWorkbenchFailureBilling) attempt(account *service.Account) error {
	if b == nil {
		return nil
	}
	b.started = time.Now()
	b.input.Account = account
	b.input.UpstreamEndpoint = GetUpstreamEndpoint(b.c, account.Platform)
	model := b.model
	if mapped := strings.TrimSpace(b.mapping.MappedModel); mapped != "" {
		model = mapped
	}
	b.input.Result = &service.OpenAIForwardResult{
		Model: model, UpstreamModel: account.GetMappedModel(model), ImageCount: b.count,
		ImageSize: service.NormalizeImageBillingTierOrDefault(b.size), ImageInputSize: b.size,
	}
	if mediaPrecharge(b.c) != nil {
		return nil
	}
	quoteInput := b.input
	quoteResult := *b.input.Result
	// Token-priced image models reserve a conservative output budget; actual
	// provider usage replaces it on success. Per-image prices ignore tokens.
	quoteResult.Usage.InputTokens = 32000
	quoteResult.Usage.OutputTokens = 32768 * b.count
	quoteResult.Usage.ImageOutputTokens = quoteResult.Usage.OutputTokens
	quoteInput.Result = &quoteResult
	quoteInput.ChannelUsageFields = clientRequestedUsageFields(b.c, b.mapping, b.model, quoteResult.UpstreamModel)
	reservation, err := b.handler.gatewayService.PrechargeMedia(b.c.Request.Context(), &quoteInput)
	if err == nil && reservation != nil {
		b.c.Set(mediaPrechargeContextKey, reservation)
	}
	return err
}

func (b *imageWorkbenchFailureBilling) observe(result *service.OpenAIForwardResult) {
	if b == nil || result == nil {
		return
	}
	copy := *result
	if copy.Model == "" {
		copy.Model = b.input.Result.Model
	}
	if copy.UpstreamModel == "" {
		copy.UpstreamModel = b.input.Result.UpstreamModel
	}
	copy.ImageCount = min(b.count, result.ImageCount)
	if len(copy.ImageOutputSizes) > b.count {
		copy.ImageOutputSizes = copy.ImageOutputSizes[:b.count]
	}
	if copy.ImageSize == "" {
		copy.ImageSize = service.NormalizeImageBillingTierOrDefault(b.size)
	}
	if copy.ImageInputSize == "" {
		copy.ImageInputSize = b.size
	}
	b.input.Result = &copy
}

func (b *imageWorkbenchFailureBilling) settledByNormalPath(result *service.OpenAIForwardResult) *service.OpenAIForwardResult {
	if b == nil {
		return result
	}
	b.observe(result)
	b.recorded = true
	return b.input.Result
}

func (b *imageWorkbenchFailureBilling) finish() {
	if b == nil || b.recorded || b.input.Account == nil || b.input.Result == nil {
		return
	}
	b.recorded = true
	ctx, cancel := context.WithTimeout(context.WithoutCancel(b.c.Request.Context()), 15*time.Second)
	defer cancel()
	if err := b.handler.gatewayService.RefundMedia(ctx, mediaReservationID(b.c)); err != nil {
		logger.L().Error("media_billing.refund_failed", zap.String("reservation_id", mediaReservationID(b.c)), zap.Error(err))
	}
	if b.handler.apiKeyService != nil {
		b.handler.apiKeyService.InvalidateAuthCacheByKey(ctx, b.input.APIKey.Key)
	}
}
