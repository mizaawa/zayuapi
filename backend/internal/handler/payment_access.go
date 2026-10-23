package handler

import (
	"errors"
	"net/http"
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
		writePaymentAccessError(c, err)
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
	access, err := h.paymentService.GetPaymentAccess(c.Request.Context(), subject.UserID)
	if err != nil {
		writePaymentAccessError(c, err)
		c.Abort()
		return
	}
	if !access.Enabled || !access.Allowed {
		writePaymentAccessError(c, service.ErrPaymentAccessDenied)
		c.Abort()
		return
	}
	c.Next()
}

func writePaymentAccessError(c *gin.Context, err error) bool {
	// ApplicationError.Is matches code and reason, so every standard NOT_FOUND
	// result is intentionally indistinguishable from an unknown API route.
	if errors.Is(err, service.ErrPaymentAccessDenied) {
		c.Header("Content-Type", "text/plain")
		c.String(http.StatusNotFound, "404 page not found")
		return true
	}
	return response.ErrorFrom(c, err)
}
