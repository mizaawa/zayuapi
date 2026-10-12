//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEightYesForwardingMatchesPrechargeAndCanonicalID(t *testing.T) {
	for _, model := range []string{"videos-mini-480p", "seedance-2-5-1080p", "wan-3.0-4k"} {
		t.Run(model, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["media_protocol"] = "8yes"
			body := []byte(`{"model":"` + model + `","prompt":"waves","seconds":"8","size":"1280x720","resolution":"720p","referenceVideos":["https://cdn.example/guide.mp4"]}`)
			info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"task-public","task_id":"private-id","model":"different-model","status":"queued"}`)),
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "/proxy/v1/videos", upstream.lastReq.URL.Path)
			require.Equal(t, "task-public", result.ResponseID)
			require.Equal(t, model, result.Model)
			require.Equal(t, info.Resolution, result.VideoResolution)
			require.Equal(t, NormalizeVideoBillingResolutionForModelOrDefault(model, "", ""), result.VideoResolution)
			require.Equal(t, info.RawDurationSeconds, result.VideoDurationSeconds)
			require.True(t, info.HasReferenceVideo)
			require.Equal(t, int64(8), gjson.GetBytes(upstream.lastBody, "duration").Int())
			require.Equal(t, "16:9", gjson.GetBytes(upstream.lastBody, "ratio").String())
			for _, key := range []string{"seconds", "size", "resolution", "aspect_ratio"} {
				require.False(t, gjson.GetBytes(upstream.lastBody, key).Exists(), key)
			}
		})
	}
}

func TestEightYesNeverUsesInternalTaskID(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "8yes"
	body := []byte(`{"model":"videos-mini-480p","prompt":"waves"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"task_id":"internal","status":"queued"}`))}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json")
	require.NoError(t, err)
	require.Empty(t, result.ResponseID)
	require.Len(t, upstream.requests, 1)
}

func TestEightYesReferenceValidation(t *testing.T) {
	for _, fields := range []string{
		`"referenceImages":["http://127.0.0.1/image"]`,
		`"referenceImages":["https://user:password@example.com/image"]`,
		`"referenceVideos":["file:///etc/passwd"]`,
		`"referenceAudios":["https://localhost/audio"]`,
		`"referenceImages":["https://cdn.example/a"],"image":"https://cdn.example/b"`,
		`"duration":4,"seconds":"8"`,
		`"duration":1.5`,
		`"n":2`,
		`"ratio":"4:3"`,
		`"referenceImages":["data:image/png;base64,not-base64"]`,
	} {
		_, _, err := normalizeEightYesVideoRequest([]byte(`{"model":"videos-mini-480p",`+fields+`}`), "application/json")
		require.Error(t, err, fields)
	}
	for _, raw := range []string{"https://example.com:0/a", "https://example.com:65536/a", "https://example.com/a#fragment", "http://[::1]/a", "https://example.com/" + strings.Repeat("a", 8192)} {
		require.False(t, ValidVideoReferenceURL(raw), raw)
	}
	require.True(t, ValidVideoReferenceURL("https://cdn.example/image.png?signature=abc"))
}

func TestEightYesUploadsBecomeMultipartAndPreserveBilling(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "8yes"
	body := []byte(`{"model":"seedance-2-5-720p","duration":12,"referenceImages":["data:image/png;base64,YQ==","https://cdn.example/frame.png","https://cdn.example/frame2.png"],"referenceVideos":["data:video/mp4;base64,Yg==","https://cdn.example/video.mp4","https://cdn.example/video2.mp4"],"referenceAudios":["data:audio/mpeg;base64,Yw==","https://cdn.example/music.mp3","https://cdn.example/music2.mp3"]}`)
	adapted, contentType, err := normalizeEightYesVideoRequest(body, "application/json")
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(bytes.NewReader(adapted), params["boundary"])
	files := map[string]string{}
	fields := map[string][]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		if part.FileName() != "" {
			files[part.FormName()] = string(data)
		} else {
			fields[part.FormName()] = append(fields[part.FormName()], string(data))
		}
	}
	require.Equal(t, map[string]string{"referenceImages": "a", "referenceVideos": "b", "referenceAudios": "c"}, files)
	require.Equal(t, []string{"https://cdn.example/frame.png", "https://cdn.example/frame2.png"}, fields["referenceImages"])
	require.Equal(t, []string{"https://cdn.example/video.mp4", "https://cdn.example/video2.mp4"}, fields["referenceVideos"])
	require.Equal(t, []string{"https://cdn.example/music.mp3", "https://cdn.example/music2.mp3"}, fields["referenceAudios"])
	info, err := VideoPrechargeRequestInfo(context.Background(), account, body, "application/json")
	require.NoError(t, err)
	require.True(t, info.HasReferenceVideo)
	require.Equal(t, 12, info.RawDurationSeconds)
	require.Equal(t, "720p", info.Resolution)
	require.Len(t, info.Uploads, 1)
	require.Equal(t, fields["referenceImages"], info.InputImageURLs)
	require.Equal(t, fields["referenceVideos"], info.ReferenceVideoURLs)
	// Native multipart callers go through the same normalization as browser uploads.
	native, nativeType, err := normalizeEightYesVideoRequest(adapted, contentType)
	require.NoError(t, err)
	nativeInfo := ParseGrokMediaRequest(nativeType, native)
	require.True(t, nativeInfo.HasReferenceVideo)
	require.Equal(t, 12, nativeInfo.RawDurationSeconds)
	require.Equal(t, info.InputImageURLs, nativeInfo.InputImageURLs)
	require.Equal(t, info.ReferenceVideoURLs, nativeInfo.ReferenceVideoURLs)
}

func TestEightYesVideoUploadBounds(t *testing.T) {
	// Assert the production policy without constructing a 64 MiB fixture.
	require.Equal(t, VideoWorkbenchMaxRequestBytes, eightYesVideoMaxUploadBytes)
	require.GreaterOrEqual(t, eightYesVideoMaxUploadBytes, 47*1024*1024)
	for _, size := range []int{4, 5, 6} {
		input := strings.Repeat("x", size)
		for _, encoded := range []bool{false, true} {
			var source io.Reader = strings.NewReader(input)
			if encoded {
				source = base64.NewDecoder(base64.StdEncoding, strings.NewReader(base64.StdEncoding.EncodeToString([]byte(input))))
			}
			data, err := readEightYesVideoUpload(source, 5)
			if size > 5 {
				require.ErrorContains(t, err, "exceeds upload limit")
				require.Nil(t, data)
			} else {
				require.NoError(t, err)
				require.Equal(t, input, string(data))
			}
		}
	}
	reader := strings.NewReader("123456789")
	_, err := readEightYesVideoUpload(reader, 5)
	require.ErrorContains(t, err, "exceeds upload limit")
	require.Equal(t, 3, reader.Len(), "stop after one byte over the limit")
	_, err = readEightYesVideoUpload(base64.NewDecoder(base64.StdEncoding, strings.NewReader("invalid!")), 5)
	require.Error(t, err)
}

func TestEightYesGrokReferenceModes(t *testing.T) {
	for _, fields := range []string{`"referenceImages":["https://cdn.example/frame.png"]`, `"input_reference":{"image_url":"https://cdn.example/frame.png"}`} {
		body, err := normalizeEightYesVideoJSON([]byte(`{"model":"grok-imagine-video-1080p",` + fields + `}`))
		require.NoError(t, err)
		require.Equal(t, "https://cdn.example/frame.png", gjson.GetBytes(body, "image").String())
	}
	body, err := normalizeEightYesVideoJSON([]byte(`{"model":"grok-imagine-video-720p","reference_images":[{"url":"https://cdn.example/guide.png"}]}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/guide.png", gjson.GetBytes(body, "reference_images.0.url").String())
	_, err = normalizeEightYesVideoJSON([]byte(`{"model":"grok-imagine-video-1080p","reference_images":[{"url":"https://cdn.example/guide.png"}]}`))
	require.Error(t, err)
}

