package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type VideoWorkbenchHandler struct {
	tasks        *service.VideoWorkbenchService
	openAI       *OpenAIGatewayHandler
	refreshAuth  func(*gin.Context) error
	execute      func(*gin.Context, string)
	pollInterval time.Duration
}

func NewVideoWorkbenchHandler(tasks *service.VideoWorkbenchService, openAI *OpenAIGatewayHandler) *VideoWorkbenchHandler {
	h := &VideoWorkbenchHandler{tasks: tasks, openAI: openAI, pollInterval: 5 * time.Second}
	if openAI != nil && openAI.gatewayService != nil {
		openAI.gatewayService.SetMediaBillingVideoWorkbench(tasks)
	}
	h.execute = func(c *gin.Context, operation string) {
		switch operation {
		case "create":
			openAI.CustomVideoGeneration(c)
		case "status":
			openAI.CustomVideoStatus(c)
		case "content":
			openAI.CustomVideoContent(c)
			if !c.GetBool(service.GrokMediaContentCompleteKey) {
				_ = c.Error(errors.New("video content download incomplete"))
			}
		}
	}
	return h
}

func (h *VideoWorkbenchHandler) SetAuthRefresh(refresh func(*gin.Context) error) {
	h.refreshAuth = refresh
}

type videoWorkbenchPayload = service.VideoWorkbenchPayload

func parseVideoWorkbenchPayload(body []byte, policy service.VideoWorkbenchRuntime) (*videoWorkbenchPayload, error) {
	var p videoWorkbenchPayload
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil {
		return nil, errors.New("invalid video request")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid video request")
	}
	p.Prompt, p.Model = strings.TrimSpace(p.Prompt), strings.TrimSpace(p.Model)
	if p.Ratio == "" {
		p.Ratio = "1:1"
	}
	p.Resolution = strings.ToLower(strings.TrimSpace(p.Resolution))
	switch p.Resolution {
	case "", "auto":
		p.Resolution = "720p"
	case "480p", "720p", "1080p", "2k", "4k":
	default:
		return nil, errors.New("resolution must be auto, 480p, 720p, 1080p, 2k or 4k")
	}
	if !utf8.ValidString(p.Prompt) || utf8.RuneCountInString(p.Prompt) < 1 || utf8.RuneCountInString(p.Prompt) > 32000 || len(p.Model) == 0 || len(p.Model) > 256 || strings.ContainsAny(p.Model, "\r\n\x00") {
		return nil, errors.New("prompt and model are required; prompt must not exceed 32000 characters")
	}
	if p.Duration < 1 || p.Duration > 86400 {
		return nil, errors.New("duration must be a whole number between 1 and 86400 seconds")
	}
	parts := strings.Split(p.Ratio, ":")
	if len(parts) != 2 {
		return nil, errors.New("invalid aspect ratio")
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 10000 {
			return nil, errors.New("invalid aspect ratio")
		}
	}
	if err := p.ValidateModel(); err != nil {
		return nil, err
	}
	for _, media := range []struct {
		values []string
		limit  int
		kind   string
	}{
		{p.ReferenceImages, policy.MaxReferenceImages, "image"},
		{p.ReferenceVideos, policy.MaxReferenceVideos, "video"},
		{p.ReferenceAudios, policy.MaxReferenceAudios, "audio"},
	} {
		if len(media.values) > media.limit {
			return nil, fmt.Errorf("too many reference %ss (maximum %d)", media.kind, media.limit)
		}
		for _, raw := range media.values {
			if !validVideoWorkbenchReference(raw, media.kind) {
				return nil, fmt.Errorf("invalid reference %s upload", media.kind)
			}
		}
	}
	return &p, nil
}

