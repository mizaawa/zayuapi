package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const imageWorkbenchUploadKey = "image_workbench_upload_admission"
const imageWorkbenchMaxUploadBytes int64 = 256 << 20

type imageWorkbenchUpload struct {
	reservation *service.WorkbenchPayloadReservation
	release     func()
	transferred bool
}

// Reserve uploads before composite routing reads their bodies. The reservation
// stays with the queued task until its worker exits.
func (h *AsyncImageHandler) WorkbenchUploadAdmission(c *gin.Context) {
	path := strings.TrimPrefix(c.Request.URL.Path, "/v1")
	if c.Request.Method != http.MethodPost || !isImageWorkbenchRequest(c) ||
		(path != "/images/generations/async" && path != "/images/edits/async") || h == nil || h.tasks == nil {
		c.Next()
		return
	}
	upload, err := h.reserveWorkbenchUpload(c)
	if err != nil {
		imageTaskError(c, err)
		c.Abort()
		return
	}
	defer func() {
		if !upload.transferred {
			upload.release()
		}
	}()
	c.Next()
}

func (h *AsyncImageHandler) reserveWorkbenchUpload(c *gin.Context) (*imageWorkbenchUpload, error) {
	if value, ok := c.Get(imageWorkbenchUploadKey); ok {
		upload, valid := value.(*imageWorkbenchUpload)
		if !valid || upload == nil {
			return nil, service.ErrImageTaskUnavailable
		}
		return upload, nil
	}
	size := c.Request.ContentLength
	if size <= 0 || size > imageWorkbenchMaxUploadBytes {
		size = imageWorkbenchMaxUploadBytes
	}
	reservation, err := h.tasks.ReserveWorkbenchUpload(int(size))
	if err != nil {
		return nil, err
	}
	upload := &imageWorkbenchUpload{reservation: reservation, release: reservation.Release}
	c.Set(imageWorkbenchUploadKey, upload)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, size)
	return upload, nil
}
