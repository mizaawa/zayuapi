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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AsyncImageHandler struct {
	tasks                *service.ImageTaskService
	openAI               *OpenAIGatewayHandler
	execute              func(platform string, c *gin.Context)
	precharge            func(*gin.Context, []byte, string, string) error
	refreshWorkbenchAuth func(*gin.Context) error
}

const imageOwnerEmailHeader = "X-Sub2API-User-Email"
const imageWorkbenchHeader = "X-Sub2API-Image-Workbench"

func isImageWorkbenchRequest(c *gin.Context) bool {
	return c.GetHeader(imageWorkbenchHeader) == "true" || c.GetHeader(imageWorkbenchHeader) == "1"
}

func NewAsyncImageHandler(tasks *service.ImageTaskService, openAI *OpenAIGatewayHandler) *AsyncImageHandler {
	h := &AsyncImageHandler{tasks: tasks, openAI: openAI}
	h.execute = h.executeWithGateway
	if openAI != nil {
		h.precharge = openAI.prechargeWorkbench
	}
	return h
}

func (h *AsyncImageHandler) SetWorkbenchAuthRefresh(refresh func(*gin.Context) error) {
	h.refreshWorkbenchAuth = refresh
}

// enabled reports whether the async image task feature is available. Object
// storage is the enablement gate: without it the endpoints are fully disabled
// so that large base64 results never land in Redis.
func (h *AsyncImageHandler) enabled() bool {
	return h != nil && h.tasks != nil && h.tasks.Enabled()
}

// pollable reports whether task lookups can be served. It is deliberately weaker
// than enabled(): results already written to Redis stay readable after the
// feature is switched off, so an in-flight task is never stranded.
func (h *AsyncImageHandler) pollable() bool {
	return h != nil && h.tasks != nil && h.tasks.Pollable()
}