func validVideoWorkbenchReference(raw, kind string) bool {
	if service.ValidVideoReferenceURL(raw) {
		return true
	}
	header, encoded, found := strings.Cut(raw, ",")
	if !found || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
		return false
	}
	claimed := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	allowed := map[string][]string{
		"image": {"image/png", "image/jpeg", "image/webp"},
		"video": {"video/mp4", "video/webm", "video/quicktime"},
		"audio": {"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/ogg", "audio/mp4", "audio/aac", "audio/x-m4a", "audio/webm", "audio/flac", "audio/x-flac"},
	}
	valid := false
	for _, candidate := range allowed[kind] {
		if claimed == candidate {
			valid = true
			break
		}
	}
	if !valid {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return false
	}
	detected := mimetype.Detect(data).String()
	if kind == "image" {
		return detected == claimed
	}
	// Container detection may report MP4/WebM/Ogg without an audio/video distinction.
	if kind == "audio" && (detected == "video/mp4" || detected == "video/webm" || detected == "application/ogg") {
		return true
	}
	return strings.HasPrefix(detected, kind+"/")
}

func (h *VideoWorkbenchHandler) Submit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	policy := h.tasks.Runtime(c.Request.Context())
	if !policy.Enabled {
		response.ErrorFrom(c, service.ErrVideoWorkbenchDisabled)
		return
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.User == nil || !service.GroupAllowsVideoWorkbench(key.Group) {
		response.ErrorFrom(c, service.ErrVideoTaskForbidden)
		return
	}
	if h.refreshAuth == nil || h.refreshAuth(c) != nil {
		response.ErrorFrom(c, service.ErrVideoTaskForbidden)
		return
	}
	key, _ = middleware.GetAPIKeyFromContext(c)
	reservedBytes := c.Request.ContentLength
	if reservedBytes <= 0 || reservedBytes > service.VideoWorkbenchMaxRequestBytes {
		reservedBytes = service.VideoWorkbenchMaxRequestBytes
	}
	release, err := h.tasks.ReservePayload(int(reservedBytes))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	transferred := false
	defer func() {
		if !transferred {
			release()
			h.openAI.refundUndispatchedWorkbench(c)
		}
	}()
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, service.VideoWorkbenchMaxRequestBytes))
	if err != nil {
		response.Error(c, http.StatusRequestEntityTooLarge, "video reference upload exceeds the request limit")
		return
	}
	if !mediaRequestFieldsUnambiguous(c.GetHeader("Content-Type"), body) {
		response.BadRequest(c, "Media request fields must be valid and unique")
		return
	}
	payload, err := parseVideoWorkbenchPayload(body, policy)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// Canonical fields are shared by video forwarding, reference pricing and audit.
	body, err = json.Marshal(payload)
	if err != nil {
		response.BadRequest(c, "invalid video request")
		return
	}
	metadata := service.VideoWorkbenchMetadata{APIKeyID: key.ID, Prompt: payload.Prompt, Model: payload.Model, Ratio: payload.Ratio, Resolution: payload.Resolution, Duration: payload.Duration, ReferenceImages: len(payload.ReferenceImages), ReferenceVideos: len(payload.ReferenceVideos), ReferenceAudios: len(payload.ReferenceAudios)}
	c.Request = c.Request.WithContext(service.WithVideoWorkbenchContext(c.Request.Context()))
	if h.openAI != nil {
		if err := h.openAI.prechargeWorkbench(c, body, service.PlatformCustom, "video"); err != nil {
			status, _, message := workbenchBillingErrorDetails(err)
			response.Error(c, status, message)
			return
		}
		if reservation := mediaPrecharge(c); reservation != nil {
			c.Request = c.Request.WithContext(service.WithMediaWorkbenchPrepayment(c.Request.Context(), reservation))
		}
	}
	task, err := h.tasks.Create(c.Request.Context(), key.UserID, metadata)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.openAI != nil && h.openAI.gatewayService != nil {
		if err := h.openAI.gatewayService.BindMediaWorkbenchTask(c.Request.Context(), mediaPrecharge(c), task.ID); err != nil {
			h.fail(task, "interrupted")
			response.Error(c, http.StatusServiceUnavailable, "Video task prepayment could not be persisted")
			return
		}
	}
	taskCtx, _, cancel := newAsyncImageContext(c, body, service.VideoWorkbenchQueueTimeout)
	transferred = true
	c.JSON(http.StatusAccepted, task.VideoWorkbenchTask)
	go func() { defer release(); h.run(task, taskCtx, body, cancel) }()
}

