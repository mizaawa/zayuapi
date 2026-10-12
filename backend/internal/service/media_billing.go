package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type MediaBillingQuote struct {
	Command UsageBillingCommand `json:"command"`
	Usage   UsageLog            `json:"usage"`
}

type MediaBillingReservation struct {
	ID       string            `json:"id"`
	Quote    MediaBillingQuote `json:"quote"`
	GroupID  int64             `json:"group_id"`
	TaskID   string            `json:"task_id"`
	Endpoint GrokMediaEndpoint `json:"endpoint"`
	// Resolution quotes retain the price at admission even if an administrator
	// edits group/channel prices while an asynchronous job is running.
	VideoPrices   map[string]MediaBillingQuote `json:"video_prices,omitempty"`
	FinalQuote    *MediaBillingQuote           `json:"-"`
	RefundPending bool                         `json:"-"`
}

type MediaBillingRepository interface {
	ReserveMedia(context.Context, *MediaBillingReservation) error
	StageMediaOutcome(context.Context, string, *MediaBillingQuote) error
	SettleMedia(context.Context, string, *UsageBillingCommand) (*UsageBillingApplyResult, error)
	RefundMedia(context.Context, string) (*MediaBillingReservation, error)
	BindMediaTask(context.Context, string, string, GrokMediaEndpoint) error
	FindMediaTask(context.Context, string, int64, int64) (*MediaBillingReservation, error)
	PendingMedia(context.Context, int) ([]*MediaBillingReservation, error)
}

func (s *OpenAIGatewayService) PrechargeMedia(ctx context.Context, input *OpenAIRecordUsageInput) (*MediaBillingReservation, error) {
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return nil, ErrBillingServiceUnavailable
	}
	copyInput := *input
	copyInput.BillingRequestID = "media:" + uuid.NewString()
	copyInput.PricingAt = time.Now()
	var quote *MediaBillingQuote
	copyInput.MediaQuote = &quote
	if err := s.RecordUsage(ctx, &copyInput); err != nil {
		return nil, err
	}
	if quote == nil {
		return nil, errors.New("media price is unavailable")
	}
	reservation := &MediaBillingReservation{ID: copyInput.BillingRequestID, Quote: *quote}
	if input.APIKey.GroupID != nil {
		reservation.GroupID = *input.APIKey.GroupID
	}
	if input.Result.VideoCount > 0 {
		reservation.VideoPrices = map[string]MediaBillingQuote{}
		for _, resolution := range []string{"", "480p", "720p", "1080p", "2k", "4k"} {
			result := *input.Result
			result.VideoResolution = resolution
			copyInput.Result = &result
			if err := s.RecordUsage(ctx, &copyInput); err == nil && quote != nil {
				reservation.VideoPrices[resolution] = *quote
			}
		}
	}
	if err := repo.ReserveMedia(ctx, reservation); err != nil {
		if errors.Is(err, ErrInsufficientBalance) || errors.Is(err, ErrDailyLimitExceeded) || errors.Is(err, ErrWeeklyLimitExceeded) || errors.Is(err, ErrMonthlyLimitExceeded) || errors.Is(err, ErrAPIKeyRateLimit5hExceeded) || errors.Is(err, ErrAPIKeyRateLimit1dExceeded) || errors.Is(err, ErrAPIKeyRateLimit7dExceeded) {
			return nil, err
		}
		logger.L().Error("media_billing.reserve_failed", zap.Error(err))
		return nil, ErrBillingServiceUnavailable
	}
	if invalidator, ok := input.APIKeyService.(apiKeyAuthCacheInvalidator); ok {
		invalidator.InvalidateAuthCacheByKey(ctx, input.APIKey.Key)
	}
	s.invalidateMediaBilling(ctx, reservation)
	return reservation, nil
}

func (s *OpenAIGatewayService) RefundMedia(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return errors.New("media precharge repository is unavailable")
	}
	if err := repo.StageMediaOutcome(ctx, id, nil); err != nil {
		return err
	}
	reservation, err := repo.RefundMedia(ctx, id)
	if err == nil {
		s.invalidateMediaBilling(ctx, reservation)
		if reservation != nil {
			usage := reservation.Quote.Usage
			usage.ActualCost, usage.TotalCost = 0, 0
			usage.InputCost, usage.OutputCost, usage.ImageOutputCost = 0, 0, 0
			usage.ImageInputCost, usage.CacheCreationCost, usage.CacheReadCost = 0, 0, 0
			usage.InputTokens, usage.OutputTokens, usage.ImageInputTokens, usage.ImageOutputTokens = 0, 0, 0, 0
			zero := 0.0
			usage.AccountStatsCost = &zero
			writeUsageLogBestEffort(ctx, s.usageLogRepo, &usage, "service.media_billing")
		}
	}
	return err
}

func (s *OpenAIGatewayService) BindMediaTask(ctx context.Context, reservation *MediaBillingReservation, taskID string, endpoint GrokMediaEndpoint) error {
	if reservation == nil {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return errors.New("media precharge repository is unavailable")
	}
	return repo.BindMediaTask(ctx, reservation.ID, taskID, endpoint)
}

func (s *OpenAIGatewayService) invalidateMediaBilling(ctx context.Context, r *MediaBillingReservation) {
	if r == nil {
		return
	}
	s.invalidateGrokMediaVideoBillingCaches(ctx, &GrokMediaVideoBillingRecord{Command: r.Quote.Command, GroupID: r.GroupID})
}

