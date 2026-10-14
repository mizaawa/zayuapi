package admin

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ResourceLibraryHandler serves the administrator resource library.
type ResourceLibraryHandler struct {
	service *service.ResourceLibraryService
}

func NewResourceLibraryHandler(svc *service.ResourceLibraryService) *ResourceLibraryHandler {
	return &ResourceLibraryHandler{service: svc}
}

func resourceLibraryFilter(c *gin.Context) (service.ResourceLibraryFilter, bool) {
	page, pageSize := response.ParsePagination(c)
	filter := service.ResourceLibraryFilter{
		Kind: c.Query("kind"), Model: c.Query("model"), Query: c.Query("q"),
		Page: page, PageSize: pageSize,
	}
	if raw := strings.TrimSpace(c.Query("group_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			response.BadRequest(c, "Invalid group_id")
			return filter, false
		}
		filter.GroupID = id
	}
	return filter, true
}

func resourceIndex(c *gin.Context) (int, bool) {
	index, err := strconv.Atoi(c.DefaultQuery("index", "0"))
	if err != nil || index < 0 || index > 100 {
		response.BadRequest(c, "Invalid index")
		return 0, false
	}
	return index, true
}

// List returns retained workbench results of every user.
// GET /api/v1/admin/resource-library/items
func (h *ResourceLibraryHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	filter, ok := resourceLibraryFilter(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// Get returns one retained result.
// GET /api/v1/admin/resource-library/items/:kind/:task_id
func (h *ResourceLibraryHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	index, ok := resourceIndex(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), c.Param("kind"), c.Param("task_id"), index)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

// Content streams a retained result for preview or download.
// GET /api/v1/admin/resource-library/items/:kind/:task_id/content
func (h *ResourceLibraryHandler) Content(c *gin.Context) {
	index, ok := resourceIndex(c)
	if !ok {
		return
	}
	content, err := h.service.Content(c.Request.Context(), c.Param("kind"), c.Param("task_id"), index)
	if err != nil {
		c.Header("Cache-Control", "no-store")
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = content.Close() }()
	serveResourceContent(c, content)
}

// Delete removes a retained result of any user.
// DELETE /api/v1/admin/resource-library/items/:kind/:task_id
func (h *ResourceLibraryHandler) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := h.service.Delete(c.Request.Context(), c.Param("kind"), c.Param("task_id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

type saveResourceRequest struct {
	Index int `json:"index"`
}

// Save preserves a retained result in the administrator archive.
// POST /api/v1/admin/resource-library/items/:kind/:task_id/save
func (h *ResourceLibraryHandler) Save(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req saveResourceRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request body")
			return
		}
	}
	if req.Index < 0 || req.Index > 100 {
		response.BadRequest(c, "Invalid index")
		return
	}
	saved, created, err := h.service.Save(c.Request.Context(), subject.UserID, c.Param("kind"), c.Param("task_id"), req.Index)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"item": saved, "created": created})
}

// ListSaved pages the administrator archive.
// GET /api/v1/admin/resource-library/saved
func (h *ResourceLibraryHandler) ListSaved(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	filter, ok := resourceLibraryFilter(c)
	if !ok {
		return
	}
	result, err := h.service.ListSaved(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// GetSaved returns one archived copy.
// GET /api/v1/admin/resource-library/saved/:id
func (h *ResourceLibraryHandler) GetSaved(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	saved, err := h.service.GetSaved(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}

// SavedContent streams an archived copy for preview or download.
// GET /api/v1/admin/resource-library/saved/:id/content
func (h *ResourceLibraryHandler) SavedContent(c *gin.Context) {
	content, err := h.service.SavedContent(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Header("Cache-Control", "no-store")
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = content.Close() }()
	serveResourceContent(c, content)
}

// DeleteSaved permanently removes an archived copy.
// DELETE /api/v1/admin/resource-library/saved/:id
func (h *ResourceLibraryHandler) DeleteSaved(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := h.service.DeleteSaved(c.Request.Context(), c.Param("id")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func serveResourceContent(c *gin.Context, content *service.ResourceContent) {
	disposition := "inline"
	if c.Query("download") == "1" {
		disposition = "attachment"
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Type", content.ContentType)
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": content.Name}))
	c.Header("X-Content-Type-Options", "nosniff")
	// Generated media is untrusted; never let it run as a document.
	c.Header("Content-Security-Policy", "default-src 'none'; media-src 'self'; img-src 'self'; sandbox")
	http.ServeContent(c.Writer, c.Request, content.Name, content.ModTime, content.Body)
}
