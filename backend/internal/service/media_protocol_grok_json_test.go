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

func TestGrokOpenAIVideoDocumentedRequestAndWorkbenchReferences(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, fields, mode, reference string
		images                        int
		video                         bool
	}{
		{"text", ``, "", "", 0, false},
		{"workbench_image", `,"referenceImages":["https://example.com/first.png"]`, "image-to-video", "images.0", 1, false},
		{"workbench_images", `,"referenceImages":["https://example.com/first.png","https://example.com/last.png"]`, "reference-images", "images.0", 2, false},
		{"sora_reference", `,"input_reference":{"image_url":"https://example.com/first.png"}`, "image-to-video", "images.0", 1, false},
		{"video_edit", `,"referenceVideos":["https://example.com/first.png"]`, "video-edit", "reference_video_url", 0, true},
		{"native_video_edit", `,"mode":"video-edit","reference_video_url":"https://example.com/first.png"`, "video-edit", "reference_video_url", 0, true},
		{"documented_reference_mode", `,"mode":"reference-images","images":["https://example.com/first.png"],"first_frame_image_url":"https://example.com/first.png","last_frame_image_url":"https://example.com/last.png","reference_voice_ids":["voice_ursa"],"keyframes":[{"timestamp_s":4,"image_url":"https://example.com/key.png"}]`, "reference-images", "images.0", 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = "openai"
			body := []byte(`{"model":"grok-imagine-video","prompt":"waves","resolution":"720p"` + tc.fields + `}`)
			info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
			require.NoError(t, err)
			require.Equal(t, 8, info.RawDurationSeconds)
			require.Len(t, info.InputImageURLs, tc.images)
			require.Equal(t, tc.video, info.HasReferenceVideo)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"task_public","status":"queued"}`))}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.NoError(t, err)
			require.Equal(t, 8, result.VideoDurationSeconds)
			require.Equal(t, tc.mode, gjson.GetBytes(upstream.lastBody, "mode").String())
			require.Equal(t, int64(8), gjson.GetBytes(upstream.lastBody, "duration").Int())
			if tc.reference != "" {
				require.Equal(t, "https://example.com/first.png", gjson.GetBytes(upstream.lastBody, tc.reference).String())
			}
			for _, alias := range []string{"seconds", "referenceImages", "referenceVideos", "reference_image_urls", "reference_video_urls", "input_reference"} {
				require.False(t, gjson.GetBytes(upstream.lastBody, alias).Exists(), alias)
			}
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestGrokOpenAIVideoRejectsConflictingReferences(t *testing.T) {
	for _, fields := range []string{
		`"images":["a"],"referenceImages":["b"]`,
		`"images":["a"],"input_reference":"b"`,
		`"images":["a"],"reference_video_url":"b"`,
		`"mode":"image-to-video","images":["a","b"]`,
		`"mode":"video-edit","images":["a"]`,
		`"mode":"unknown"`,
		`"referenceVideos":["a","b"]`,
		`"referenceAudios":["a"]`,
		`"reference_video_url":42`,
	} {
		_, err := normalizeOpenAIVideoJSON([]byte(`{"model":"grok-imagine-video",` + fields + `}`))
		require.Error(t, err, fields)
	}
}
