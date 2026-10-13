//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
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
			for _, tc := range []struct {
				name       string
				fields     string
				duration   int
				refField   string
				refURL     string
				imageCount int
				hasVideo   bool
			}{
				{name: "defaults", fields: `{}`, duration: 5},
				{name: "single_image", fields: `{"duration":12,"input_reference":"https://cdn.example/input.png"}`, duration: 12, refField: "input_reference", refURL: "https://cdn.example/input.png", imageCount: 1},
				{name: "frames", fields: `{"first_frame_image_url":"https://cdn.example/first.png","last_frame_image_url":"https://cdn.example/last.png"}`, duration: 5, refField: "last_frame_image_url", refURL: "https://cdn.example/last.png", imageCount: 2},
				{name: "images", fields: `{"reference_image_urls":["https://cdn.example/frame.png"]}`, duration: 5, refField: "reference_image_urls.0", refURL: "https://cdn.example/frame.png", imageCount: 1},
				{name: "videos", fields: `{"reference_video_urls":["https://cdn.example/ref.mp4"]}`, duration: 5, refField: "reference_video_urls.0", refURL: "https://cdn.example/ref.mp4", hasVideo: true},
				{name: "audio", fields: `{"reference_audio_urls":["https://cdn.example/ref.mp3"]}`, duration: 5, refField: "reference_audio_urls.0", refURL: "https://cdn.example/ref.mp3"},
				{name: "workbench_images", fields: `{"seconds":"5","referenceImages":["https://cdn.example/frame.png"]}`, duration: 5, refField: "reference_image_urls.0", refURL: "https://cdn.example/frame.png", imageCount: 1},
				{name: "workbench_videos", fields: `{"referenceVideos":["https://cdn.example/ref.mp4"]}`, duration: 5, refField: "reference_video_urls.0", refURL: "https://cdn.example/ref.mp4", hasVideo: true},
				{name: "workbench_audio", fields: `{"referenceAudios":["https://cdn.example/ref.mp3"]}`, duration: 5, refField: "reference_audio_urls.0", refURL: "https://cdn.example/ref.mp3"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					account := customVideoTestAccount()
					account.Credentials["media_protocol"] = "openai"
					var fields map[string]any
					require.NoError(t, json.Unmarshal([]byte(tc.fields), &fields))
					fields["model"], fields["prompt"], fields["resolution"], fields["ratio"] = model, "waves", "480p", "16:9"
					body, err := json.Marshal(fields)
					require.NoError(t, err)
					info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
					require.NoError(t, err)
					require.Equal(t, tc.duration, info.RawDurationSeconds)
					require.Equal(t, "480p", info.Resolution)
					require.Equal(t, tc.hasVideo, info.HasReferenceVideo)
					require.Len(t, info.InputImageURLs, tc.imageCount)
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
					require.Equal(t, int64(tc.duration), gjson.GetBytes(upstream.lastBody, "duration").Int())
					require.Equal(t, "16:9", gjson.GetBytes(upstream.lastBody, "aspect_ratio").String())
					if tc.refField != "" {
						require.Equal(t, tc.refURL, gjson.GetBytes(upstream.lastBody, tc.refField).String())
					}
					for _, alias := range []string{"seconds", "referenceImages", "referenceVideos", "referenceAudios"} {
						require.False(t, gjson.GetBytes(upstream.lastBody, alias).Exists(), alias)
					}
					require.Equal(t, info.RawDurationSeconds, result.VideoDurationSeconds)
					require.Equal(t, info.Resolution, result.VideoResolution)
					require.Equal(t, "task_public", result.ResponseID)
				})
			}
		})
	}
}

func TestOpenAIVideoJSONValidationAndNativeReferences(t *testing.T) {
	for _, body := range []string{
		`{"duration":5,"seconds":"6"}`, `{"duration":1.5}`, `{"duration":0}`, `{"n":2}`,
		`{"resolution":"unpriced"}`, `{"resolution":720}`, `{"resolution":"480p","resolution_name":"720p"}`,
		`{"ratio":"16:9","aspect_ratio":"1:1"}`, `{"ratio":"0:1"}`,
		`{"referenceImages":["a"],"reference_image_urls":["b"]}`, `{"reference_video_urls":[{}]}`,
		`{"input_reference":null}`, `{"input_reference":{}}`, `{"input_reference":" "}`,
		`{"first_frame_image_url":[]}`, `{"last_frame_image_url":42}`,
	} {
		_, err := normalizeOpenAIVideoJSON([]byte(body))
		require.Error(t, err, body)
	}
	body, err := normalizeOpenAIVideoJSON([]byte(`{"model":"seedance-2.5","resolution":"auto","first_frame_image_url":"https://cdn.example/first.png","last_frame_image_url":"https://cdn.example/last.png","referenceImages":[],"referenceVideos":[],"referenceAudios":[]}`))
	require.NoError(t, err)
	require.Equal(t, "720p", gjson.GetBytes(body, "resolution").String())
	require.Equal(t, int64(5), gjson.GetBytes(body, "duration").Int())
	info := ParseGrokMediaRequest("application/json", body)
	require.Len(t, info.InputImageURLs, 2)
	require.True(t, validMediaProtocol("openai"))
	_, err = VideoPrechargeRequestInfo(context.Background(), &Account{Credentials: map[string]any{"media_protocol": "openai"}}, []byte("multipart"), "multipart/form-data")
	require.Error(t, err)
}

func TestOpenAIVideoRejectsMixedReferenceModesBeforePrecharge(t *testing.T) {
	modes := []map[string]any{
		{"input_reference": "https://cdn.example/input.png"},
		{"first_frame_image_url": "https://cdn.example/first.png", "last_frame_image_url": "https://cdn.example/last.png"},
		{"reference_image_urls": []string{"https://cdn.example/ref.png"}},
		{"reference_video_urls": []string{"https://cdn.example/ref.mp4"}},
		{"reference_audio_urls": []string{"https://cdn.example/ref.mp3"}},
		{"referenceImages": []string{"https://cdn.example/ref.png"}},
		{"referenceVideos": []string{"https://cdn.example/ref.mp4"}},
		{"referenceAudios": []string{"https://cdn.example/ref.mp3"}},
	}
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "openai"
	for i, first := range modes {
		for _, second := range modes[i+1:] {
			fields := map[string]any{"model": "seedance-2.5", "prompt": "waves"}
			for key, value := range first {
				fields[key] = value
			}
			for key, value := range second {
				fields[key] = value
			}
			body, err := json.Marshal(fields)
			require.NoError(t, err)
			_, err = VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
			require.ErrorContains(t, err, "reference", string(body))
		}
	}
}
