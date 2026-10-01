package middleware

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFailoverCompactKeepalivePreservesRetryBudget(t *testing.T) {
	for _, fallbackFails := range []bool{false, true} {
		name := "fallback_succeeds"
		if fallbackFails {
			name = "fallback_exhausted"
		}
		t.Run(name, func(t *testing.T) {
			key := failoverTestKey()
			stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive}}
			var groups []int64
			r := failoverTestRouter(key, stub, func(c *gin.Context) {
				current, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				groups = append(groups, *current.GroupID)
				service.MarkOpenAICompactClientStream(c)
				stop := service.StartOpenAICompactSSEKeepalive(c, time.Millisecond)
				defer stop()
				require.Eventually(t, func() bool { return c.Writer.Written() }, time.Second, time.Millisecond)
				require.True(t, service.StopOpenAICompactSSEKeepaliveCommitted(c), "later attempts must inherit the committed SSE headers")
				require.Equal(t, -1, service.OpenAICompactKeepaliveAdjustedWrittenSize(c))
				if *current.GroupID == 1 || fallbackFails {
					service.RecordAPIKeyFailoverUpstreamFailure(c.Request.Context(), http.StatusServiceUnavailable, nil)
					service.MarkOpsStreamError(c, "upstream_error", "unavailable", http.StatusServiceUnavailable)
					_, err := c.Writer.WriteString("event: response.failed\ndata: {\"type\":\"response.failed\"}\n\n")
					require.NoError(t, err)
				} else {
					_, err := c.Writer.WriteString("event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
					require.NoError(t, err)
				}
				c.Writer.Flush()
			})
			w := failoverTestResponse(r)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
			require.Contains(t, w.Body.String(), ": keepalive\n\n")
			require.Equal(t, 1, stub.activations)
			if fallbackFails {
				require.Equal(t, []int64{1, 1, 1, 2, 2, 2}, groups)
				require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.failed"))
				require.NotContains(t, w.Body.String(), "event: response.completed")
			} else {
				require.Equal(t, []int64{1, 1, 1, 2}, groups)
				require.NotContains(t, w.Body.String(), "event: response.failed")
				require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.completed"))
			}
		})
	}
}