// Submit accepts the same payload as the synchronous Images endpoint and
// returns before the upstream image generation begins.
func (h *AsyncImageHandler) Submit(c *gin.Context) {
	if isImageWorkbenchRequest(c) && h != nil && h.tasks != nil && !h.tasks.WorkbenchRuntime(c.Request.Context()).Enabled {
		imageTaskError(c, service.ErrImageWorkbenchDisabled)
		return
	}
	enabled := h.enabled()
	if isImageWorkbenchRequest(c) && h != nil && h.tasks != nil {
		enabled = h.tasks.WorkbenchEnabled(c.Request.Context())
	}
	if !enabled {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	ownerEmail, ok := imageOwnerEmail(c, apiKey)
	if !ok {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	platform := ""
	if apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	// Composite groups are resolved by route middleware before this handler.
	// Use that concrete target so async tasks share synchronous image routing
	// and its billing/quota accounting path.
	if resolvedPlatform, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context()); resolved {
		platform = resolvedPlatform
	}
	if platform != service.PlatformOpenAI && platform != service.PlatformGrok && platform != service.PlatformCustom {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "Images API is not supported for this platform")
		return
	}
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		imageTaskJSONError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if isImageWorkbenchRequest(c) && (apiKey.Group == nil || !apiKey.Group.ImageWorkbenchEnabled) {
		imageTaskJSONError(c, http.StatusForbidden, "permission_error", "This group is not enabled for the image workbench")
		return
	}
	if h == nil || h.tasks == nil || h.execute == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			imageTaskJSONError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if !mediaRequestFieldsUnambiguous(c.GetHeader("Content-Type"), body) {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Media request fields must be valid and unique")
		return
	}
	if asyncImageRequestStreams(c.GetHeader("Content-Type"), body) {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "streaming image requests cannot be submitted as asynchronous tasks")
		return
	}
	var metadata *service.ImageWorkbenchMetadata
	if isImageWorkbenchRequest(c) {
		policy := h.tasks.WorkbenchRuntime(c.Request.Context())
		metadata, err = parseImageWorkbenchMetadata(c.GetHeader("Content-Type"), body, policy.MaxImageSizeMB, policy.ReferenceImageLimit())
		if err != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	if err := h.validateRequest(c, platform, body); err != nil {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if !h.checkSecurityAuditBeforeSubmit(c, apiKey, platform, body) {
		return
	}

	releasePayload := func() {}
	timeout := h.tasks.ExecutionTimeout()
	if metadata != nil {
		releasePayload, err = h.tasks.ReserveWorkbenchPayload(len(body))
		if err != nil {
			imageTaskError(c, err)
			return
		}
		timeout = service.ImageWorkbenchQueueTimeout
	}
	if h.precharge != nil {
		if err := h.precharge(c, body, platform, "image"); err != nil {
			releasePayload()
			status, code, message := workbenchBillingErrorDetails(err)
			imageTaskJSONError(c, status, code, message)
			return
		}
		if reservation := mediaPrecharge(c); reservation != nil {
			c.Request = c.Request.WithContext(service.WithMediaWorkbenchPrepayment(c.Request.Context(), reservation))
		}
	}
	transferred := false
	defer func() {
		if !transferred {
			h.openAI.refundUndispatchedWorkbench(c)
		}
	}()
	taskCtx, recorder, cancel := newAsyncImageContext(c, body, timeout)
	owner := service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID, UserEmail: ownerEmail, IsAdmin: apiKey.User != nil && apiKey.User.IsAdmin()}
	var task *service.ImageTask
	if metadata != nil {
		task, err = h.tasks.CreateWorkbench(c.Request.Context(), owner, *metadata)
	} else {
		task, err = h.tasks.Create(c.Request.Context(), owner)
	}
	if err != nil {
		cancel()
		releasePayload()
		imageTaskError(c, err)
		return
	}

	pollURL := imageTaskPollURL(c.Request.URL.Path, task.ID)
	c.Header("Cache-Control", "no-store")
	c.Header("Location", pollURL)
	c.Header("Retry-After", "3")
	c.JSON(http.StatusAccepted, gin.H{
		"id":         task.ID,
		"task_id":    task.TaskID,
		"object":     task.Object,
		"status":     task.Status,
		"created_at": task.CreatedAt,
		"expires_at": task.ExpiresAt,
		"poll_url":   pollURL,
		"workbench":  task.Workbench,
	})

	transferred = true
	go func() {
		defer releasePayload()
		defer h.openAI.refundUndispatchedWorkbench(taskCtx)
		stopPrepaymentLease := h.openAI.keepWorkbenchPrepayment(taskCtx)
		defer stopPrepaymentLease()
		if metadata != nil {
			authorize := func(ctx context.Context) (service.ImageTaskOwner, error) {
				taskCtx.Request = taskCtx.Request.WithContext(ctx)
				if h.refreshWorkbenchAuth != nil {
					if err := h.refreshWorkbenchAuth(taskCtx); err != nil {
						return owner, err
					}
				} else if h.openAI != nil {
					return owner, service.ErrImageTaskUnavailable
				}
				fresh, ok := middleware2.GetAPIKeyFromContext(taskCtx)
				if !ok || fresh == nil || fresh.User == nil || fresh.ID != owner.APIKeyID || fresh.UserID != owner.UserID || service.NormalizeImageOwnerEmail(fresh.User.Email) != owner.UserEmail {
					return owner, service.ErrImageTaskForbidden
				}
				current := owner
				current.IsAdmin = fresh.User.IsAdmin()
				return current, nil
			}
			claimed, err := h.tasks.AwaitWorkbenchSlot(taskCtx.Request.Context(), task.ID, authorize)
			if err != nil {
				cancel()
				if !errors.Is(err, service.ErrImageTaskNotFound) {
					status := infraerrors.Code(err)
					if status < 400 {
						status = http.StatusGatewayTimeout
					}
					h.failTask(task.ID, status, imageTaskErrorPayload("api_error", "queued image task could not start"))
				}
				return
			}
			checkCtx, checkCancel := context.WithTimeout(context.WithoutCancel(taskCtx.Request.Context()), 5*time.Second)
			current, err := authorize(checkCtx)
			checkCancel()
			if err != nil || (claimed.IsAdmin && !current.IsAdmin) {
				cancel()
				h.failTask(task.ID, http.StatusForbidden, imageTaskErrorPayload("permission_error", "queued task authorization was revoked"))
				return
			}
			// Queue waiting does not consume the generation timeout. The server
			// task ID also supplies a unique, trusted billing identity per image.
			executionCtx, executionCancel := context.WithTimeout(context.WithoutCancel(taskCtx.Request.Context()), h.tasks.ExecutionTimeout())
			cancel()
			cancel = executionCancel
			taskCtx.Request = taskCtx.Request.WithContext(executionCtx)
			taskCtx.Set(imageWorkbenchTaskIDContextKey, task.ID)
		}
		h.run(task.ID, platform, taskCtx, recorder, cancel)
	}()
}

