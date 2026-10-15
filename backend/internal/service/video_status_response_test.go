//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNewAPIVideoStatusRecovery(t *testing.T) {
	prefix := `{"error":{"message":"upstream panic","type":"new_api_panic"}}`
	task := `{"id":"task_public","task_id":"task_public","object":"video","status":"completed","progress":100,"metadata":{"url":"/v1/videos/provider-id/content"}}`
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"completed", prefix + task, true},
		{"queued", prefix + strings.Replace(task, "completed", "queued", 1), true},
		{"failed", prefix + strings.Replace(task, "completed", "failed", 1), true},
		{"ordinary_error", `{"error":{"type":"upstream_error"}}` + task, false},
		{"other_task", prefix + strings.ReplaceAll(task, "task_public", "task_other"), false},
		{"conflicting_id", prefix + strings.Replace(task, `"task_id":"task_public"`, `"task_id":"other"`, 1), false},
		{"missing_id", prefix + `{"object":"video","status":"completed"}`, false},
		{"unknown_state", prefix + strings.Replace(task, "completed", "unknown", 1), false},
		{"extra_document", prefix + task + `{}`, false},
		{"truncated", prefix + task[:len(task)-1], false},
		{"trailing_garbage", prefix + task + "garbage", false},
		{"bare_task", task, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ok := recoverNewAPIVideoStatus([]byte(tc.body), "task_public")
			require.Equal(t, tc.ok, ok)
			if ok {
				require.NotContains(t, string(body), "new_api_panic")
			}
		})
	}
}

func TestCustomVideoStatusRecoversNewAPIPanicBeforeBillingAndForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	task := `{"id":"task_public","task_id":"task_public","object":"video","status":"completed","progress":100}`
	for _, protocol := range []string{"", "sora"} {
		t.Run(protocol, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = protocol
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"type":"new_api_panic"}}` + task)),
			}}
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_public", nil)
			result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoStatus, "task_public", nil, "")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.BufferedResponseStatus)
			require.JSONEq(t, task, string(result.BufferedResponseBody))
			require.Len(t, upstream.requests, 1)
			_, completed := mediaVideoStatusQuote(&MediaBillingReservation{}, result.BufferedResponseBody)
			require.True(t, completed)
		})
	}
}

func TestCustomVideoContentFallbackRecoversNewAPITaskStatus(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "sora"
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		videoContentDownloadTestResponse(404, "application/json", `{"error":{"message":"not found"}}`),
		videoContentDownloadTestResponse(500, "application/json", `{"error":{"type":"new_api_panic"}}{"id":"public-id","object":"video","status":"completed","metadata":{"url":"/proxy/v1/videos/internal-id/content"}}`),
		videoContentDownloadTestResponse(200, "video/mp4", "video-payload"),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(http.MethodGet, "/v1/videos/public-id/content", nil)
	_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoContent, "public-id", nil, "")
	require.NoError(t, err)
	require.Equal(t, "video-payload", recorder.Body.String())
	require.True(t, c.GetBool(GrokMediaContentCompleteKey))
	require.Len(t, upstream.requests, 3)
	for _, request := range upstream.requests {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "Bearer custom-secret", request.Header.Get("Authorization"))
	}
}