func (h *VideoWorkbenchHandler) run(task *service.VideoWorkbenchRecord, c *gin.Context, body []byte, cancel context.CancelFunc) {
	var completedBody []byte
	defer func() {
		if h.openAI == nil || h.openAI.gatewayService == nil {
			return
		}
		ctx, finalizeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer finalizeCancel()
		if err := h.openAI.gatewayService.FinalizeMediaWorkbenchVideo(ctx, mediaPrecharge(c), completedBody); err != nil {
			logger.L().Error("video_workbench.billing_finalization_pending", zap.String("task_id", task.ID), zap.Error(err))
		}
	}()
	defer h.openAI.refundUndispatchedWorkbench(c)
	stopPrepaymentLease := h.openAI.keepWorkbenchPrepayment(c)
	defer stopPrepaymentLease()
	defer func() {
		cancel()
		if recovered := recover(); recovered != nil {
			h.fail(task, "interrupted")
		}
	}()
	if err := h.tasks.AwaitSlot(c.Request.Context(), task); err != nil {
		if !errors.Is(err, service.ErrVideoTaskNotFound) {
			h.fail(task, "interrupted")
		}
		return
	}
	ctx, executionCancel := context.WithTimeout(service.WithVideoWorkbenchContext(context.WithoutCancel(c.Request.Context())), service.VideoWorkbenchExecutionTimeout)
	cancel()
	cancel = executionCancel
	c.Request = c.Request.WithContext(ctx)
	if h.refreshAuth == nil || h.refreshAuth(c) != nil {
		h.fail(task, "authorization_failed")
		return
	}
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := h.tasks.Heartbeat(ctx, task); err != nil {
					executionCancel()
					return
				}
			}
		}
	}()
	created := h.call(c, body, "create", "")
	if created.Code < 200 || created.Code >= 300 {
		h.fail(task, videoWorkbenchErrorCode(created.Body.Bytes(), created.Code, "request_rejected"))
		return
	}
	upstreamID := videoWorkbenchResponseID(created.Body.Bytes())
	if upstreamID == "" {
		h.fail(task, "request_rejected")
		return
	}
	state := videoWorkbenchResponseState(created.Body.Bytes())
	stateBody := created.Body.Bytes()
	for state != "completed" {
		if state == "failed" {
			// Query even an immediately failed creation so the standard idempotent
			// refund path observes and reverses its original billing receipt.
			_ = h.call(c, nil, "status", upstreamID)
			h.fail(task, videoWorkbenchErrorCode(stateBody, 0, "generation_failed"))
			return
		}
		timer := time.NewTimer(h.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			h.fail(task, "timeout")
			return
		case <-timer.C:
		}
		status := h.call(c, nil, "status", upstreamID)
		if status.Code >= 200 && status.Code < 300 {
			state = videoWorkbenchResponseState(status.Body.Bytes())
			stateBody = status.Body.Bytes()
		}
		if status.Code == http.StatusNotFound || status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden {
			h.fail(task, videoWorkbenchErrorCode(status.Body.Bytes(), status.Code, "generation_failed"))
			return
		}
	}
	completedBody = stateBody
	if h.openAI != nil && h.openAI.gatewayService != nil {
		if err := h.openAI.gatewayService.PrepareMediaWorkbenchVideoDelivery(ctx, mediaPrecharge(c), completedBody); err != nil {
			h.fail(task, "interrupted")
			return
		}
	}
	reader, writer := io.Pipe()
	defer func() { _ = reader.Close() }()
	contentDone := make(chan struct{})
	go func() {
		defer close(contentDone)
		defer func() { _ = writer.Close() }()
		defer func() {
			if recover() != nil {
				_ = writer.CloseWithError(errors.New("video download interrupted"))
			}
		}()
		stream := &videoWorkbenchStreamWriter{header: make(http.Header), pipe: writer}
		contentCtx := videoWorkbenchGatewayContext(c, nil, "content", upstreamID, stream)
		h.execute(contentCtx, "content")
		stream.finish(contentCtx)
	}()
	err := h.tasks.Complete(ctx, task, reader)
	_ = reader.CloseWithError(err)
	<-contentDone
	if err != nil {
		h.fail(task, "storage_failed")
	}
}

