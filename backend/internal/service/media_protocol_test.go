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

func TestMediaProtocolAutoResolutionMatchesPrechargeAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"", "sora"} {
		for _, model := range []string{"sd-2.0", "grok-imagine-video", "custom-video", "sora-2"} {
			t.Run(protocol+"/"+model, func(t *testing.T) {
				account := customVideoTestAccount()
				account.Credentials["media_protocol"] = protocol
				body := []byte(`{"model":"` + model + `","prompt":"waves","duration":5,"ratio":"16:9","resolution":"auto"}`)
				info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
				require.NoError(t, err)
				require.True(t, info.ResolutionValid)
				require.Equal(t, "720p", info.Resolution)
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"request_id":"private-task","status":"pending"}`)),
				}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
				svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
				result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				if protocol == "sora" && model == "sora-2" {
					require.Equal(t, "1280x720", gjson.GetBytes(upstream.lastBody, "size").String())
					require.False(t, gjson.GetBytes(upstream.lastBody, "resolution").Exists())
				} else {
					require.Equal(t, "720p", gjson.GetBytes(upstream.lastBody, "resolution").String())
				}
				require.Equal(t, info.Resolution, result.VideoResolution)
				require.Equal(t, info.RawDurationSeconds, result.VideoDurationSeconds)
			})
		}
	}
}

func TestMediaProtocolSoraWorkbench(t *testing.T) {
	account := &Account{Credentials: map[string]any{"media_protocol": "sora"}}
	body, err := normalizeVideoProtocolBody(context.Background(), account, []byte(`{"model":"grok-imagine-video","prompt":"test","duration":4,"ratio":"16:9","resolution":"480p","referenceImages":["data:image/png;base64,YQ=="]}`))
	require.NoError(t, err)
	require.Equal(t, "4", gjson.GetBytes(body, "seconds").String())
	require.Equal(t, gjson.String, gjson.GetBytes(body, "seconds").Type)
	require.Equal(t, "854x480", gjson.GetBytes(body, "size").String())
	require.Equal(t, "16:9", gjson.GetBytes(body, "aspect_ratio").String())
	require.Equal(t, "data:image/png;base64,YQ==", gjson.GetBytes(body, "input_reference.image_url").String())
	require.False(t, gjson.GetBytes(body, "duration").Exists())
	require.False(t, gjson.GetBytes(body, "referenceImages").Exists())
	_, err = normalizeVideoProtocolBody(context.Background(), account, []byte(`{"model":"grok-imagine-video","referenceVideos":["https://example.com/v.mp4"]}`))
	require.Error(t, err)
}

func TestMediaProtocolEightYesVideo(t *testing.T) {
	account := &Account{Credentials: map[string]any{"media_protocol": "8yes", "base_url": "https://provider.example/v1"}, Platform: PlatformCustom, Type: AccountTypeAPIKey}
	body, err := normalizeVideoProtocolBody(context.Background(), account, []byte(`{"model":"wan-3.0-720p","seconds":"4","ratio":"1:1","resolution":"auto","referenceImages":["https://example.com/a.png"],"referenceAudios":["https://example.com/a.mp3"]}`))
	require.NoError(t, err)
	require.Equal(t, int64(4), gjson.GetBytes(body, "duration").Int())
	require.Equal(t, gjson.Number, gjson.GetBytes(body, "duration").Type)
	require.Equal(t, "1:1", gjson.GetBytes(body, "ratio").String())
	require.Equal(t, "https://example.com/a.png", gjson.GetBytes(body, "referenceImages.0").String())
	require.Equal(t, "https://example.com/a.mp3", gjson.GetBytes(body, "referenceAudios.0").String())
	require.False(t, gjson.GetBytes(body, "resolution").Exists())
	s := &OpenAIGatewayService{cfg: customVideoTestConfig()}
	for _, test := range []struct {
		endpoint GrokMediaEndpoint
		id, path string
	}{
		{GrokMediaEndpointVideosGenerations, "", "/v1/videos"},
		{GrokMediaEndpointVideoStatus, "task1", "/v1/videos/task1"},
		{GrokMediaEndpointVideoContent, "task1", "/v1/videos/task1/content"},
	} {
		url, err := s.buildCompatibleMediaURL(account, test.endpoint, test.id)
		require.NoError(t, err)
		require.Equal(t, "https://provider.example"+test.path, url)
	}
}

func TestMediaProtocolPixelSizeBilling(t *testing.T) {
	for _, resolution := range []string{"480p", "720p", "1080p", "2k", "4k"} {
		for _, ratio := range []string{"1:1", "16:9", "9:16"} {
			size, err := videoProtocolPixelSize(ratio, resolution)
			require.NoError(t, err)
			actual, ok := normalizeVideoBillingResolution(size)
			require.True(t, ok, size)
			require.Equal(t, resolution, actual)
		}
	}
	_, err := videoProtocolPixelSize("10000:1", "4k")
	require.Error(t, err)
}

func TestMediaProtocolDurationAndResolutionAdmission(t *testing.T) {
	for _, protocol := range []string{"sora", "8yes"} {
		account := &Account{Credentials: map[string]any{"media_protocol": protocol}}
		for _, body := range []string{
			`{"model":"grok-imagine-video","duration":-1}`,
			`{"model":"grok-imagine-video","duration":1.5}`,
			`{"model":"grok-imagine-video","duration":4,"seconds":"8"}`,
			`{"model":"grok-imagine-video","resolution":"unpriced"}`,
		} {
			_, err := VideoPrechargeRequestInfo(context.Background(), account, []byte(body), "application/json")
			require.Error(t, err, body)
		}
	}
	for _, test := range []struct {
		protocol, model string
		duration        int
	}{
		{"8yes", "seedance-2-5-720p", 8}, {"8yes", "grok-imagine-video-480p", 8}, {"sora", "grok-imagine-video", 8},
	} {
		account := &Account{Credentials: map[string]any{"media_protocol": test.protocol}}
		info, err := VideoPrechargeRequestInfo(context.Background(), account, []byte(`{"model":"`+test.model+`"}`), "application/json")
		require.NoError(t, err)
		require.Equal(t, test.duration, info.RawDurationSeconds)
	}
}
