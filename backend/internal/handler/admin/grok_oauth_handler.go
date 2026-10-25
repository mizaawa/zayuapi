package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type GrokOAuthHandler struct {
	grokOAuthService *service.GrokOAuthService
	adminService     service.AdminService
	quotaService     *service.GrokQuotaService
	reconciler       service.GrokOAuthReconciler
}

func NewGrokOAuthHandler(
	grokOAuthService *service.GrokOAuthService,
	adminService service.AdminService,
	quotaService *service.GrokQuotaService,
	reconciler service.GrokOAuthReconciler,
) *GrokOAuthHandler {
	return &GrokOAuthHandler{
		grokOAuthService: grokOAuthService,
		adminService:     adminService,
		quotaService:     quotaService,
		reconciler:       reconciler,
	}
}

func (h *GrokOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformGrok {
		response.BadRequest(c, "Account platform does not match Grok OAuth endpoint")
		return
	}
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	tokenInfo, err := h.grokOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	newCredentials := h.grokOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = service.MergeCredentials(account.Credentials, newCredentials)
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updatedAccount))
}

type GrokOAuthReconcileRequest struct {
	DryRun               *bool `json:"dry_run"`
	Apply                bool  `json:"apply"`
	AfterID              int64 `json:"after_id"`
	Limit                int   `json:"limit"`
	RefreshWindowSeconds int64 `json:"refresh_window_seconds"`
}

func (h *GrokOAuthHandler) ReconcileOAuthAccounts(c *gin.Context) {
	var req GrokOAuthReconcileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	if req.Apply == dryRun {
		response.ErrorFrom(c, service.ErrGrokOAuthReconcileMode)
		return
	}
	if req.RefreshWindowSeconds < 0 || req.RefreshWindowSeconds > int64((24*time.Hour)/time.Second) {
		response.ErrorFrom(c, service.ErrGrokOAuthReconcileWindow)
		return
	}
	if h.reconciler == nil {
		response.InternalError(c, "Grok OAuth reconciliation service is unavailable")
		return
	}
	result, err := h.reconciler.ReconcileGrokOAuth(c.Request.Context(), service.GrokOAuthReconcileInput{
		DryRun:        dryRun,
		Apply:         req.Apply,
		AfterID:       req.AfterID,
		Limit:         req.Limit,
		RefreshWindow: time.Duration(req.RefreshWindowSeconds) * time.Second,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "grok quota service is not enabled")
		return
	}
	result, err := h.quotaService.QueryQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) ResetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "grok quota service is not enabled")
		return
	}
	result, err := h.quotaService.ResetQuota(c.Request.Context(), accountID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *GrokOAuthHandler) RuntimeSanity(c *gin.Context) {
	response.Success(c, xai.RuntimeSanity())
}