func (s *OpenAIGatewayService) ReconcileMediaTask(ctx context.Context, taskID string, userID, keyID int64, body []byte) (bool, error) {
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return false, nil
	}
	r, err := repo.FindMediaTask(ctx, taskID, userID, keyID)
	if err != nil || r == nil {
		return false, err
	}
	return true, s.settleMediaStatus(ctx, r, body)
}

func (s *OpenAIGatewayService) settleMediaStatus(ctx context.Context, r *MediaBillingReservation, body []byte) error {
	if GrokMediaVideoStatusFailed(body) {
		return s.RefundMedia(ctx, r.ID)
	}
	complete := false
	for _, path := range []string{"status", "data.status", "video.status", "state", "data.state"} {
		switch strings.ToLower(gjson.GetBytes(body, path).String()) {
		case "completed", "succeeded", "success", "done":
			complete = true
		}
	}
	if !complete {
		return nil
	}
	quote := r.Quote
	for _, path := range []string{"resolution", "video.resolution", "data.resolution", "size"} {
		if resolution, ok := normalizeVideoBillingResolution(gjson.GetBytes(body, path).String()); ok {
			if resolved, found := r.VideoPrices[resolution]; found {
				quote = resolved
			}
			break
		}
	}
	seconds := 0
	for _, path := range []string{"video.duration", "duration", "seconds", "data.duration", "data.seconds"} {
		if v := gjson.GetBytes(body, path).Int(); v > 0 && v <= 86400 {
			seconds = int(v)
			break
		}
	}
	if seconds > 0 && quote.Usage.VideoDurationSeconds != nil && *quote.Usage.VideoDurationSeconds > 0 {
		if quote.Usage.BillingMode != nil && *quote.Usage.BillingMode == string(BillingModeVideo) {
			scaleMediaQuote(&quote, float64(seconds)/float64(*quote.Usage.VideoDurationSeconds))
		}
		quote.Usage.VideoDurationSeconds = &seconds
	}
	quote.Command.RequestFingerprint = ""
	quote.Command.Normalize()
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	if err := repo.StageMediaOutcome(ctx, r.ID, &quote); err != nil {
		return err
	}
	result, err := repo.SettleMedia(ctx, r.ID, &quote.Command)
	if err != nil {
		return err
	}
	if result == nil || !result.Applied {
		return nil
	}
	s.invalidateMediaBilling(ctx, r)
	writeUsageLogBestEffort(ctx, s.usageLogRepo, &quote.Usage, "service.media_billing")
	return nil
}

func scaleMediaQuote(q *MediaBillingQuote, scale float64) {
	for _, value := range []*float64{&q.Command.BalanceCost, &q.Command.SubscriptionCost, &q.Command.APIKeyQuotaCost, &q.Command.APIKeyRateLimitCost, &q.Command.AccountQuotaCost, &q.Command.UserPlatformQuotaCost, &q.Usage.TotalCost, &q.Usage.ActualCost, &q.Usage.InputCost, &q.Usage.OutputCost, &q.Usage.ImageOutputCost, &q.Usage.ImageInputCost, &q.Usage.CacheCreationCost, &q.Usage.CacheReadCost} {
		*value = QuantizeUsageBillingAmount(*value * scale)
	}
	if q.Usage.AccountStatsCost != nil {
		value := QuantizeUsageBillingAmount(*q.Usage.AccountStatsCost * scale)
		q.Usage.AccountStatsCost = &value
	}
}

func (s *OpenAIGatewayService) startMediaReconciler() {
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok || s.accountRepo == nil || s.httpUpstream == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mediaBillingCancel = cancel
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			items, err := repo.PendingMedia(ctx, 20)
			if err != nil {
				logger.L().Error("media_billing.pending_failed", zap.Error(err))
				continue
			}
			for _, r := range items {
				if ctx.Err() != nil {
					return
				}
				checkCtx, checkCancel := context.WithTimeout(ctx, 30*time.Second)
				err := s.reconcilePendingMedia(checkCtx, r)
				checkCancel()
				if err != nil {
					logger.L().Warn("media_billing.reconcile_retry", zap.String("reservation_id", r.ID))
				}
			}
		}
	}()
}

func (s *OpenAIGatewayService) reconcilePendingMedia(ctx context.Context, r *MediaBillingReservation) error {
	if r.RefundPending {
		return s.RefundMedia(ctx, r.ID)
	}
	if r.FinalQuote != nil {
		repo, ok := s.usageBillingRepo.(MediaBillingRepository)
		if !ok {
			return ErrBillingServiceUnavailable
		}
		result, err := repo.SettleMedia(ctx, r.ID, &r.FinalQuote.Command)
		if err == nil && result != nil && result.Applied {
			s.invalidateMediaBilling(ctx, r)
			writeUsageLogBestEffort(ctx, s.usageLogRepo, &r.FinalQuote.Usage, "service.media_billing")
		}
		return err
	}
	if r.TaskID == "" {
		return fmt.Errorf("media outcome has not been confirmed")
	}
	account, err := s.accountRepo.GetByID(ctx, r.Quote.Command.AccountID)
	if err != nil {
		return err
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("media_billing_buffer", true)
	c.Request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "/v1/videos/"+r.TaskID, nil)
	var result *OpenAIForwardResult
	if r.Endpoint.IsSeedance() {
		result, err = s.ForwardSeedance(ctx, c, account, SeedanceEndpointStatus, r.TaskID, nil)
	} else {
		result, err = s.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideoStatus, r.TaskID, nil, "")
	}
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("empty video status")
	}
	body := result.BufferedResponseBody
	if len(body) == 0 {
		body = recorder.Body.Bytes()
	}
	return s.settleMediaStatus(ctx, r, body)
}
