package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RechargeActivity is the dashboard's read-only, combined recharge timeline.
// GET /api/v1/admin/payment/recharge-activity
func (h *PaymentHandler) RechargeActivity(c *gin.Context) {
	page := 1
	if raw := c.Query("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			response.BadRequest(c, "Invalid page")
			return
		}
		page = parsed
	}
	status := c.Query("status")
	if status != "" && status != "paid" && status != "unpaid" {
		response.BadRequest(c, "Invalid status")
		return
	}
	channel := c.Query("channel")
	if channel != "" && channel != "direct" && channel != "redeem" && channel != "manual" {
		response.BadRequest(c, "Invalid channel")
		return
	}
	var start, end time.Time
	if raw := c.Query("start_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid start_time")
			return
		}
		start = parsed
	}
	if raw := c.Query("end_time"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid end_time")
			return
		}
		end = parsed
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		response.BadRequest(c, "end_time must be after start_time")
		return
	}
	email := strings.TrimSpace(c.Query("email"))
	if len([]rune(email)) > 100 {
		response.BadRequest(c, "Email search is too long")
		return
	}
	items, total, err := h.paymentService.AdminRechargeActivity(c.Request.Context(), service.RechargeActivityFilter{
		Page: page, Email: email, Status: status, Channel: channel, StartTime: start, EndTime: end,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, int64(total), page, 25)
}

// RechargeRisks returns only the latest 25 grouped recharge risk observations.
// GET /api/v1/admin/payment/recharge-risks
func (h *PaymentHandler) RechargeRisks(c *gin.Context) {
	events, err := h.paymentService.AdminRechargeRisks(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, events)
}
