package admin

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// DegradationMonitorHandler serves the degradation monitor (降智监控) admin page.
type DegradationMonitorHandler struct {
	service *service.DegradationMonitorService
}

// NewDegradationMonitorHandler creates the handler.
func NewDegradationMonitorHandler(svc *service.DegradationMonitorService) *DegradationMonitorHandler {
	return &DegradationMonitorHandler{service: svc}
}

type degradationMonitorRequest struct {
	Name            string `json:"name" binding:"omitempty,max=100"`
	GroupID         int64  `json:"group_id" binding:"required,gt=0"`
	Model           string `json:"model" binding:"required,max=200"`
	ReasoningEffort string `json:"reasoning_effort" binding:"omitempty,max=16"`
	Protocol        string `json:"protocol" binding:"omitempty,oneof=openai_chat openai_responses anthropic"`
	IntervalMinutes *int   `json:"interval_minutes" binding:"omitempty,min=0,max=10080"`
	PromptID        *int64 `json:"prompt_id" binding:"omitempty,min=0"`
	RandomPrompt    bool   `json:"random_prompt"`
	Enabled         *bool  `json:"enabled"`
}

func (r degradationMonitorRequest) toInput() service.DegradationMonitorInput {
	input := service.DegradationMonitorInput{
		Name:            r.Name,
		GroupID:         r.GroupID,
		Model:           r.Model,
		ReasoningEffort: r.ReasoningEffort,
		Protocol:        r.Protocol,
		PromptID:        r.PromptID,
		RandomPrompt:    r.RandomPrompt,
		Enabled:         true,
	}
	if r.IntervalMinutes != nil {
		input.IntervalMinutes = *r.IntervalMinutes
	}
	if r.Enabled != nil {
		input.Enabled = *r.Enabled
	}
	return input
}

type degradationSortOrderRequest struct {
	Updates []struct {
		ID        int64 `json:"id" binding:"required,gt=0"`
		SortOrder int   `json:"sort_order"`
	} `json:"updates" binding:"required,min=1,max=500,dive"`
}

type degradationPromptRequest struct {
	Name   string `json:"name" binding:"omitempty,max=100"`
	Prompt string `json:"prompt" binding:"required"`
}

type degradationConfigRequest struct {
	DefaultIntervalMinutes int    `json:"default_interval_minutes" binding:"required"`
	TimeoutMinutes         int    `json:"timeout_minutes" binding:"required"`
	MaxRetries             *int   `json:"max_retries" binding:"required"`
	MaxDisplayPerGroup     int    `json:"max_display_per_group" binding:"required"`
	DefaultReasoningEffort string `json:"default_reasoning_effort" binding:"required"`
	DefaultPromptID        int64  `json:"default_prompt_id"`
}

