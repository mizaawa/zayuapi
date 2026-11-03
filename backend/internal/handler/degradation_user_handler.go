package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// degradationPreviewPolicy isolates model-generated documents: inline
// animation may run, but the frame has an opaque origin and cannot reach the
// network, the site's storage, forms, popups or nested frames.
const degradationPreviewPolicy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; " +
	"img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; " +
	"worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-scripts"

// degradationPreviewShell is the static frame document. The parent page posts
// the generated HTML; nothing user-specific is ever served from this URL.
const degradationPreviewShell = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="referrer" content="no-referrer"></head><body><script>
addEventListener('message', function receive(event) {
  var data = event.data;
  if (event.source !== parent || !data || data.type !== 'sub2api-degradation-preview' || typeof data.html !== 'string' || data.html.length > 2200000) return;
  removeEventListener('message', receive);
  document.open();
  document.write(data.html);
  document.close();
});
</script></body></html>`

// DegradationUserHandler serves the read-only degradation check page.
type DegradationUserHandler struct {
	service *service.DegradationMonitorService
}

// NewDegradationUserHandler creates the handler.
func NewDegradationUserHandler(svc *service.DegradationMonitorService) *DegradationUserHandler {
	return &DegradationUserHandler{service: svc}
}

type degradationUserMonitorResponse struct {
	ID              int64                    `json:"id"`
	Name            string                   `json:"name"`
	GroupName       string                   `json:"group_name"`
	Platform        string                   `json:"platform"`
	Model           string                   `json:"model"`
	Protocol        string                   `json:"protocol"`
	ReasoningEffort string                   `json:"reasoning_effort"`
	IntervalMinutes int                      `json:"interval_minutes"`
	LastRunAt       *string                  `json:"last_run_at"`
	Runs            []dto.DegradationUserRun `json:"runs"`
}

// Overview GET /api/v1/degradation/overview
func (h *DegradationUserHandler) Overview(c *gin.Context) {
	overview, err := h.service.Overview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	monitors := make([]degradationUserMonitorResponse, 0, len(overview.Monitors))
	for _, monitor := range overview.Monitors {
		runs := make([]dto.DegradationUserRun, 0, len(monitor.Runs))
		for _, run := range monitor.Runs {
			runs = append(runs, dto.DegradationUserRunFromService(run, false))
		}
		item := degradationUserMonitorResponse{
			ID:              monitor.ID,
			Name:            monitor.Name,
			GroupName:       monitor.GroupName,
			Platform:        monitor.Platform,
			Model:           monitor.Model,
			Protocol:        monitor.Protocol,
			ReasoningEffort: monitor.EffectiveReasoningEffort,
			IntervalMinutes: monitor.EffectiveIntervalMinutes,
			Runs:            runs,
		}
		if monitor.LastRunAt != nil {
			formatted := monitor.LastRunAt.UTC().Format(time.RFC3339)
			item.LastRunAt = &formatted
		}
		monitors = append(monitors, item)
	}
	response.Success(c, gin.H{
		"enabled":       overview.Enabled,
		"display_limit": overview.DisplayLimit,
		"monitors":      monitors,
	})
}

// GetRun GET /api/v1/degradation/runs/:id
func (h *DegradationUserHandler) GetRun(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, infraerrors.BadRequest("INVALID_ID", "invalid id"))
		return
	}
	run, err := h.service.GetRunForUser(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.DegradationUserRunFromService(run, true))
}

// PreviewFrame GET /api/v1/degradation/preview-frame
//
// The sandboxed iframe loads this public shell and receives the document via
// postMessage, so generated markup never runs with the site's origin or CSP.
func (h *DegradationUserHandler) PreviewFrame(c *gin.Context) {
	c.Header("Content-Security-Policy", degradationPreviewPolicy)
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(degradationPreviewShell))
}