func (h *VideoWorkbenchHandler) fail(task *service.VideoWorkbenchRecord, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.tasks.Fail(ctx, task, code); err != nil && !errors.Is(err, service.ErrVideoTaskNotFound) {
		logger.L().Warn("video_workbench.failure_store_failed", zap.String("task_id", task.ID), zap.Error(err))
	}
}

func (h *VideoWorkbenchHandler) call(c *gin.Context, body []byte, operation, id string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	callCtx := videoWorkbenchGatewayContext(c, body, operation, id, recorder)
	h.execute(callCtx, operation)
	return recorder
}

func videoWorkbenchGatewayContext(c *gin.Context, body []byte, operation, id string, writer http.ResponseWriter) *gin.Context {
	cloned := c.Copy()
	recorderCtx, _ := gin.CreateTestContext(writer)
	cloned.Writer = recorderCtx.Writer
	cloned.Request = c.Request.Clone(c.Request.Context())
	cloned.Request.URL.Path, cloned.Request.URL.RawPath, cloned.Request.URL.RawQuery = "/v1/videos", "", ""
	cloned.Request.Header = c.Request.Header.Clone()
	cloned.Request.Header.Del("Range")
	cloned.Request.Header.Del("X-Idempotency-Key")
	cloned.Request.Header.Del("Idempotency-Key")
	cloned.Request.Header.Set("Content-Type", "application/json")
	cloned.Request.Method = http.MethodPost
	cloned.Request.Body = io.NopCloser(bytes.NewReader(body))
	cloned.Request.ContentLength = int64(len(body))
	cloned.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	if operation != "create" {
		cloned.Request.Method = http.MethodGet
		cloned.Request.URL.Path += "/" + url.PathEscape(id)
		cloned.Params = gin.Params{{Key: "request_id", Value: id}}
		if operation == "content" {
			cloned.Request.URL.Path += "/content"
		}
	}
	return cloned
}

func videoWorkbenchResponseID(body []byte) string {
	for _, path := range []string{"id", "data.id", "video.id", "request_id", "data.request_id", "task_id", "data.task_id"} {
		id := gjson.GetBytes(body, path).String()
		if id == "" || len(id) > 256 {
			continue
		}
		valid := true
		for _, r := range id {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				continue
			}
			valid = false
			break
		}
		if valid {
			return id
		}
	}
	return ""
}

func videoWorkbenchResponseState(body []byte) string {
	for _, path := range []string{"status", "data.status", "video.status", "state", "data.state"} {
		switch strings.ToLower(gjson.GetBytes(body, path).String()) {
		case "completed", "succeeded", "success", "done":
			return "completed"
		case "failed", "failure", "fail", "error", "cancelled", "canceled", "expired":
			return "failed"
		}
	}
	return "processing"
}

