package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GrokImages handles xAI image generation/editing through Grok groups.
func (h *OpenAIGatewayHandler) GrokImages(c *gin.Context) {
	endpoint := service.GrokMediaEndpointImagesGenerations
	if strings.Contains(c.Request.URL.Path, "/images/edits") {
		endpoint = service.GrokMediaEndpointImagesEdits
	}
	h.handleGrokMedia(c, endpoint, "", service.PlatformGrok)
}

// GrokVideoGeneration handles xAI video generation through Grok groups.
func (h *OpenAIGatewayHandler) GrokVideoGeneration(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideosGenerations, "", service.PlatformGrok)
}

// CustomVideoGeneration handles the standard asynchronous OpenAI-compatible
// video creation endpoint through Custom API-key accounts.
func (h *OpenAIGatewayHandler) CustomVideoGeneration(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideosGenerations, "", service.PlatformCustom)
}

// GrokVideoEdit handles asynchronous xAI video edits through Grok groups.
func (h *OpenAIGatewayHandler) GrokVideoEdit(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideosEdits, "", service.PlatformGrok)
}

// GrokVideoExtension handles asynchronous xAI video extensions through Grok groups.
func (h *OpenAIGatewayHandler) GrokVideoExtension(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideosExtensions, "", service.PlatformGrok)
}

// GrokVideoStatus handles xAI video status retrieval through Grok groups.
func (h *OpenAIGatewayHandler) GrokVideoStatus(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoStatus, c.Param("request_id"), service.PlatformGrok)
}

func (h *OpenAIGatewayHandler) CustomVideoStatus(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoStatus, c.Param("request_id"), service.PlatformCustom)
}

func (h *OpenAIGatewayHandler) CompositeVideoStatus(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoStatus, c.Param("request_id"), service.PlatformCustom)
}

// GrokVideoContent proxies downloadable video content through the task's upstream account.
func (h *OpenAIGatewayHandler) GrokVideoContent(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoContent, c.Param("request_id"), service.PlatformGrok)
}

func (h *OpenAIGatewayHandler) CustomVideoContent(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoContent, c.Param("request_id"), service.PlatformCustom)
}

func (h *OpenAIGatewayHandler) CompositeVideoContent(c *gin.Context) {
	h.handleGrokMedia(c, service.GrokMediaEndpointVideoContent, c.Param("request_id"), service.PlatformCustom)
}

