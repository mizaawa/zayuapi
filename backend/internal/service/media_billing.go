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
	ID            string            `json:"id"`
	Quote         MediaBillingQuote `json:"quote"`
	GroupID       int64             `json:"group_id"`
	TaskID        string            `json:"task_id"`
	Endpoint      GrokMediaEndpoint `json:"endpoint"`
	MediaProtocol string            `json:"media_protocol,omitempty"`
	// Resolution quotes retain the price at admission even if an administrator
	// edits group/channel prices while an asynchronous job is running.
	VideoPrices     map[string]MediaBillingQuote `json:"video_prices,omitempty"`
	FinalQuote      *MediaBillingQuote           `json:"-"`
	RefundPending   bool                         `json:"-"`
	WorkbenchKind   string                       `json:"workbench_kind,omitempty"`
	QueueLeaseUntil time.Time                    `json:"-"`
	Dispatched      bool                         `json:"-"`
}

type MediaBillingRepository interface {
	ReserveMedia(context.Context, *MediaBillingReservation) error
	StageMediaOutcome(context.Context, string, *MediaBillingQuote) error
	SettleMedia(context.Context, string, *UsageBillingCommand) (*UsageBillingApplyResult, error)
	RefundMedia(context.Context, string) (*MediaBillingReservation, error)
	BindMediaTask(context.Context, string, string, GrokMediaEndpoint) error
	FindMediaTask(context.Context, string, int64, int64) (*MediaBillingReservation, error)
	PendingMedia(context.Context, int) ([]*MediaBillingReservation, error)
	RenewMediaQueue(context.Context, string, int64, int64) error
	DispatchMediaQueue(context.Context, string, int64, int64, int64) error
	RefundMediaQueue(context.Context, string, bool) (*MediaBillingReservation, error)
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
	reservation := &MediaBillingReservation{ID: copyInput.BillingRequestID, Quote: *quote, WorkbenchKind: input.WorkbenchKind, MediaProtocol: input.Account.MediaProtocol()}
	if input.APIKey.GroupID != nil {
		reservation.GroupID = *input.APIKey.GroupID
	}
	if input.Result.VideoCount > 0 && reservation.MediaProtocol != "8yes" {
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

type mediaWorkbenchPrepaymentKey struct{}

// Only server-created queue contexts carry this capability. Headers, payloads
// and client task IDs cannot grant an exemption from spending checks.
func WithMediaWorkbenchPrepayment(ctx context.Context, r *MediaBillingReservation) context.Context {
	return context.WithValue(ctx, mediaWorkbenchPrepaymentKey{}, r)
}

func MediaWorkbenchPrepaid(ctx context.Context, key *APIKey) bool {
	r, _ := ctx.Value(mediaWorkbenchPrepaymentKey{}).(*MediaBillingReservation)
	return r != nil && r.ID != "" && (r.WorkbenchKind == "image" || r.WorkbenchKind == "video") &&
		key != nil && key.GroupID != nil && r.GroupID == *key.GroupID &&
		r.Quote.Command.UserID == key.UserID && r.Quote.Command.APIKeyID == key.ID
}

func mediaPrepaidAccountID(ctx context.Context, groupID *int64) int64 {
	r, _ := ctx.Value(mediaWorkbenchPrepaymentKey{}).(*MediaBillingReservation)
	if r == nil || r.WorkbenchKind == "" || groupID == nil || r.GroupID != *groupID {
		return 0
	}
	return r.Quote.Command.AccountID
}

// Admission chooses a priced account without consuming a generation slot.
// Execution later acquires that account's normal scheduler/concurrency slot.
func (s *OpenAIGatewayService) SelectMediaPrechargeAccount(ctx context.Context, groupID *int64, platform, model string, capability OpenAIEndpointCapability, imageCapability OpenAIImagesCapability) (*Account, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(WithOpenAIProfitControlSuppressed(ctx))
	excluded := map[int64]struct{}{}
	for {
		account, err := s.selectAccountForModelWithExclusions(ctx, groupID, platform, "", model, excluded, false, 0, capability, false)
		if err != nil {
			if imageCapability == OpenAIImagesCapabilityNative {
				imageCapability, excluded = OpenAIImagesCapabilityBasic, map[int64]struct{}{}
				continue
			}
			return nil, err
		}
		if imageCapability == "" || account.SupportsOpenAIImageCapability(imageCapability) {
			return account, nil
		}
		excluded[account.ID] = struct{}{}
	}
}

func (s *OpenAIGatewayService) RenewMediaQueue(ctx context.Context, r *MediaBillingReservation) error {
	if r == nil || r.WorkbenchKind == "" {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	return repo.RenewMediaQueue(ctx, r.ID, r.Quote.Command.UserID, r.Quote.Command.APIKeyID)
}

func (s *OpenAIGatewayService) DispatchMediaQueue(ctx context.Context, r *MediaBillingReservation, accountID int64) error {
	if r == nil || r.WorkbenchKind == "" {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	return repo.DispatchMediaQueue(ctx, r.ID, r.Quote.Command.UserID, r.Quote.Command.APIKeyID, accountID)
}

func (s *OpenAIGatewayService) RefundMediaQueue(ctx context.Context, id string, expiredOnly bool) error {
	if id == "" {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	r, err := repo.RefundMediaQueue(ctx, id, expiredOnly)
	if err == nil {
		s.invalidateMediaBilling(ctx, r)
	}
	return err
}

func MediaRefundUsage(quote MediaBillingQuote) UsageLog {
	u := quote.Usage
	u.ActualCost, u.TotalCost = 0, 0
	u.InputCost, u.OutputCost, u.ImageOutputCost = 0, 0, 0
	u.ImageInputCost, u.CacheCreationCost, u.CacheReadCost = 0, 0, 0
	u.InputTokens, u.OutputTokens, u.ImageInputTokens, u.ImageOutputTokens = 0, 0, 0, 0
	u.CacheCreationTokens, u.CacheReadTokens, u.CacheCreation5mTokens, u.CacheCreation1hTokens = 0, 0, 0, 0
	zero := 0.0
	u.AccountStatsCost = &zero
	return u
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
	if keys := s.mediaBillingAPIKeys.Load(); keys != nil {
		keys.InvalidateAuthCacheByUserID(ctx, r.Quote.Command.UserID)
	}
}

func (s *OpenAIGatewayService) SetMediaBillingAPIKeyService(keys *APIKeyService) {
	s.mediaBillingAPIKeys.Store(keys)
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
	// 8yes prices the exact requested model, including its resolution suffix.
	// Its status response may describe the upstream's different internal model.
	if r.MediaProtocol != "8yes" {
		for _, path := range []string{"resolution", "video.resolution", "data.resolution", "size"} {
			if resolution, ok := normalizeVideoBillingResolution(gjson.GetBytes(body, path).String()); ok {
				if resolved, found := r.VideoPrices[resolution]; found {
					quote = resolved
				}
				break
			}
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
		}
		return err
	}
	if r.TaskID == "" {
		if r.WorkbenchKind != "" && !r.Dispatched {
			return s.RefundMediaQueue(ctx, r.ID, true)
		}
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
