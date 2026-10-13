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

func TestOpenAIVideoPrechargeMatchesForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"grok-imagine-video", "seedance-2.5"} {
		t.Run(model, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = "openai"
			body := []byte(`{"model":"` + model + `","prompt":"waves","seconds":"5","resolution":"480p","ratio":"16:9","referenceImages":["https://cdn.example/frame.png"],"referenceVideos":["https://cdn.example/ref.mp4"],"referenceAudios":["https://cdn.example/ref.mp3"]}`)
			info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
			require.NoError(t, err)
			require.Equal(t, 5, info.RawDurationSeconds)
			require.Equal(t, "480p", info.Resolution)
			require.True(t, info.HasReferenceVideo)
			require.Equal(t, []string{"https://cdn.example/frame.png"}, info.InputImageURLs)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"task_public","task_id":"internal","status":"queued"}`)),
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "https://custom.example.test/proxy/v1/videos", upstream.lastReq.URL.String())
			require.Equal(t, gjson.Number, gjson.GetBytes(upstream.lastBody, "duration").Type)
			require.Equal(t, int64(5), gjson.GetBytes(upstream.lastBody, "duration").Int())
			require.Equal(t, "16:9", gjson.GetBytes(upstream.lastBody, "aspect_ratio").String())
			require.Equal(t, "https://cdn.example/ref.mp4", gjson.GetBytes(upstream.lastBody, "reference_video_urls.0").String())
			require.Equal(t, "https://cdn.example/ref.mp3", gjson.GetBytes(upstream.lastBody, "reference_audio_urls.0").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "seconds").Exists())
			require.False(t, gjson.GetBytes(upstream.lastBody, "referenceVideos").Exists())
			require.Equal(t, info.RawDurationSeconds, result.VideoDurationSeconds)
			require.Equal(t, info.Resolution, result.VideoResolution)
			require.Equal(t, "task_public", result.ResponseID)
		})
	}
}

func TestOpenAIVideoJSONValidationAndNativeReferences(t *testing.T) {
	for _, body := range []string{
		`{"duration":5,"seconds":"6"}`, `{"duration":1.5}`, `{"duration":0}`, `{"n":2}`,
		`{"resolution":"unpriced"}`, `{"resolution":720}`, `{"resolution":"480p","resolution_name":"720p"}`,
		`{"ratio":"16:9","aspect_ratio":"1:1"}`, `{"ratio":"0:1"}`,
		`{"referenceImages":["a"],"reference_image_urls":["b"]}`, `{"reference_video_urls":[{}]}`,
	} {
		_, err := normalizeOpenAIVideoJSON([]byte(body))
		require.Error(t, err, body)
	}
	body, err := normalizeOpenAIVideoJSON([]byte(`{"model":"seedance-2.5","duration":5,"resolution":"auto","input_reference":"https://cdn.example/input.png","first_frame_image_url":"https://cdn.example/first.png","last_frame_image_url":"https://cdn.example/last.png","reference_image_urls":["https://cdn.example/ref.png"]}`))
	require.NoError(t, err)
	require.Equal(t, "720p", gjson.GetBytes(body, "resolution").String())
	info := ParseGrokMediaRequest("application/json", body)
	require.Len(t, info.InputImageURLs, 4)
	require.True(t, validMediaProtocol("openai"))
	_, err = VideoPrechargeRequestInfo(context.Background(), &Account{Credentials: map[string]any{"media_protocol": "openai"}}, []byte("multipart"), "multipart/form-data")
	require.Error(t, err)
}
