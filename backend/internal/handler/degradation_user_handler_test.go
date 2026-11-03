//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDegradationPreviewFrameIsolatesGeneratedDocuments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewDegradationUserHandler(nil)
	router.GET("/api/v1/degradation/preview-frame", handler.PreviewFrame)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/degradation/preview-frame", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	policy := recorder.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"connect-src 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
		"sandbox allow-scripts",
	} {
		require.Contains(t, policy, directive)
	}
	require.NotContains(t, policy, "allow-same-origin", "generated documents must never share the site origin")
	require.Equal(t, "SAMEORIGIN", recorder.Header().Get("X-Frame-Options"))
	require.Equal(t, "no-referrer", recorder.Header().Get("Referrer-Policy"))
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/html"))
	body := recorder.Body.String()
	require.Contains(t, body, "sub2api-degradation-preview")
	require.Contains(t, body, "event.source !== parent", "only the embedding page may inject a document")
}
