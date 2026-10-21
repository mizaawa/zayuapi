package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *PaymentHandler) GetPaymentAccess(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	var access *service.PaymentAccess
	var err error
	if previous := c.Query("watch"); previous != "" {
		access, err = h.paymentService.WaitForPaymentAccess(c.Request.Context(), subject.UserID, previous, 25*time.Second)
	} else {
		access, err = h.paymentService.GetPaymentAccess(c.Request.Context(), subject.UserID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, access)
}

func (h *PaymentHandler) RequirePaymentAccess(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		c.Abort()
		return
	}
	if err := h.paymentService.RequirePaymentAccess(c.Request.Context(), subject.UserID); err != nil {
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	c.Next()
}
