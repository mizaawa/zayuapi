package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
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

func TestImageWorkbenchQueuedUnknownLengthUploadHoldsOnlyItsBody(t *testing.T) {
	h, _, _ := queueHandlerFixture(t, 1, time.Minute)
	running, finish := make(chan struct{}), make(chan struct{})
	h.execute = func(_ string, c *gin.Context) {
		close(running)
		<-finish
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": "aW1hZ2U="}}})
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7,
			User:  &service.User{ID: 7, Email: "owner@example.com"},
			Group: &service.Group{ID: 3, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageWorkbenchEnabled: true}})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.WorkbenchUploadAdmission, h.Submit)
	body := `{"model":"gpt-image-2","prompt":"small","n":1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(body))
	request.ContentLength = -1 // chunked: no declared length
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(imageWorkbenchHeader, "true")
	request.Header.Set(imageOwnerEmailHeader, "owner@example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	defer close(finish)
	select {
	case <-running:
	case <-time.After(3 * time.Second):
		t.Fatal("queued task did not start")
	}
	release, err := h.tasks.ReserveWorkbenchPayload(int(imageWorkbenchMaxUploadBytes) - len(body))
	require.NoError(t, err, "a small running task must not keep the full admission estimate")
	release()
}
