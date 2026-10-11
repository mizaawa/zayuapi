//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardMiniMaxVideoWorkbenchProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, expected string
		duration             int
	}{
		{
			name:     "H3 multimodal references",
			body:     `{"model":"MiniMax-H3","prompt":"waves","duration":14,"ratio":"9:16","referenceImages":["data:image/png;base64,aW1hZ2U="],"referenceVideos":["data:video/mp4;base64,dmlkZW8="],"referenceAudios":["data:audio/mpeg;base64,YXVkaW8="]}`,
			expected: `{"model":"MiniMax-H3","prompt":"waves","duration":14,"ratio":"9:16","metadata":{"ratio":"9:16","content":[{"type":"text","text":"waves"},{"type":"image_url","role":"reference_image","image_url":{"url":"data:image/png;base64,aW1hZ2U="}},{"type":"video_url","role":"reference_video","video_url":{"url":"data:video/mp4;base64,dmlkZW8="}},{"type":"audio_url","role":"reference_audio","audio_url":{"url":"data:audio/mpeg;base64,YXVkaW8="}}]}}`,
			duration: 14,
		},
		{
			name:     "H3 alias text only",
			body:     `{"model":"minimax-h3-f","prompt":"waves","duration":4,"ratio":"4:3"}`,
			expected: `{"model":"minimax-h3-f","prompt":"waves","duration":4,"ratio":"4:3","metadata":{"ratio":"4:3","content":[{"type":"text","text":"waves"}]}}`,
			duration: 4,
		},
		{
			name:     "Hailuo first and last frames",
			body:     `{"model":"MiniMax-Hailuo-02","prompt":"waves","duration":10,"ratio":"16:9","referenceImages":["data:image/png;base64,aW1hZ2Ux","data:image/png;base64,aW1hZ2Uy"]}`,
			expected: `{"model":"MiniMax-Hailuo-02","prompt":"waves","duration":10,"ratio":"16:9","metadata":{"ratio":"16:9","first_frame_image":"data:image/png;base64,aW1hZ2Ux","last_frame_image":"data:image/png;base64,aW1hZ2Uy"}}`,
			duration: 10,
		},
		{
			name:     "Hailuo Fast image",
			body:     `{"model":"MiniMax-Hailuo-2.3-Fast","prompt":"waves","duration":6,"ratio":"16:9","referenceImages":["data:image/png;base64,aW1hZ2U="]}`,
			expected: `{"model":"MiniMax-Hailuo-2.3-Fast","prompt":"waves","duration":6,"ratio":"16:9","metadata":{"ratio":"16:9","first_frame_image":"data:image/png;base64,aW1hZ2U="}}`,
			duration: 6,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, workbench := range []bool{false, true} {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				body := []byte(tc.body)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"id":"minimax-private-id","status":"queued"}`)),
				}}
				svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
				ctx := context.Background()
				if workbench {
					ctx = WithVideoWorkbenchContext(ctx)
				}
				result, err := svc.ForwardGrokMedia(ctx, c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json")
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				expected := tc.body
				if workbench {
					expected = tc.expected
				}
				require.JSONEq(t, expected, string(upstream.lastBody))
				require.Equal(t, "https://custom.example.test/proxy/v1/videos", upstream.lastReq.URL.String())
				require.Equal(t, tc.duration, result.VideoDurationSeconds)
				require.Equal(t, 1, result.VideoCount)
				require.Equal(t, gjson.Get(tc.body, "model").String(), result.BillingModel)
				require.Empty(t, result.VideoResolution)
				require.Empty(t, recorder.Body.Bytes(), "binding and billing must finish before publishing creation")
				require.NotContains(t, string(upstream.lastBody), "custom-secret")
			}
		})
	}
}

func TestMiniMaxVideoWorkbenchValidatesBeforeForwarding(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"H3 short duration", `{"model":"MiniMax-H3","duration":3,"ratio":"16:9"}`},
		{"H3 long duration", `{"model":"MiniMax-H3","duration":16,"ratio":"16:9"}`},
		{"H3 unsupported ratio", `{"model":"MiniMax-H3","duration":5,"ratio":"2:1"}`},
		{"H3 too many videos", `{"model":"MiniMax-H3","duration":5,"ratio":"16:9","referenceVideos":["a","b","c","d"]}`},
		{"H3 too many audios", `{"model":"MiniMax-H3","duration":5,"ratio":"16:9","referenceAudios":["a","b","c","d"]}`},
		{"Hailuo default five seconds", `{"model":"MiniMax-Hailuo-2.3","duration":5,"ratio":"16:9"}`},
		{"Hailuo unsupported videos", `{"model":"MiniMax-Hailuo-02","duration":6,"ratio":"16:9","referenceVideos":["a"]}`},
		{"Hailuo unsupported audio", `{"model":"MiniMax-Hailuo-02","duration":6,"ratio":"16:9","referenceAudios":["a"]}`},
		{"Hailuo too many frames", `{"model":"MiniMax-Hailuo-02","duration":6,"ratio":"16:9","referenceImages":["a","b","c"]}`},
		{"Fast requires an image", `{"model":"MiniMax-Hailuo-2.3-Fast","duration":6,"ratio":"16:9"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := []byte(tc.body)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			_, err := svc.ForwardGrokMedia(WithVideoWorkbenchContext(context.Background()), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.Error(t, err)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestMiniMaxAdapterLeavesOtherWorkbenchModelsUnchanged(t *testing.T) {
	for _, model := range []string{"grok-imagine-video-480p", "seedance-2-5-720p", "minimax-m2.7", "vendor-video"} {
		body := []byte(`{"model":"` + model + `","prompt":"waves","duration":60,"ratio":"2:1","referenceVideos":["video"]}`)
		out, err := normalizeMiniMaxVideoWorkbenchBody(WithVideoWorkbenchContext(context.Background()), body)
		require.NoError(t, err)
		require.Equal(t, body, out)
	}
}
