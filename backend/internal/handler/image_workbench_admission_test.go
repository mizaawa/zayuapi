package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchCountingBody struct{ reads int }

func (b *workbenchCountingBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *workbenchCountingBody) Close() error             { return nil }

func TestImageWorkbenchAdmissionRejectsBeforeReadingBody(t *testing.T) {
	h, _, _ := queueHandlerFixture(t, 1, 0)
	release, err := h.tasks.ReserveWorkbenchPayload(int(imageWorkbenchMaxUploadBytes))
	require.NoError(t, err)
	defer release()
	router := gin.New()
	router.POST("/v1/images/edits/async", h.WorkbenchUploadAdmission, func(c *gin.Context) {
		_, _ = io.ReadAll(c.Request.Body)
		t.Error("exhausted upload budget reached the body reader")
	})
	body := &workbenchCountingBody{}
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", body)
	request.Header.Set(imageWorkbenchHeader, "true")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Zero(t, body.reads)
}

func TestImageWorkbenchAdmissionReleasesRejectedUploadsAndBoundsDeclaredLength(t *testing.T) {
	h, _, _ := queueHandlerFixture(t, 1, 0)
	router := gin.New()
	router.POST("/v1/images/edits/async", h.WorkbenchUploadAdmission, func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		var tooLarge *http.MaxBytesError
		require.ErrorAs(t, err, &tooLarge)
		c.AbortWithStatus(http.StatusBadRequest)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", strings.NewReader("longer than declared"))
	request.ContentLength = 1
	request.Header.Set(imageWorkbenchHeader, "true")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	release, err := h.tasks.ReserveWorkbenchPayload(int(imageWorkbenchMaxUploadBytes))
	require.NoError(t, err, "failed uploads must release the entire reservation")
	release()
}