func TestEightYesImageProtocolPreservesOpenAIParameters(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "8yes"
	body := []byte(`{"model":"gpt-image-2","prompt":"waves","n":2,"size":"1536x1024","quality":"high","response_format":"b64_json"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"YQ=="},{"b64_json":"Yg=="}]}`)),
	}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, 2, result.ImageCount)
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Equal(t, "/proxy/v1/images/generations", upstream.lastReq.URL.Path)
}

func TestCustomAccountRejectsUnsupportedMediaProtocol(t *testing.T) {
	for _, protocol := range []any{"", "sora", "8yes", " 8YES "} {
		credentials := map[string]any{"api_key": "secret", "base_url": "https://provider.example", "media_protocol": protocol}
		require.NoError(t, validateCustomAccountCredentials(PlatformCustom, AccountTypeAPIKey, credentials))
	}
	for _, protocol := range []any{"unsupported", 123, true} {
		credentials := map[string]any{"api_key": "secret", "base_url": "https://provider.example", "media_protocol": protocol}
		require.Error(t, validateCustomAccountCredentials(PlatformCustom, AccountTypeAPIKey, credentials))
	}
	account := customVideoTestAccount()
	account.Credentials["media_protocol"] = "unsupported"
	_, err := VideoPrechargeRequestInfo(context.Background(), account, []byte(`{"model":"video-model"}`), "application/json")
	require.Error(t, err)
	svc := &OpenAIGatewayService{}
	_, err = svc.ForwardImages(context.Background(), nil, account, nil, &OpenAIImagesRequest{Model: "gpt-image-2", N: 1}, "")
	require.ErrorContains(t, err, "unsupported media protocol")
}
