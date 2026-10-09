//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesEmptyCompletedFailsOverBeforeOutput(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			cfg := &config.Config{}
			svc := &OpenAIGatewayService{cfg: cfg, toolCorrector: NewCodexToolCorrector()}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_empty\"}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_empty\",\"output\":[]}}\n\n"))}
			_, err := svc.handleStreamingResponse(context.Background(), response, c, &Account{ID: 802, Platform: platform}, time.Now(), "model", "model")
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, http.StatusBadGateway, failover.StatusCode)
			require.False(t, c.Writer.Written())
		})
	}
}

func TestResponsesEmptyCompletedKeepsValidOutputOrUsage(t *testing.T) {
	for _, stream := range []string{
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_text\",\"output\":[]}}\n\n",
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_usage\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n",
	} {
		svc := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
		_, err := svc.handleStreamingResponse(context.Background(), response, c, &Account{ID: 803, Platform: PlatformGrok}, time.Now(), "model", "model")
		require.NoError(t, err)
	}
}
