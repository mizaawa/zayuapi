//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoBillingFieldsRejectDurationAmbiguity(t *testing.T) {
	for _, protocol := range []string{"", "sora", "8yes"} {
		account := customVideoTestAccount()
		account.Credentials["media_protocol"] = protocol
		for _, fields := range []string{
			`"duration":1,"seconds":"12"`,
			`"duration":1.5`, `"duration":"1.5"`, `"duration":true`,
			`"duration":null`, `"seconds":-1`, `"seconds":0`,
			`"duration":86401`, `"n":1.5`,
		} {
			t.Run(protocol+"/"+fields, func(t *testing.T) {
				body := []byte(`{"model":"video-720p",` + fields + `}`)
				_, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
				require.Error(t, err)
				upstream := &httpUpstreamRecorder{}
				svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
				_, err = svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
				require.Error(t, err)
				require.Empty(t, upstream.requests)
			})
		}
		for _, seconds := range []string{"12", "1", "1.5", "-1", "0", "86401", "invalid"} {
			t.Run(fmt.Sprintf("%s/multipart/%s", protocol, seconds), func(t *testing.T) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "video-720p"))
				require.NoError(t, writer.WriteField("seconds", seconds))
				require.NoError(t, writer.WriteField("duration", "12"))
				require.NoError(t, writer.Close())
				info, err := VideoPrechargeRequestInfo(context.Background(), account, body.Bytes(), writer.FormDataContentType())
				if seconds == "12" {
					require.NoError(t, err)
					require.Equal(t, 12, info.RawDurationSeconds)
				} else {
					require.Error(t, err)
				}
			})
		}
		info, err := VideoPrechargeRequestInfo(context.Background(), account, []byte(`{"model":"video-720p","duration":"12"}`), "application/json")
		require.NoError(t, err)
		require.Equal(t, 12, info.RawDurationSeconds, protocol)
	}
}

func TestVideoMultipartArrayReferencesRetainSurcharge(t *testing.T) {
	for _, name := range []string{"referenceVideos", "reference_videos", "video_refs"} {
		for _, file := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/file=%t", name, file), func(t *testing.T) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				require.NoError(t, writer.WriteField("model", "seedance-2-5-720p"))
				if file {
					part, err := writer.CreateFormFile(name+"[]", "clip.mp4")
					require.NoError(t, err)
					_, err = io.WriteString(part, "fixture")
					require.NoError(t, err)
				} else {
					require.NoError(t, writer.WriteField(name+"[]", "https://cdn.example.com/clip.mp4"))
				}
				require.NoError(t, writer.Close())
				info := ParseGrokMediaRequest(writer.FormDataContentType(), body.Bytes())
				require.True(t, info.HasReferenceVideo)
				require.InDelta(t, 1.4, VideoReferencePriceMultiplier(info.Model, info.HasReferenceVideo), 1e-12)
			})
		}
	}
}

func TestSoraMultipartDefaultDurationMatchesPrechargeAndUsage(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "sora"
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "waves"))
	require.NoError(t, writer.WriteField("size", "1280x720"))
	require.NoError(t, writer.Close())
	info, err := VideoPrechargeRequestInfo(context.Background(), account, body.Bytes(), writer.FormDataContentType())
	require.NoError(t, err)
	require.Equal(t, 4, info.RawDurationSeconds)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"sora-task","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body.Bytes(), writer.FormDataContentType())
	require.NoError(t, err)
	require.Equal(t, info.RawDurationSeconds, result.VideoDurationSeconds)
	require.Equal(t, info.Resolution, result.VideoResolution)
	require.Equal(t, body.Bytes(), upstream.lastBody)
}