func videoWorkbenchErrorCode(body []byte, status int, fallback string) string {
	// Only fixed public codes leave the workbench. Provider messages may contain
	// private URLs, credentials or internal task identifiers.
	for _, path := range []string{"error.code", "error.type", "data.error.code", "code"} {
		switch strings.ToLower(gjson.GetBytes(body, path).String()) {
		case "model_not_found", "model_not_available", "model_not_exist", "unknown_model":
			return "model_unavailable"
		case "insufficient_balance", "insufficient_quota", "media_precharge_insufficient_balance":
			return "insufficient_balance"
		case "rate_limit_exceeded", "too_many_requests", "video_task_limit", "video_queue_full":
			return "rate_limited"
		case "content_policy_violation", "moderation_blocked", "content_rejected":
			return "content_rejected"
		case "invalid_api_key", "authentication_error", "permission_denied":
			return "authorization_failed"
		case "invalid_resolution", "unsupported_resolution":
			return "unsupported_resolution"
		case "invalid_duration", "unsupported_duration":
			return "unsupported_duration"
		case "invalid_aspect_ratio", "unsupported_aspect_ratio":
			return "unsupported_ratio"
		case "no_available_accounts", "no_available_channels":
			return "channel_unavailable"
		}
	}
	for _, path := range []string{"error.message", "error", "data.error.message", "message"} {
		value := gjson.GetBytes(body, path)
		if value.Type != gjson.String {
			continue
		}
		message := strings.ToLower(value.String())
		for _, rule := range []struct {
			code    string
			phrases []string
		}{
			{"upstream_pricing_unavailable", []string{"video model has no configured resolution pricing", "custom video pricing is not configured"}},
			{"channel_unavailable", []string{"no available channel", "no available account", "no enabled channel", "没有可用渠道", "无可用渠道", "没有启用的可用渠道"}},
			{"insufficient_balance", []string{"insufficient balance", "insufficient quota", "余额不足", "额度不足"}},
			{"content_rejected", []string{"content policy", "moderation rejected", "moderation blocked", "内容审核未通过"}},
			{"unsupported_resolution", []string{"unsupported resolution", "invalid resolution", "invalid video resolution", "resolution must", "resolution is not supported", "分辨率不支持", "不支持的分辨率"}},
			{"unsupported_duration", []string{"invalid duration", "unsupported duration", "duration must", "video duration must", "duration is not supported", "时长不支持", "不支持的时长"}},
			{"unsupported_ratio", []string{"invalid aspect ratio", "unsupported aspect ratio", "aspect ratio must", "aspect ratio is not supported", "不支持的宽高比"}},
			{"invalid_reference", []string{"invalid reference", "too many reference", "reference video is not supported", "reference audio is not supported", "unsupported reference", "素材无效", "参考图数量超出"}},
		} {
			for _, phrase := range rule.phrases {
				if strings.Contains(message, phrase) {
					return rule.code
				}
			}
		}
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authorization_failed"
	case http.StatusPaymentRequired:
		return "insufficient_balance"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusUnprocessableEntity:
		return "invalid_parameters"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "service_unavailable"
	}
	return fallback
}

type videoWorkbenchStreamWriter struct {
	header  http.Header
	pipe    *io.PipeWriter
	status  int
	written int64
}

func (w *videoWorkbenchStreamWriter) Header() http.Header { return w.header }
func (w *videoWorkbenchStreamWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if status != http.StatusOK {
		_ = w.pipe.CloseWithError(errors.New("video content unavailable"))
	}
}
func (w *videoWorkbenchStreamWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.status != http.StatusOK {
		return len(data), nil
	}
	n, err := w.pipe.Write(data)
	w.written += int64(n)
	return n, err
}
func (w *videoWorkbenchStreamWriter) Flush() {}

func (w *videoWorkbenchStreamWriter) finish(c *gin.Context) {
	err := c.Request.Context().Err()
	if err == nil && (w.status != http.StatusOK || c.Errors.Last() != nil) {
		err = errors.New("video content download incomplete")
	}
	if length := w.header.Get("Content-Length"); err == nil && length != "" {
		expected, parseErr := strconv.ParseInt(length, 10, 64)
		if parseErr != nil || expected != w.written {
			err = io.ErrUnexpectedEOF
		}
	}
	_ = w.pipe.CloseWithError(err)
}

func (h *VideoWorkbenchHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	tasks, err := h.tasks.List(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"settings": h.tasks.Runtime(c.Request.Context()), "tasks": tasks})
}

func (h *VideoWorkbenchHandler) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), subject.UserID, c.Param("task_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *VideoWorkbenchHandler) Download(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	file, task, err := h.tasks.Download(c.Request.Context(), subject.UserID, c.Param("task_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = file.Close() }()
	ext := ".mp4"
	if task.ContentType == "video/webm" {
		ext = ".webm"
	}
	if task.ContentType == "video/quicktime" {
		ext = ".mov"
	}
	c.Header("Content-Disposition", `attachment; filename="generated-video`+ext+`"`)
	c.Header("Content-Type", task.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, "generated-video"+ext, time.Time{}, file)
}
