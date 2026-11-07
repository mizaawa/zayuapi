//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSoraBridgeConvertsAfterCustomProtocolSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, protocol, model, size, imageField string
	}{
		{"default", "", "custom-video", "1280x720", "referenceImages.0"},
		{"8yes", "8yes", "videos-mini-720p", "1280x720", "referenceImages.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = tc.protocol
			body := []byte(`{"model":"` + tc.model + `","prompt":"waves","seconds":"8","size":"` + tc.size + `","input_reference":{"image_url":"https://cdn.example.test/frame.png"}}`)
			ctx := WithSoraVideoBridge(context.Background())
			quoted, err := VideoPrechargeRequestInfo(ctx, account, body, "application/json")
			require.NoError(t, err)
			require.Equal(t, 8, quoted.RawDurationSeconds)
			require.Equal(t, VideoBillingResolution720P, quoted.Resolution)
			require.Len(t, quoted.InputImageURLs, 1)

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"task-public","task_id":"task-private","status":"pending"}`)),
			}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/sora/videos", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardGrokMedia(ctx, c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.NoError(t, err)
			require.Equal(t, "task-public", result.ResponseID)
			require.Equal(t, quoted.Resolution, result.VideoResolution)
			require.Equal(t, quoted.RawDurationSeconds, result.VideoDurationSeconds)
			require.Equal(t, "https://custom.example.test/proxy/v1/videos", upstream.lastReq.URL.String())
			require.Equal(t, "Bearer custom-secret", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, int64(8), gjson.GetBytes(upstream.lastBody, "duration").Int())
			require.Equal(t, "16:9", gjson.GetBytes(upstream.lastBody, "ratio").String())
			require.Equal(t, "https://cdn.example.test/frame.png", gjson.GetBytes(upstream.lastBody, tc.imageField).String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "seconds").Exists())
			require.False(t, gjson.GetBytes(upstream.lastBody, "size").Exists())
			require.Equal(t, "queued", gjson.GetBytes(result.BufferedResponseBody, "status").String())
			require.Equal(t, "video", gjson.GetBytes(result.BufferedResponseBody, "object").String())
			require.False(t, gjson.GetBytes(result.BufferedResponseBody, "task_id").Exists())
			require.Equal(t, tc.model, gjson.GetBytes(result.BufferedResponseBody, "model").String())
			require.Equal(t, tc.size, gjson.GetBytes(result.BufferedResponseBody, "size").String())
			require.True(t, svc.WriteBufferedGrokMediaResponse(c, result))
			require.Equal(t, http.StatusAccepted, recorder.Code)
		})
	}
}

func TestSoraBridgeStatusMapsLifecycleAndKeepsSettlementFields(t *testing.T) {
	for _, tc := range []struct {
		body, status string
	}{
		{`{"data":{"state":"waiting"}}`, "queued"},
		{`{"status":"running"}`, "in_progress"},
		{`{"status":"succeeded","video":{"duration":9,"resolution":"1080p","url":"https://cdn.example.test/a.mp4"}}`, "completed"},
		{`{"status":"cancelled","error":{"code":"provider_failed","message":"no capacity"}}`, "failed"},
	} {
		t.Run(tc.status+tc.body, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = "8yes"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/sora/videos/task-public", nil)
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardGrokMedia(WithSoraVideoBridge(context.Background()), c, account, GrokMediaEndpointVideoStatus, "task-public", nil, "")
			require.NoError(t, err)
			require.Equal(t, "task-public", gjson.GetBytes(result.BufferedResponseBody, "id").String())
			require.Equal(t, tc.status, gjson.GetBytes(result.BufferedResponseBody, "status").String())
			if tc.status == "completed" {
				require.Equal(t, int64(9), gjson.GetBytes(result.BufferedResponseBody, "duration").Int())
				require.Equal(t, "1080p", gjson.GetBytes(result.BufferedResponseBody, "resolution").String())
				require.Equal(t, "/v1/sora/videos/task-public/content", gjson.GetBytes(result.BufferedResponseBody, "content_url").String())
			}
			if tc.status == "failed" {
				require.True(t, GrokMediaVideoStatusFailed(result.BufferedResponseBody))
				require.Equal(t, "provider_failed", gjson.GetBytes(result.BufferedResponseBody, "error.code").String())
			}
		})
	}
}

func TestSoraBridgeDownloadsNonSoraStatusURL(t *testing.T) {
	account := customVideoTestAccount()
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"content not ready"}`))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"data":{"state":"done"},"metadata":{"url":"/proxy/v1/videos/task-file/content"}}`))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video-bytes"))},
	}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sora/videos/task-public/content", nil)
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	_, err := svc.ForwardGrokMedia(WithSoraVideoBridge(context.Background()), c, account, GrokMediaEndpointVideoContent, "task-public", nil, "")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "https://custom.example.test/proxy/v1/videos/task-public/content", upstream.requests[0].URL.String())
	require.Equal(t, "https://custom.example.test/proxy/v1/videos/task-public", upstream.requests[1].URL.String())
	require.Equal(t, "https://custom.example.test/proxy/v1/videos/task-file/content", upstream.requests[2].URL.String())
	require.Equal(t, "Bearer custom-secret", upstream.requests[2].Header.Get("Authorization"))
	require.Equal(t, "video-bytes", recorder.Body.String())
}

func TestSoraBridgeKeepsNativeSoraAndValidatesBeforeBilling(t *testing.T) {
	body := []byte(`{"model":"sora-2","prompt":"waves","seconds":"8","size":"1280x720"}`)
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "sora"
	converted, contentType, err := normalizeSoraBridgeVideoRequest(account, body, "application/json")
	require.NoError(t, err)
	require.Equal(t, body, converted)
	require.Equal(t, "application/json", contentType)
	account.Credentials["media_protocol"] = "8yes"
	_, err = VideoPrechargeRequestInfo(WithSoraVideoBridge(context.Background()), account, body, "application/json")
	var validation *SoraVideoBridgeValidationError
	require.ErrorAs(t, err, &validation)
	_, err = normalizeSoraBridgeVideoResponse(GrokMediaEndpointVideoStatus, "owner-task", soraBridgeVideoRequest{}, []byte(`{"id":"different-task","status":"completed"}`))
	require.ErrorContains(t, err, "different task ID")
}

func TestSoraBridgeImmediateCompletionRetainsActualBillingMetadata(t *testing.T) {
	body, err := normalizeSoraBridgeVideoResponse(GrokMediaEndpointVideosGenerations, "", soraBridgeVideoRequest{
		Model: "custom-video", Seconds: 8, Size: "1280x720",
	}, []byte(`{"id":"task-1","status":"completed","video":{"duration":10,"resolution":"1080p"}}`))
	require.NoError(t, err)
	require.Equal(t, int64(10), gjson.GetBytes(body, "duration").Int())
	require.Equal(t, "10", gjson.GetBytes(body, "seconds").String())
	require.Equal(t, "1080p", gjson.GetBytes(body, "resolution").String())
	body, err = normalizeSoraBridgeVideoResponse(GrokMediaEndpointVideoStatus, "task-1", soraBridgeVideoRequest{}, []byte(`{"status":"completed","data":{"state":"failed"}}`))
	require.NoError(t, err)
	require.True(t, GrokMediaVideoStatusFailed(body), "failure wins over contradictory success fields")
}

func TestSoraBridgeRejectsAmbiguousOrUnsupportedInputs(t *testing.T) {
	account := customVideoTestAccount()
	for _, body := range []string{
		`{"model":"video","prompt":"waves","seconds":0,"size":"1280x720"}`,
		`{"model":"video","prompt":"waves","seconds":8.5,"size":"1280x720"}`,
		`{"model":"video","prompt":"waves","seconds":8,"size":"1280x720","duration":8}`,
		`{"model":"video","prompt":"waves","seconds":8,"size":"1000x1000"}`,
		`{"model":"video","prompt":"waves","seconds":8,"size":"1280x720","input_reference":"http://127.0.0.1/a.png"}`,
	} {
		_, _, err := normalizeSoraBridgeVideoRequest(account, []byte(body), "application/json")
		require.Error(t, err, body)
	}
	account.Credentials["media_protocol"] = "8yes"
	_, _, err := normalizeSoraBridgeVideoRequest(account, []byte(`{"model":"videos-mini-480p","prompt":"waves","seconds":8,"size":"1280x720"}`), "application/json")
	require.ErrorContains(t, err, "model resolution must match")
}

func TestSoraBridgeMultipartImageBecomesWorkbenchReference(t *testing.T) {
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/pS8AAAAASUVORK5CYII=")
	require.NoError(t, err)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{"model": "MiniMax-H3", "prompt": "waves", "seconds": "5", "size": "1280x720"} {
		require.NoError(t, writer.WriteField(name, value))
	}
	part, err := writer.CreateFormFile("input_reference", "frame.png")
	require.NoError(t, err)
	_, err = part.Write(image)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	account := customVideoTestAccount()
	converted, _, err := normalizeSoraBridgeVideoRequest(account, body.Bytes(), writer.FormDataContentType())
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(gjson.GetBytes(converted, "referenceImages.0").String(), "data:image/png;base64,"))
	providerBody, err := normalizeMiniMaxVideoWorkbenchBody(WithVideoWorkbenchContext(context.Background()), converted)
	require.NoError(t, err)
	require.Equal(t, "image_url", gjson.GetBytes(providerBody, "metadata.content.1.type").String())
}

func TestSoraBridgeHTTPErrorEnvelopeDoesNotChangeNativePassthrough(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusServiceUnavailable} {
		for _, bridge := range []bool{false, true} {
			body := []byte(`{"model":"custom-video","prompt":"waves","seconds":"8","size":"1280x720"}`)
			nativeError := `{"message":"provider unavailable","vendor_code":17}`
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: status, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader(nativeError)),
			}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
			ctx := context.Background()
			if bridge {
				ctx = WithSoraVideoBridge(ctx)
			}
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			_, err := svc.ForwardGrokMedia(ctx, c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "a bridge must never replay paid creation")
			responseBody := recorder.Body.Bytes()
			var failover *UpstreamFailoverError
			if errors.As(err, &failover) {
				responseBody = failover.ResponseBody
				require.Equal(t, status, failover.StatusCode)
			}
			if bridge {
				require.Equal(t, "upstream_error", gjson.GetBytes(responseBody, "error.code").String())
				require.NotEmpty(t, gjson.GetBytes(responseBody, "error.message").String())
				require.False(t, gjson.GetBytes(responseBody, "vendor_code").Exists())
			} else {
				require.JSONEq(t, nativeError, string(responseBody))
			}
		}
	}
}