// ListWorkbench uses the panel session so task recovery works across keys and
// devices without retaining API credentials in browser storage.
func (h *AsyncImageHandler) ListWorkbench(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.pollable() {
		response.ErrorFrom(c, service.ErrImageTaskUnavailable)
		return
	}
	tasks, err := h.tasks.ListWorkbench(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	policy := h.tasks.WorkbenchRuntime(c.Request.Context())
	role, _ := middleware2.GetUserRoleFromContext(c)
	response.Success(c, gin.H{
		"enabled":              h.tasks.WorkbenchEnabled(c.Request.Context()),
		"max_concurrent":       policy.Limit(role == service.RoleAdmin),
		"max_image_size_mb":    policy.MaxImageSizeMB,
		"max_reference_images": policy.ReferenceImageLimit(),
		"user_max_concurrent":  policy.MaxConcurrent,
		"admin_exempt":         policy.AdminExempt,
		"retention_seconds":    int(policy.Retention().Seconds()),
		"tutorial_url":         policy.TutorialURL,
		"documentation_url":    policy.DocumentationURL,
		"tasks":                tasks,
	})
}

func (h *AsyncImageHandler) DeleteWorkbench(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.pollable() {
		response.ErrorFrom(c, service.ErrImageTaskUnavailable)
		return
	}
	if err := h.tasks.DeleteWorkbench(c.Request.Context(), subject.UserID, c.Param("task_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *AsyncImageHandler) DownloadWorkbenchImage(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if !h.pollable() {
		response.ErrorFrom(c, service.ErrImageTaskUnavailable)
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil || index < 0 {
		response.BadRequest(c, "Invalid image index")
		return
	}
	data, contentType, err := h.tasks.DownloadWorkbenchImage(c.Request.Context(), subject.UserID, c.Param("task_id"), index)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	extension := ".png"
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	}
	c.Header("Content-Disposition", `attachment; filename="generated-image-`+strconv.Itoa(index+1)+extension+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, contentType, data)
}

func parseImageWorkbenchMetadata(contentType string, body []byte, maxImageSizeMB, maxReferenceImages int) (*service.ImageWorkbenchMetadata, error) {
	if isMultipartImagesContentType(contentType) {
		return nil, errors.New("workbench requests must use JSON with images[].image_url")
	}
	if !hasUniqueJSONMembers(body) || !validImageWorkbenchFields(body) {
		return nil, errors.New("workbench requests require valid JSON with unique fields")
	}
	var payload struct {
		Prompt         string `json:"prompt"`
		Model          string `json:"model"`
		Quality        string `json:"quality"`
		Size           string `json:"size"`
		N              int    `json:"n"`
		Stream         bool   `json:"stream"`
		ResponseFormat string `json:"response_format"`
		Images         []struct {
			ImageURL string `json:"image_url"`
		} `json:"images"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("invalid image workbench request")
	}
	if payload.Stream {
		return nil, errors.New("streaming image requests cannot be submitted as asynchronous tasks")
	}
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Prompt == "" || utf8.RuneCountInString(payload.Prompt) > 32000 || payload.Model == "" {
		return nil, errors.New("model and a prompt of at most 32000 characters are required")
	}
	if payload.N == 0 {
		payload.N = 1
	}
	if payload.N != 1 {
		return nil, errors.New("each image workbench task must generate exactly one image")
	}
	if payload.Quality == "" {
		payload.Quality = "auto"
	}
	switch payload.Quality {
	case "auto", "low", "medium", "high":
	default:
		return nil, errors.New("invalid image quality")
	}
	if len(payload.Images) > maxReferenceImages {
		return nil, fmt.Errorf("at most %d reference images are allowed", maxReferenceImages)
	}
	for _, image := range payload.Images {
		header, encoded, ok := strings.Cut(strings.TrimSpace(image.ImageURL), ",")
		if !ok {
			return nil, errors.New("reference images must be base64 PNG, JPEG, or WebP data URLs")
		}
		switch strings.ToLower(header) {
		case "data:image/png;base64", "data:image/jpeg;base64", "data:image/webp;base64":
		default:
			return nil, errors.New("reference images must be base64 PNG, JPEG, or WebP data URLs")
		}
		// Count decoded bytes without allocating a second copy of large uploads.
		size, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)))
		if err != nil || size == 0 {
			return nil, errors.New("invalid reference image data")
		}
		if maxImageSizeMB > 0 && (size+1024*1024-1)/(1024*1024) > int64(maxImageSizeMB) {
			return nil, fmt.Errorf("each reference image must be %d MB or smaller", maxImageSizeMB)
		}
	}
	return &service.ImageWorkbenchMetadata{
		Prompt: payload.Prompt, Model: payload.Model, Quality: payload.Quality,
		Size: payload.Size, Count: payload.N,
	}, nil
}

func (h *AsyncImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, apiKey *service.APIKey, platform string, body []byte) bool {
	if h == nil || h.openAI == nil {
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		imageTaskJSONError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return false
	}
	model := ""
	moderationBody := body
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	} else if h.openAI.gatewayService != nil {
		parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return false
		}
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	}
	if len(moderationBody) == 0 {
		c.Set(securityAuditCompletedContextKey, true)
		return true
	}
	reqLog := requestLogger(c, "handler.async_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", model))
	decision := h.openAI.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, model, moderationBody)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *AsyncImageHandler) Get(c *gin.Context) {
	// Polling deliberately does not require the feature to be enabled, only that
	// the task store is reachable. Turning the switch off in the admin UI must not
	// strand tasks that were already accepted — their results are still in Redis
	// and their submitters are still polling.
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	ownerEmail, ok := imageOwnerEmail(c, apiKey)
	if !ok {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID, UserEmail: ownerEmail}, c.Param("task_id"))
	if err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if task.Status == service.ImageTaskStatusProcessing || task.Status == service.ImageTaskStatusQueued {
		c.Header("Retry-After", "3")
	}
	c.JSON(http.StatusOK, task)
}

// imageOwnerEmail binds the browser-provided identity hint to the authenticated
// API key owner. The header is never trusted on its own; it must match the
// server-loaded user record case-insensitively after whitespace normalization.
func imageOwnerEmail(c *gin.Context, apiKey *service.APIKey) (string, bool) {
	provided := service.NormalizeImageOwnerEmail(c.GetHeader(imageOwnerEmailHeader))
	if apiKey == nil || apiKey.User == nil {
		return "", false
	}
	expected := service.NormalizeImageOwnerEmail(apiKey.User.Email)
	return expected, expected != "" && provided != "" && expected == provided
}

func (h *AsyncImageHandler) validateRequest(c *gin.Context, platform string, body []byte) error {
	if h.openAI == nil || h.openAI.gatewayService == nil {
		return nil
	}
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		if strings.TrimSpace(parsed.Model) == "" {
			return errors.New("model is required")
		}
		return nil
	}
	parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		return err
	}
	if parsed.Stream {
		return errors.New("streaming image requests cannot be submitted as asynchronous tasks")
	}
	return nil
}

func (h *AsyncImageHandler) executeWithGateway(platform string, c *gin.Context) {
	if h.openAI == nil {
		imageTaskJSONError(c, http.StatusServiceUnavailable, "api_error", "image gateway is unavailable")
		return
	}
	if platform == service.PlatformGrok {
		h.openAI.GrokImages(c)
		return
	}
	h.openAI.Images(c)
}

func (h *AsyncImageHandler) run(taskID, platform string, taskCtx *gin.Context, recorder *httptest.ResponseRecorder, cancel context.CancelFunc) {
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().Error("image_task.execution_panicked", zap.String("task_id", taskID), zap.Any("panic", recovered))
			h.failTask(taskID, http.StatusInternalServerError, imageTaskErrorPayload("api_error", "image generation task panicked"))
		}
	}()

	h.execute(platform, taskCtx)
	body := bytes.TrimSpace(recorder.Body.Bytes())
	if err := taskCtx.Request.Context().Err(); err != nil && len(body) == 0 {
		h.failTask(taskID, http.StatusGatewayTimeout, imageTaskErrorPayload("timeout_error", "image generation task timed out"))
		return
	}
	statusCode := recorder.Code
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		if len(body) == 0 || !json.Valid(body) {
			h.failTask(taskID, http.StatusBadGateway, imageTaskErrorPayload("api_error", "upstream returned an invalid image response"))
			return
		}
		if isImageWorkbenchRequest(taskCtx) {
			var response struct {
				Data []json.RawMessage `json:"data"`
			}
			if json.Unmarshal(body, &response) != nil || len(response.Data) == 0 {
				h.failTask(taskID, http.StatusBadGateway, imageTaskErrorPayload("api_error", "upstream returned no generated images"))
				return
			}
		}
		if err := h.tasks.Complete(context.Background(), taskID, statusCode, json.RawMessage(body)); err != nil {
			logger.L().Error("image_task.complete_store_failed", zap.String("task_id", taskID), zap.Error(err))
		}
		return
	}
	h.failTask(taskID, statusCode, extractImageTaskError(body))
}

func (h *AsyncImageHandler) failTask(taskID string, statusCode int, taskErr json.RawMessage) {
	if err := h.tasks.Fail(context.Background(), taskID, statusCode, taskErr); err != nil {
		logger.L().Error("image_task.failure_store_failed", zap.String("task_id", taskID), zap.Error(err))
	}
}

func newAsyncImageContext(c *gin.Context, body []byte, timeoutDuration time.Duration) (*gin.Context, *httptest.ResponseRecorder, context.CancelFunc) {
	base := context.WithoutCancel(c.Request.Context())
	executionCtx, cancel := context.WithTimeout(base, timeoutDuration)
	request := c.Request.Clone(executionCtx)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	request.ContentLength = int64(len(body))
	request.URL.Path = strings.TrimSuffix(request.URL.Path, "/async")

	taskCtx := c.Copy()
	recorder := httptest.NewRecorder()
	recorderCtx, _ := gin.CreateTestContext(recorder)
	taskCtx.Writer = recorderCtx.Writer
	taskCtx.Request = request
	return taskCtx, recorder, cancel
}

func asyncImageRequestStreams(contentType string, body []byte) bool {
	if isMultipartImagesContentType(contentType) {
		return false
	}
	var envelope struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.Stream
}

func imageTaskPollURL(submitPath, taskID string) string {
	if strings.HasPrefix(submitPath, "/v1/") {
		return "/v1/images/tasks/" + taskID
	}
	return "/images/tasks/" + taskID
}

func extractImageTaskError(body []byte) json.RawMessage {
	if json.Valid(body) {
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 && json.Valid(envelope.Error) {
			return envelope.Error
		}
		return json.RawMessage(body)
	}
	return imageTaskErrorPayload("api_error", "image generation failed")
}

func imageTaskErrorPayload(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(gin.H{"type": errorType, "message": message})
	return data
}

func imageTaskError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if strings.TrimSpace(code) == "" {
		code = "IMAGE_TASK_ERROR"
	}
	imageTaskJSONError(c, status, code, message)
}

func imageTaskJSONError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}