type degradationMonitorResponse struct {
	ID                       int64   `json:"id"`
	Name                     string  `json:"name"`
	GroupID                  int64   `json:"group_id"`
	GroupName                string  `json:"group_name"`
	GroupPlatform            string  `json:"group_platform"`
	GroupStatus              string  `json:"group_status"`
	GroupDeleted             bool    `json:"group_deleted"`
	Model                    string  `json:"model"`
	ReasoningEffort          string  `json:"reasoning_effort"`
	EffectiveReasoningEffort string  `json:"effective_reasoning_effort"`
	Protocol                 string  `json:"protocol"`
	IntervalMinutes          int     `json:"interval_minutes"`
	EffectiveIntervalMinutes int     `json:"effective_interval_minutes"`
	PromptID                 *int64  `json:"prompt_id"`
	PromptName               string  `json:"prompt_name"`
	RandomPrompt             bool    `json:"random_prompt"`
	Enabled                  bool    `json:"enabled"`
	SortOrder                int     `json:"sort_order"`
	Running                  bool    `json:"running"`
	LastRunAt                *string `json:"last_run_at"`
	LastResultStatus         string  `json:"last_result_status"`
	LastResultAt             *string `json:"last_result_at"`
	LastResultError          string  `json:"last_result_error"`
	StoredRuns               int64   `json:"stored_runs"`
	StoredBytes              int64   `json:"stored_bytes"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
}

func degradationMonitorToResponse(monitor *service.DegradationMonitor, cfg service.DegradationConfig) *degradationMonitorResponse {
	return &degradationMonitorResponse{
		ID:                       monitor.ID,
		Name:                     monitor.Name,
		GroupID:                  monitor.GroupID,
		GroupName:                monitor.GroupName,
		GroupPlatform:            monitor.GroupPlatform,
		GroupStatus:              monitor.GroupStatus,
		GroupDeleted:             monitor.GroupDeleted,
		Model:                    monitor.Model,
		ReasoningEffort:          monitor.ReasoningEffort,
		EffectiveReasoningEffort: monitor.EffectiveReasoningEffort(cfg),
		Protocol:                 monitor.Protocol,
		IntervalMinutes:          monitor.IntervalMinutes,
		EffectiveIntervalMinutes: monitor.EffectiveIntervalMinutes(cfg),
		PromptID:                 monitor.PromptID,
		PromptName:               monitor.PromptName,
		RandomPrompt:             monitor.RandomPrompt,
		Enabled:                  monitor.Enabled,
		SortOrder:                monitor.SortOrder,
		LastRunAt:                formatDegradationTime(monitor.LastRunAt),
		CreatedAt:                monitor.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:                monitor.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func formatDegradationTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func parseDegradationID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ID", "invalid id"))
		return 0, false
	}
	return id, true
}

func bindDegradationJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return false
	}
	return true
}

// ListMonitors GET /api/v1/admin/degradation/monitors
func (h *DegradationMonitorHandler) ListMonitors(c *gin.Context) {
	views, cfg, err := h.service.ListMonitors(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]*degradationMonitorResponse, 0, len(views))
	for _, view := range views {
		item := degradationMonitorToResponse(view.Monitor, cfg)
		item.Running = view.Running
		item.LastResultStatus = view.Stats.LastRunStatus
		item.LastResultAt = formatDegradationTime(view.Stats.LastRunAt)
		item.LastResultError = view.Stats.LastRunError
		item.StoredRuns = view.Stats.StoredRuns
		item.StoredBytes = view.Stats.StoredBytes
		items = append(items, item)
	}
	response.Success(c, gin.H{"items": items, "config": cfg, "enabled": h.service.Enabled(c.Request.Context())})
}

// CreateMonitor POST /api/v1/admin/degradation/monitors
func (h *DegradationMonitorHandler) CreateMonitor(c *gin.Context) {
	var req degradationMonitorRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	monitor, err := h.service.CreateMonitor(c.Request.Context(), req.toInput(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, degradationMonitorToResponse(monitor, h.service.GetConfig(c.Request.Context())))
}

// UpdateMonitor PUT /api/v1/admin/degradation/monitors/:id
func (h *DegradationMonitorHandler) UpdateMonitor(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	var req degradationMonitorRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	monitor, err := h.service.UpdateMonitor(c.Request.Context(), id, req.toInput())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, degradationMonitorToResponse(monitor, h.service.GetConfig(c.Request.Context())))
}

// DeleteMonitor DELETE /api/v1/admin/degradation/monitors/:id
func (h *DegradationMonitorHandler) DeleteMonitor(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteMonitor(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

// UpdateSortOrder PUT /api/v1/admin/degradation/monitors/sort-order
func (h *DegradationMonitorHandler) UpdateSortOrder(c *gin.Context) {
	var req degradationSortOrderRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	updates := make([]service.DegradationSortOrderUpdate, 0, len(req.Updates))
	for _, update := range req.Updates {
		updates = append(updates, service.DegradationSortOrderUpdate{ID: update.ID, SortOrder: update.SortOrder})
	}
	if err := h.service.UpdateSortOrders(c.Request.Context(), updates); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Sort order updated successfully"})
}

// RunMonitor POST /api/v1/admin/degradation/monitors/:id/run
func (h *DegradationMonitorHandler) RunMonitor(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	if err := h.service.RunNow(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, gin.H{"queued": true})
}

// StorageStats GET /api/v1/admin/degradation/monitors/:id/storage
func (h *DegradationMonitorHandler) StorageStats(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	stats, err := h.service.StorageStats(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"count": stats.Count, "bytes": stats.Bytes})
}

// ReleaseStorage DELETE /api/v1/admin/degradation/monitors/:id/runs
func (h *DegradationMonitorHandler) ReleaseStorage(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	deleted, err := h.service.ReleaseStorage(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": deleted})
}

// GetRun GET /api/v1/admin/degradation/runs/:id
func (h *DegradationMonitorHandler) GetRun(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	run, err := h.service.GetRun(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.DegradationRunFromService(run, true))
}

// ListPrompts GET /api/v1/admin/degradation/prompts
func (h *DegradationMonitorHandler) ListPrompts(c *gin.Context) {
	prompts, err := h.service.ListPrompts(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]dto.DegradationPrompt, 0, len(prompts))
	for _, prompt := range prompts {
		items = append(items, dto.DegradationPromptFromService(prompt))
	}
	response.Success(c, gin.H{"items": items})
}

// CreatePrompt POST /api/v1/admin/degradation/prompts
func (h *DegradationMonitorHandler) CreatePrompt(c *gin.Context) {
	var req degradationPromptRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	prompt, err := h.service.CreatePrompt(c.Request.Context(), req.Name, req.Prompt)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.DegradationPromptFromService(prompt))
}

// UpdatePrompt PUT /api/v1/admin/degradation/prompts/:id
func (h *DegradationMonitorHandler) UpdatePrompt(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	var req degradationPromptRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	prompt, err := h.service.UpdatePrompt(c.Request.Context(), id, req.Name, req.Prompt)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.DegradationPromptFromService(prompt))
}

// DeletePrompt DELETE /api/v1/admin/degradation/prompts/:id
func (h *DegradationMonitorHandler) DeletePrompt(c *gin.Context) {
	id, ok := parseDegradationID(c)
	if !ok {
		return
	}
	if err := h.service.DeletePrompt(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, nil)
}

// GetConfig GET /api/v1/admin/degradation/config
func (h *DegradationMonitorHandler) GetConfig(c *gin.Context) {
	response.Success(c, h.service.GetConfig(c.Request.Context()))
}

// UpdateConfig PUT /api/v1/admin/degradation/config
func (h *DegradationMonitorHandler) UpdateConfig(c *gin.Context) {
	var req degradationConfigRequest
	if !bindDegradationJSON(c, &req) {
		return
	}
	cfg, err := h.service.UpdateConfig(c.Request.Context(), service.DegradationConfig{
		DefaultIntervalMinutes: req.DefaultIntervalMinutes,
		TimeoutMinutes:         req.TimeoutMinutes,
		MaxRetries:             *req.MaxRetries,
		MaxDisplayPerGroup:     req.MaxDisplayPerGroup,
		DefaultReasoningEffort: req.DefaultReasoningEffort,
		DefaultPromptID:        req.DefaultPromptID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}
