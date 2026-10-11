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

func imageWorkbenchBillingRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(imageWorkbenchTaskIDContextKey)
}

// A failed workbench attempt still uses the normal group pricing and ledger.
// Queue admission, local validation failures and retries are not new charges.
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
	if imageWorkbenchBillingRequestID(c) == "" {
		return nil
	}
	return &imageWorkbenchFailureBilling{
		handler: h, c: c, model: model, size: size, count: max(1, count), mapping: mapping,
		input: service.OpenAIRecordUsageInput{
			APIKey: apiKey, User: apiKey.User, Subscription: subscription,
			InboundEndpoint: GetInboundEndpoint(c), UserAgent: c.GetHeader("User-Agent"),
			IPAddress: ip.GetClientIP(c), RequestPayloadHash: service.HashUsageRequestPayload(body),
			APIKeyService: h.apiKeyService, QuotaPlatform: service.QuotaPlatform(c.Request.Context(), apiKey),
			SessionID: service.ExtractClientSessionID(c), BillingRequestID: imageWorkbenchBillingRequestID(c),
		},
	}
}

func (b *imageWorkbenchFailureBilling) attempt(account *service.Account) {
	if b == nil {
		return
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
	copy.ImageCount = b.count
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
	if b.input.Result.Duration == 0 {
		b.input.Result.Duration = time.Since(b.started)
	}
	b.input.ChannelUsageFields = clientRequestedUsageFields(b.c, b.mapping, b.model, b.input.Result.UpstreamModel)
	input := b.input
	b.handler.submitMandatoryUsageRecordTask(b.c.Request.Context(), func(ctx context.Context) {
		if err := b.handler.gatewayService.RecordUsage(ctx, &input); err != nil {
			logger.L().Error("image_workbench.failure_billing_failed",
				zap.String("task_id", input.BillingRequestID), zap.Error(err))
		}
	})
}