func (h *OpenAIGatewayHandler) handleGrokMedia(c *gin.Context, endpoint service.GrokMediaEndpoint, requestID, targetPlatform string) {
	c.Set("media_billing_buffer", true)
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}

	reqLog := requestLogger(
		c,
		"handler.openai_gateway.grok_media",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("endpoint", string(endpoint)),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var err error
	if endpoint.RequiresRequestBody() {
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
				return
			}
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
			return
		}
		if len(body) == 0 {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
			return
		}
	}

	contentType := c.GetHeader("Content-Type")
	requestInfo := service.GrokMediaRequestInfo{}
	if endpoint.IsSeedance() && endpoint == service.SeedanceEndpointCreate {
		requestInfo, err = service.ParseSeedanceRequest(body)
		if err != nil {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	} else {
		requestInfo = service.ParseGrokMediaRequest(contentType, body)
	}
	requestModel := requestInfo.Model
	routingModel := requestModel
	if targetPlatform == service.PlatformGrok {
		routingModel = service.NormalizeGrokMediaModelForEndpoint(endpoint, requestModel, requestInfo.HasInputImage())
	}
	channelMapping := service.ChannelMappingResult{MappedModel: routingModel}
	if endpoint.IsGenerationRequest() && targetPlatform != service.PlatformCustom {
		channelMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, routingModel)
		if mappedModel := strings.TrimSpace(channelMapping.MappedModel); mappedModel != "" {
			routingModel = mappedModel
			if endpoint.RequiresRequestBody() && channelMapping.Mapped {
				body = h.gatewayService.ReplaceModelInBody(body, mappedModel)
			}
		}
	}
	if endpoint.IsGenerationRequest() && strings.TrimSpace(requestModel) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if endpoint.IsVideoLookupRequest() && strings.TrimSpace(requestID) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request_id is required")
		return
	}
	if (targetPlatform == service.PlatformCustom || targetPlatform == service.PlatformComposite) &&
		endpoint.IsVideoGenerationRequest() && !endpoint.IsSeedance() {
		validationResolution := requestInfo.Resolution
		if !requestInfo.ResolutionValid {
			// ParseGrokMediaRequest retains Grok's historical model/480p
			// fallback in Resolution. Custom pricing diagnostics must instead
			// reflect the absence/invalidity of a client-supplied resolution.
			validationResolution = ""
		}
		if err := h.gatewayService.ValidateCustomVideoPricing(
			c.Request.Context(), apiKey, requestModel, validationResolution,
		); err != nil {
			// Custom/Composite video creation must reach the upstream even when
			// local pricing is not configured. Pricing is resolved again after a
			// successful task response; this check is diagnostic only and must not
			// turn an upstream-compatible request into a local 400.
			reqLog.Warn("custom_video.pricing_unavailable",
				zap.Error(err),
				zap.String("resolution", validationResolution),
			)
		}
	}

	reqLog = reqLog.With(zap.String("model", requestModel))
	setOpsRequestContext(c, requestModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	if endpoint.IsGenerationRequest() {
		if endpoint.IsImageGenerationRequest() && !service.GroupAllowsImageGeneration(apiKey.Group) {
			h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
			return
		}
		if moderationBody := requestInfo.ModerationBody(); len(moderationBody) > 0 {
			decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, requestModel, moderationBody)
			if decision != nil && !decision.AllowNextStage {
				h.openAISecurityAuditError(c, decision)
				return
			}
		}
		if !endpoint.IsSeedance() {
			imageReleaseFunc, acquired := h.acquireImageGenerationSlot(c, streamStarted)
			if !acquired {
				return
			}
			if imageReleaseFunc != nil {
				defer imageReleaseFunc()
			}
		}
	}

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	var workbenchBilling *imageWorkbenchFailureBilling
	if endpoint.IsImageGenerationRequest() {
		workbenchBilling = newImageWorkbenchFailureBilling(h, c, apiKey, subscription, requestModel, requestInfo.Size, requestInfo.N, channelMapping, body)
	}
	defer workbenchBilling.finish()
	videoTransferred := false
	defer func() {
		if endpoint.IsVideoGenerationRequest() && !videoTransferred {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 15*time.Second)
			defer cancel()
			if err := h.gatewayService.RefundMedia(ctx, mediaReservationID(c)); err != nil {
				reqLog.Error("media_billing.refund_failed", zap.Error(err))
			}
		}
	}()
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	sessionSeed := body
	if len(sessionSeed) == 0 && strings.TrimSpace(requestID) != "" {
		sessionSeed = []byte(requestID)
	}
	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, sessionSeed)
	boundLookupAccountID := int64(0)
	var boundLookupAccount *service.Account
	if endpoint.IsVideoLookupRequest() {
		sessionHash = service.GrokMediaVideoRequestSessionHash(requestID, subject.UserID, apiKey.ID)
		boundLookupAccount, err = h.gatewayService.ResolveGrokMediaVideoRequestAccountDetails(
			c.Request.Context(), apiKey.GroupID, requestID, subject.UserID, apiKey.ID,
		)
		if err != nil || boundLookupAccount == nil || boundLookupAccount.Platform != targetPlatform {
			reqLog.Info("grok_media.video_lookup_owner_binding_missing", zap.Error(err))
			h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video request not found")
			return
		}
		boundLookupAccountID = boundLookupAccount.ID
	}
	if targetPlatform != service.PlatformGrok && targetPlatform != service.PlatformCustom && targetPlatform != service.PlatformOpenAI {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Videos API is not supported for this platform")
		return
	}
	if endpoint.IsGenerationRequest() {
		if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
			reqLog.Info("grok_media.billing_eligibility_check_failed", zap.Error(err))
			status, code, message, retryAfter := billingErrorDetails(err)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.errorResponse(c, status, code, message)
			return
		}
	}
	// Grok 媒体（图片/视频生成与视频查询）按媒体倍率计费，不在 token 利润门
	// 范围内：显式豁免，防止 service 层防御性装门按文本 D 误过滤媒体请求，
	// 也防止已计费的在途视频任务因绑定账号被门排除而查询返回伪 404。
	requestCtx := service.WithOpenAIProfitControlSuppressed(c.Request.Context())
	if boundLookupAccountID > 0 {
		defer func() {
			refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(requestCtx), time.Second)
			defer cancel()
			if bindErr := h.gatewayService.BindGrokMediaVideoRequestAccount(
				refreshCtx, apiKey.GroupID, requestID, subject.UserID, apiKey.ID, boundLookupAccountID,
			); bindErr != nil {
				reqLog.Warn("grok_media.refresh_video_request_account_binding_failed", zap.Error(bindErr))
			}
		}()
	}
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	mediaEligibilityRejected := false
	switchCount := 0
	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	routingStart := time.Now()
	requiredCapability := mediaRequiredCapability(endpoint, targetPlatform)

	for {
		if failoverClientGone(c) {
			return
		}
		var selection *service.AccountSelectionResult
		var scheduleDecision service.OpenAIAccountScheduleDecision
		var err error
		if boundLookupAccountID > 0 {
			if targetPlatform == service.PlatformCustom {
				selection, scheduleDecision, err = h.gatewayService.SelectBoundCustomVideoAccount(
					requestCtx, apiKey.GroupID, boundLookupAccount,
				)
			} else {
				selection, scheduleDecision, err = h.gatewayService.SelectBoundAccountWithSchedulerForCapability(
					requestCtx,
					apiKey.GroupID,
					boundLookupAccountID,
					routingModel,
					service.OpenAIUpstreamTransportHTTPSSE,
					requiredCapability,
					targetPlatform,
				)
			}
		} else {
			selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForCapability(
				requestCtx,
				apiKey.GroupID,
				"",
				sessionHash,
				routingModel,
				failedAccountIDs,
				service.OpenAIUpstreamTransportHTTPSSE,
				requiredCapability,
				false,
				false,
				false,
				targetPlatform,
			)
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("grok_media.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("grok_media.account_select_failed",
				zap.Error(err),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if endpoint.IsGenerationRequest() && errors.Is(err, service.ErrNoAvailableAccounts) &&
				(len(failedAccountIDs) == 0 || (mediaEligibilityRejected && lastFailoverErr == nil)) {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.errorResponse(c, http.StatusServiceUnavailable, "media_no_eligible_account", "No eligible media accounts")
				return
			}
			if len(failedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, routingModel, targetPlatform)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, false)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
			}
			return
		}
		if selection == nil || selection.Account == nil {
			if endpoint.IsGenerationRequest() {
				markOpsRoutingCapacityLimited(c)
				h.errorResponse(c, http.StatusServiceUnavailable, "media_no_eligible_account", "No eligible media accounts")
				return
			}
			cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, routingModel, targetPlatform)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.errorResponse(c, cls.Status, cls.ErrType, cls.Message)
			return
		}
		reqLog.Debug("grok_media.account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)

		account := selection.Account
		if endpoint.IsGenerationRequest() && targetPlatform == service.PlatformGrok {
			eligible, eligibilityReason, eligibilityErr := h.ensureGrokMediaAccountEligibility(requestCtx, account)
			if !eligible {
				mediaEligibilityRejected = true
				failedAccountIDs[account.ID] = struct{}{}
				reqLog.Warn("grok_media.account_eligibility_rejected",
					zap.Int64("account_id", account.ID),
					zap.String("reason", eligibilityReason),
					zap.Bool("probe_failed", eligibilityErr != nil),
				)
				if switchCount >= maxAccountSwitches {
					markOpsRoutingCapacityLimited(c)
					h.errorResponse(c, http.StatusServiceUnavailable, "media_no_eligible_account", "No eligible media accounts")
					return
				}
				switchCount++
				continue
			}
		}
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, false, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// 媒体路径已显式豁免利润门（suppress 标记），此分支仅防御性兜底，
			// 同样受否决上限约束。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		writerSizeBeforeForward := c.Writer.Size()
		if err := workbenchBilling.attempt(account); err != nil {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			status, code, message, _ := billingErrorDetails(err)
			h.errorResponse(c, status, code, message)
			return
		}
		if endpoint.IsVideoGenerationRequest() {
			if err := h.prechargeVideo(c, apiKey, subscription, account, endpoint, requestInfo, channelMapping, body); err != nil {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				status, code, message, _ := billingErrorDetails(err)
				h.errorResponse(c, status, code, message)
				return
			}
		}
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			if endpoint.IsSeedance() {
				return h.gatewayService.ForwardSeedance(requestCtx, c, account, endpoint, requestID, body)
			}
			return h.gatewayService.ForwardGrokMedia(requestCtx, c, account, endpoint, requestID, body, contentType)
		}()
		workbenchBilling.observe(result)

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if failoverClientGone(c) {
					reqLog.Info("grok_media.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, grokMediaScheduleModel(account, routingModel, nil), false, nil)
				}
				// Creating an asynchronous Custom video is not replay-safe: a
				// transport error, 429, or 5xx may occur after the upstream has
				// accepted the task. Retrying another account can create and bill
				// duplicate videos, so always terminate after the first attempt.
				if endpoint.IsVideoGenerationRequest() && (targetPlatform == service.PlatformCustom || endpoint.IsSeedance()) {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				if endpoint.IsVideoLookupRequest() {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				if failoverErr.RetryableOnSameAccount {
					retryLimit := account.GetPoolModeRetryCount()
					if sameAccountRetryCount[account.ID] < retryLimit {
						sameAccountRetryCount[account.ID]++
						reqLog.Warn("grok_media.pool_mode_same_account_retry",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
							zap.Int("retry_limit", retryLimit),
							zap.Int("retry_count", sameAccountRetryCount[account.ID]),
						)
						select {
						case <-requestCtx.Done():
							return
						case <-time.After(sameAccountRetryDelay):
						}
						continue
					}
				}
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				reqLog.Warn("grok_media.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, grokMediaScheduleModel(account, routingModel, nil), false, nil)
			if !service.IsResponseCommitted(c) && c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("grok_media.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, grokMediaScheduleModel(account, routingModel, result), true, nil)
		if endpoint == service.GrokMediaEndpointVideoStatus || endpoint == service.SeedanceEndpointStatus {
			handled, settleErr := h.gatewayService.ReconcileMediaTask(requestCtx, requestID, subject.UserID, apiKey.ID, result.BufferedResponseBody)
			if settleErr != nil {
				h.errorResponse(c, http.StatusBadGateway, "billing_error", "Video settlement is pending; retry the status request")
				return
			}
			if !handled && service.GrokMediaVideoStatusFailed(result.BufferedResponseBody) {
				refundCtx, refundCancel := context.WithTimeout(context.WithoutCancel(requestCtx), 10*time.Second)
				_, refunded, refundErr := h.gatewayService.RefundFailedGrokMediaVideoTask(
					refundCtx, apiKey.GroupID, requestID, subject.UserID, apiKey.ID,
				)
				if refundErr != nil {
					refundCancel()
					reqLog.Error("custom_video.failed_task_refund_failed",
						zap.String("request_id", requestID),
						zap.Error(refundErr),
					)
					h.errorResponse(c, http.StatusBadGateway, "billing_error", "Failed to refund video task")
					return
				}
				if refunded && h.apiKeyService != nil {
					h.apiKeyService.InvalidateAuthCacheByKey(refundCtx, apiKey.Key)
				}
				refundCancel()
			}
			if !h.gatewayService.WriteBufferedGrokMediaResponse(c, result) {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to write video task response")
			}
			return
		}
		if endpoint.IsVideoGenerationRequest() && mediaPrecharge(c) != nil {
			finalizeCtx, finalizeCancel := context.WithTimeout(context.WithoutCancel(requestCtx), 20*time.Second)
			defer finalizeCancel()
			reservation := mediaPrecharge(c)
			// A successful upstream response may already represent paid work.
			// Retain the reservation if persisting its task ID fails; refunding
			// here would make an accepted generation free during a DB outage.
			videoTransferred = true
			if result.ResponseID == "" {
				videoTransferred = !service.GrokMediaVideoStatusFailed(result.BufferedResponseBody)
				reqLog.Error("media_billing.accepted_without_task_id", zap.String("reservation_id", reservation.ID))
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Video response is missing a task ID")
				return
			}
			if err := h.gatewayService.BindMediaTask(finalizeCtx, reservation, result.ResponseID, endpoint); err != nil {
				reqLog.Error("media_billing.task_binding_pending", zap.String("reservation_id", reservation.ID), zap.String("task_id", result.ResponseID))
				h.errorResponse(c, http.StatusBadGateway, "billing_error", "Failed to persist video billing")
				return
			}
			if err := h.gatewayService.BindGrokMediaVideoRequestAccount(finalizeCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID); err != nil {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to persist video task")
				return
			}
			if _, err := h.gatewayService.ReconcileMediaTask(finalizeCtx, result.ResponseID, subject.UserID, apiKey.ID, result.BufferedResponseBody); err != nil {
				reqLog.Error("media_billing.immediate_settlement_failed", zap.Error(err))
			}
			h.gatewayService.WriteBufferedGrokMediaResponse(c, result)
		} else if endpoint == service.SeedanceEndpointCreate {
			if err := h.gatewayService.BindGrokMediaVideoRequestAccount(
				requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
			); err != nil {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to persist Seedance task")
				return
			}
			if !h.gatewayService.WriteBufferedGrokMediaResponse(c, result) {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to write Seedance task response")
				return
			}
		} else if targetPlatform == service.PlatformCustom && endpoint.IsVideoGenerationRequest() {
			if err := h.finalizeCustomVideoCreation(
				c, requestCtx, reqLog, apiKey, subject, subscription, account, result,
				requestModel, channelMapping, body,
			); err != nil {
				reqLog.Error("custom_video.finalize_failed", zap.Error(err))
				if !service.IsResponseCommitted(c) {
					h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Failed to persist video task")
				}
				return
			}
		} else {
			if endpoint.IsGenerationRequest() && strings.TrimSpace(result.ResponseID) != "" {
				if err := h.gatewayService.BindGrokMediaVideoRequestAccount(
					requestCtx, apiKey.GroupID, result.ResponseID, subject.UserID, apiKey.ID, account.ID,
				); err != nil {
					reqLog.Warn("grok_media.bind_video_request_account_failed",
						zap.Int64("account_id", account.ID),
						zap.String("request_id", result.ResponseID),
						zap.Error(err),
					)
				}
			}
			if shouldRecordGrokMediaUsage(endpoint, requestModel) {
				result = workbenchBilling.settledByNormalPath(result)
				recordGrokMediaUsage(c, h, reqLog, apiKey, subject, subscription, account, result, requestModel, channelMapping, body, requestID)
			}
			if endpoint.IsVideoGenerationRequest() {
				h.gatewayService.WriteBufferedGrokMediaResponse(c, result)
			}
		}
		reqLog.Debug("grok_media.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) finalizeCustomVideoCreation(
	c *gin.Context,
	requestCtx context.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	requestModel string,
	channelMapping service.ChannelMappingResult,
	body []byte,
) error {
	if h == nil || h.gatewayService == nil || c == nil || apiKey == nil || account == nil || result == nil {
		return errors.New("custom video creation result is incomplete")
	}
	taskID := strings.TrimSpace(result.ResponseID)
	if taskID == "" {
		return errors.New("custom video upstream response is missing id")
	}

	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(requestCtx), 10*time.Second)
	defer cancel()
	if err := h.gatewayService.BindGrokMediaVideoRequestAccount(
		finalizeCtx, apiKey.GroupID, taskID, subject.UserID, apiKey.ID, account.ID,
	); err != nil {
		return err
	}

	channelUsageFields := channelMapping.ToUsageFields(clientRequestedModel(c, requestModel), result.UpstreamModel)
	var receipt *service.UsageBillingCommand
	if err := h.gatewayService.RecordUsage(finalizeCtx, &service.OpenAIRecordUsageInput{
		Result:             result,
		APIKey:             apiKey,
		User:               apiKey.User,
		Account:            account,
		Subscription:       subscription,
		InboundEndpoint:    GetInboundEndpoint(c),
		UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
		UserAgent:          c.GetHeader("User-Agent"),
		IPAddress:          ip.GetClientIP(c),
		RequestPayloadHash: service.HashUsageRequestPayload(body),
		APIKeyService:      h.apiKeyService,
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
		SessionID:          service.ExtractClientSessionID(c),
		ChannelUsageFields: channelUsageFields,
		BillingRequestID:   service.GrokMediaVideoBillingRequestID(taskID),
		BillingReceipt:     &receipt,
	}); err != nil {
		return err
	}

	if receipt != nil {
		record := &service.GrokMediaVideoBillingRecord{Command: *receipt}
		if apiKey.GroupID != nil {
			record.GroupID = *apiKey.GroupID
		}
		if err := h.gatewayService.BindGrokMediaVideoBillingRecord(
			finalizeCtx, apiKey.GroupID, taskID, subject.UserID, apiKey.ID, record,
		); err != nil {
			refunded, refundErr := h.gatewayService.ReverseGrokMediaVideoBilling(finalizeCtx, taskID, record)
			if refunded && h.apiKeyService != nil {
				h.apiKeyService.InvalidateAuthCacheByKey(finalizeCtx, apiKey.Key)
			}
			if refundErr != nil {
				return fmt.Errorf("bind video billing record: %v; compensate charge: %w", err, refundErr)
			}
			return fmt.Errorf("bind video billing record: %w", err)
		}
	}

	if !h.gatewayService.WriteBufferedGrokMediaResponse(c, result) {
		return errors.New("custom video response is no longer writable")
	}
	reqLog.Debug("custom_video.creation_committed", zap.String("request_id", taskID))
	return nil
}

func (h *OpenAIGatewayHandler) ensureGrokMediaAccountEligibility(ctx context.Context, account *service.Account) (bool, string, error) {
	if account == nil {
		return false, "missing_account", errors.New("grok media account is required")
	}
	eligible, reason := account.GrokMediaGenerationEligibility()
	if eligible || reason != "billing_unobserved" {
		return eligible, reason, nil
	}
	if h == nil || h.grokMediaEligibilityProber == nil {
		return false, "billing_probe_unavailable", errors.New("grok media eligibility probe is not configured")
	}
	return h.grokMediaEligibilityProber.ProbeMediaEligibility(ctx, account.ID)
}

func grokMediaRequiredCapability(endpoint service.GrokMediaEndpoint) service.OpenAIEndpointCapability {
	if endpoint.IsGenerationRequest() {
		return service.OpenAIEndpointCapabilityGrokMediaGeneration
	}
	return ""
}

func mediaRequiredCapability(endpoint service.GrokMediaEndpoint, platform string) service.OpenAIEndpointCapability {
	if endpoint.IsSeedance() {
		return service.OpenAIEndpointCapabilitySeedance
	}
	if platform != service.PlatformGrok {
		return ""
	}
	return grokMediaRequiredCapability(endpoint)
}

func grokMediaScheduleModel(account *service.Account, routingModel string, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if account == nil {
		return strings.TrimSpace(routingModel)
	}
	return account.GetMappedModel(routingModel)
}

func shouldRecordGrokMediaUsage(endpoint service.GrokMediaEndpoint, requestModel string) bool {
	return endpoint.IsGenerationRequest() && strings.TrimSpace(requestModel) != ""
}

func recordGrokMediaUsage(
	c *gin.Context,
	h *OpenAIGatewayHandler,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	subject middleware2.AuthSubject,
	subscription *service.UserSubscription,
	account *service.Account,
	result *service.OpenAIForwardResult,
	requestModel string,
	channelMapping service.ChannelMappingResult,
	body []byte,
	requestID string,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	payloadForHash := body
	if len(payloadForHash) == 0 && strings.TrimSpace(requestID) != "" {
		payloadForHash = []byte(requestID)
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	// OriginalModel 记录客户端请求的模型：composite 分组下 body 已被改写为具体模型，
	// 公开别名需从 context 取回，与其他端点的用量归因口径一致（计费不受影响：
	// BillingModelSource 为空不会触发来源覆盖）。
	channelUsageFields := channelMapping.ToUsageFields(clientRequestedModel(c, requestModel), result.UpstreamModel)
	billingRequestID := imageWorkbenchBillingRequestID(c)
	reservationID := mediaReservationID(c)
	h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: service.HashUsageRequestPayload(payloadForHash),
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			ChannelUsageFields: channelUsageFields,
			BillingRequestID:   billingRequestID,
			MediaReservationID: reservationID,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.grok_media"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestModel),
				zap.Int64("account_id", account.ID),
			).Error("grok_media.record_usage_failed", zap.Error(err))
			reqLog.Debug("grok_media.record_usage_failed", zap.Error(err))
		}
	})
}
