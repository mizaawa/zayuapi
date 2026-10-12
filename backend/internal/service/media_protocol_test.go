//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

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

func TestMediaProtocol968Video(t *testing.T) {
	account := &Account{Credentials: map[string]any{"media_protocol": "968", "base_url": "https://provider.example/v1"}, Platform: PlatformCustom, Type: AccountTypeAPIKey}
	body, err := normalizeVideoProtocolBody(context.Background(), account, []byte(`{"model":"wan3-t","seconds":"4","ratio":"1:1","resolution":"auto","referenceImages":["https://example.com/a.png"],"referenceAudios":["https://example.com/a.mp3"]}`))
	require.NoError(t, err)
	require.Equal(t, int64(4), gjson.GetBytes(body, "duration").Int())
	require.Equal(t, gjson.Number, gjson.GetBytes(body, "duration").Type)
	require.Equal(t, "1:1", gjson.GetBytes(body, "aspect_ratio").String())
	require.Equal(t, "https://example.com/a.png", gjson.GetBytes(body, "image_refs.0").String())
	require.Equal(t, "https://example.com/a.mp3", gjson.GetBytes(body, "audio_refs.0").String())
	require.False(t, gjson.GetBytes(body, "resolution").Exists())
	s := &OpenAIGatewayService{cfg: customVideoTestConfig()}
	for _, test := range []struct {
		endpoint GrokMediaEndpoint
		id, path string
	}{
		{GrokMediaEndpointVideosGenerations, "", "/v1/videos/generations"},
		{GrokMediaEndpointVideoStatus, "task1", "/v1/videos/generations/task1"},
		{GrokMediaEndpointVideoContent, "task1", "/v1/videos/task1/content"},
	} {
		url, err := s.buildCompatibleMediaURL(account, test.endpoint, test.id)
		require.NoError(t, err)
		require.Equal(t, "https://provider.example"+test.path, url)
	}
}

func TestMediaProtocol968Images(t *testing.T) {
	body, err := normalize968ImageBody([]byte(`{"model":"banana-2","prompt":"test","quality":"high"}`), &OpenAIImagesRequest{N: 1, Model: "banana-2", Prompt: "test", Size: "2048x1152", SizeTier: "2K", Quality: "high", InputImageURLs: []string{"https://example.com/a.png"}})
	require.NoError(t, err)
	require.Equal(t, "16:9", gjson.GetBytes(body, "size").String())
	require.Equal(t, "2k", gjson.GetBytes(body, "resolution").String())
	require.False(t, gjson.GetBytes(body, "quality").Exists())
	require.Equal(t, "https://example.com/a.png", gjson.GetBytes(body, "image_urls.0").String())
	_, err = normalize968ImageBody(nil, &OpenAIImagesRequest{N: 3})
	require.Error(t, err)
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
	for _, protocol := range []string{"sora", "968"} {
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
		{"968", "sd2.5", 30}, {"968", "sd-2.0", 15}, {"968", "wan3-t", 5}, {"sora", "grok-imagine-video", 8},
	} {
		account := &Account{Credentials: map[string]any{"media_protocol": test.protocol}}
		info, err := VideoPrechargeRequestInfo(context.Background(), account, []byte(`{"model":"`+test.model+`"}`), "application/json")
		require.NoError(t, err)
		require.Equal(t, test.duration, info.RawDurationSeconds)
	}
}
