package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Parsers must agree on the paid model and parameters before any reservation.
func mediaRequestFieldsUnambiguous(contentType string, body []byte) bool {
	seen := map[string]struct{}{}
	remember := func(name string) bool {
		lower := strings.ToLower(name)
		switch lower {
		case "model", "n", "size", "resolution", "resolution_name", "duration", "seconds", "ratio", "aspect_ratio", "quality", "response_format":
			name = lower
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = struct{}{}
		return true
	}
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/") {
		_, params, err := mime.ParseMediaType(contentType)
		if err != nil || params["boundary"] == "" {
			return false
		}
		reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return true
			}
			if err != nil {
				return false
			}
			name := part.FormName()
			// Repeated image files are valid; paid scalar fields must be unique.
			if part.FileName() == "" && !remember(name) {
				_ = part.Close()
				return false
			}
			if err := part.Close(); err != nil {
				return false
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !remember(name) || !consumeUniqueJSONValue(decoder) {
			return false
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}

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
		WorkbenchKind: c.GetString(workbenchPrechargeKindContextKey),
		Result:        result, APIKey: key, User: key.User, Account: account, Subscription: subscription,
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
