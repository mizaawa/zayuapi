//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardCustomVideoGenerationUsesStandardAsyncEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{
		"model":"seedance-2-5-720p",
		"prompt":"turn and smile",
		"duration":8,
		"ratio":"16:9",
		"referenceImages":["https://assets.example.test/input.jpg"],
		"referenceVideos":["https://assets.example.test/guide.mp4"],
		"referenceAudios":["https://assets.example.test/music.mp3"]
	}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	account := customVideoTestAccount()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"task-standard-id","request_id":"legacy-request-id","task_id":"legacy-task-id","status":"queued"}`,
		)),
	}}
	cache := &videoBindingCache{}
	svc := &OpenAIGatewayService{
		cfg:          customVideoTestConfig(),
		cache:        cache,
		httpUpstream: upstream,
	}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, account, GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodPost, upstream.lastReq.Method)
	require.Equal(t, "https://custom.example.test/proxy/v1/videos", upstream.lastReq.URL.String())
	require.NotContains(t, upstream.lastReq.URL.Path, "/videos/generations")
	require.Equal(t, "Bearer custom-secret", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.JSONEq(t, string(body), string(upstream.lastBody))

	// The standard Sora response id wins over legacy aliases and is the value
	// persisted for subsequent status/content requests.
	require.Equal(t, "task-standard-id", result.ResponseID)
	require.Equal(t, "seedance-2-5-720p", result.BillingModel)
	require.Equal(t, 1, result.VideoCount)
	require.Empty(t, result.VideoResolution, "Custom must not infer resolution from the model suffix")
	require.Equal(t, 8, result.VideoDurationSeconds)
	require.InDelta(t, 1.4, result.VideoPriceMultiplier, 1e-12)
	require.Empty(t, recorder.Body.Bytes(), "creation success must remain buffered until task metadata is durable")

	groupID := int64(17)
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(
		context.Background(), &groupID, result.ResponseID, 21, 31, account.ID,
	))
	boundID, err := svc.ResolveGrokMediaVideoRequestAccount(
		context.Background(), &groupID, "task-standard-id", 21, 31,
	)
	require.NoError(t, err)
	require.Equal(t, account.ID, boundID)
	require.GreaterOrEqual(t, cache.lastTTL, 7*24*time.Hour)
	require.True(t, svc.WriteBufferedGrokMediaResponse(c, result))
	require.JSONEq(t, `{"id":"task-standard-id","request_id":"legacy-request-id","task_id":"legacy-task-id","status":"queued"}`, recorder.Body.String())
}

func TestForwardCustomVideoGenerationPreservesRequestedClipCount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"videos-standard-720p","prompt":"two clips","n":2,"duration":4}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-two-clips","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          customVideoTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.Equal(t, 2, result.VideoCount)
	require.Empty(t, result.VideoResolution, "Custom must not infer resolution from the model suffix")
	require.Equal(t, 4, result.VideoDurationSeconds)
}

func TestCustomVideoTextOnlyResolutionIsNotSilentlyChanged(t *testing.T) {
	for _, resolution := range []string{"", "1080p"} {
		t.Run("json_"+resolution, func(t *testing.T) {
			body := `{"model":"minimax-h3-f","prompt":"waves","duration":4,"ratio":"16:9"}`
			if resolution != "" {
				body = `{"model":"minimax-h3-f","prompt":"waves","duration":4,"ratio":"16:9","resolution":"1080p"}`
			}
			out, contentType, err := normalizeCustomVideoForwardBody(GrokMediaEndpointVideosGenerations, []byte(body), "application/json")
			require.NoError(t, err)
			require.Equal(t, "application/json", contentType)
			require.JSONEq(t, body, string(out))
		})
	}

	t.Run("canvas_multipart_1080p_without_image", func(t *testing.T) {
		fields := map[string]string{
			"model": "minimax-h3-f", "prompt": "waves", "seconds": "4",
			"size": "1920x1080", "resolution_name": "1080p", "mode": "frames",
			"generate_audio": "true", "watermark": "false",
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for name, value := range fields {
			require.NoError(t, writer.WriteField(name, value))
		}
		require.NoError(t, writer.Close())
		out, contentType, err := normalizeCustomVideoForwardBody(GrokMediaEndpointVideosGenerations, body.Bytes(), writer.FormDataContentType())
		require.NoError(t, err)
		_, params, err := mime.ParseMediaType(contentType)
		require.NoError(t, err)
		reader := multipart.NewReader(bytes.NewReader(out), params["boundary"])
		actual := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			require.Empty(t, part.FileName())
			data, err := io.ReadAll(part)
			require.NoError(t, err)
			require.NotContains(t, actual, part.FormName())
			actual[part.FormName()] = string(data)
		}
		delete(fields, "resolution_name")
		fields["resolution"] = "1080p"
		require.Equal(t, fields, actual, "text-only requests must not acquire synthetic reference images or lose their selected resolution")
	})
}

func TestParseGrokMediaRequestAcceptsStringClipCount(t *testing.T) {
	info := ParseGrokMediaRequest("application/json", []byte(`{"model":"videos-standard-720p","n":"2"}`))
	require.Equal(t, 2, info.N)
}

func TestForwardCustomVideoResolutionNormalizesToResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"custom-video-model","prompt":"waves","resolution":"720p","vendor_option":"keep"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-resolution","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.Equal(t, "720p", gjson.GetBytes(upstream.lastBody, "resolution").String())
	require.Equal(t, "keep", gjson.GetBytes(upstream.lastBody, "vendor_option").String())
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)
}

func TestForwardCustomVideoResolutionNameNormalizesToResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"custom-video-model","prompt":"waves","resolution":"invalid","resolution_name":"720"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-resolution-name","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.Equal(t, "720p", gjson.GetBytes(upstream.lastBody, "resolution").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "resolution_name").Exists())
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)
	require.Empty(t, recorder.Body.Bytes())
}

func TestForwardCustomVideoInvalidJSONResolutionIsRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"custom-video-model","prompt":"waves","resolution":"not-a-resolution","resolution_name":"also-invalid"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-invalid-resolution","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, gjson.GetBytes(upstream.lastBody, "resolution").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "resolution_name").Exists())
	require.Empty(t, result.VideoResolution)
	require.Empty(t, recorder.Body.Bytes())
}

func TestForwardCustomVideoMissingResolutionDoesNotUseModelSuffix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"videos-standard-720p","prompt":"waves"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-missing-resolution","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, gjson.GetBytes(upstream.lastBody, "resolution").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "resolution_name").Exists())
	require.Empty(t, result.VideoResolution)
}

func TestForwardCustomVideoInvalidMultipartResolutionIsRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "custom-video-model"))
	require.NoError(t, writer.WriteField("prompt", "waves"))
	require.NoError(t, writer.WriteField("resolution", "not-a-resolution"))
	require.NoError(t, writer.WriteField("resolution_name", "still-invalid"))
	part, err := writer.CreateFormFile("image", "input.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("preserve-this-file"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-invalid-multipart","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body.Bytes(), writer.FormDataContentType(),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.VideoResolution)
	require.Empty(t, recorder.Body.Bytes())

	mediaType, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
	fields := map[string][]string{}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		require.NoError(t, nextErr)
		data, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		fields[part.FormName()] = append(fields[part.FormName()], string(data))
	}
	require.NotContains(t, fields, "resolution")
	require.NotContains(t, fields, "resolution_name")
	require.Equal(t, []string{"preserve-this-file"}, fields["image"])
}

func TestForwardCustomVideoMultipartResolutionNameNormalizesToResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "custom-video-model"))
	require.NoError(t, writer.WriteField("resolution_name", "1280x720"))
	require.NoError(t, writer.Close())
	contentType := writer.FormDataContentType()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", contentType)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-valid-multipart","status":"queued"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body.Bytes(), contentType,
	)
	require.NoError(t, err)
	require.Equal(t, VideoBillingResolution720P, result.VideoResolution)

	mediaType, params, err := mime.ParseMediaType(upstream.lastReq.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"])
	fields := map[string][]string{}
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		require.NoError(t, nextErr)
		data, readErr := io.ReadAll(part)
		require.NoError(t, readErr)
		fields[part.FormName()] = append(fields[part.FormName()], string(data))
	}
	require.Equal(t, []string{"720p"}, fields["resolution"])
	require.NotContains(t, fields, "resolution_name")
}

func TestParseGrokMediaResolutionMetadataDistinguishesMissingAndInvalid(t *testing.T) {
	missing := ParseGrokMediaRequest("application/json", []byte(`{"model":"videos-standard-720p"}`))
	require.False(t, missing.ResolutionProvided)
	require.False(t, missing.ResolutionValid)
	require.Equal(t, "", missing.RawResolution)
	require.Equal(t, VideoBillingResolution720P, missing.Resolution, "Grok retains its model suffix default")

	invalid := ParseGrokMediaRequest("application/json", []byte(`{"model":"videos-standard-720p","resolution":"invalid"}`))
	require.True(t, invalid.ResolutionProvided)
	require.False(t, invalid.ResolutionValid)
	require.Equal(t, "invalid", invalid.RawResolution)
	require.Equal(t, VideoBillingResolution720P, invalid.Resolution, "Grok retains its legacy fallback")

	validAlias := ParseGrokMediaRequest("application/json", []byte(`{"model":"videos-standard-480p","resolution_name":"2160p"}`))
	require.True(t, validAlias.ResolutionProvided)
	require.True(t, validAlias.ResolutionValid)
	require.Equal(t, "2160p", validAlias.RawResolution)
	require.Equal(t, VideoBillingResolution4K, validAlias.Resolution)
	require.Equal(t, VideoBillingResolution480P, validAlias.GrokResolution, "resolution_name must not change Grok's historical fallback")
}

func TestForwardCustomVideoStatusUsesBoundTaskURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-abc123", nil)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"task-abc123","status":"processing"}`)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideoStatus, "task-abc123", nil, "",
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, "https://custom.example.test/proxy/v1/videos/task-abc123", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer custom-secret", upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastBody)
	require.Empty(t, recorder.Body.Bytes())
	require.True(t, svc.WriteBufferedGrokMediaResponse(c, result))
	require.JSONEq(t, `{"id":"task-abc123","status":"processing"}`, recorder.Body.String())
}

func TestForwardCustomVideoStatusPreservesSignedURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-abc123", nil)
	responseBody := `{"id":"task-abc123","status":"completed","url":"https://cdn.example.test/task-abc123.mp4?sig=one","video":{"url":"https://cdn.example.test/task-abc123-alt.mp4?sig=two"}}`

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideoStatus, "task-abc123", nil, "",
	)
	require.NoError(t, err)
	require.Empty(t, recorder.Body.Bytes())
	require.True(t, svc.WriteBufferedGrokMediaResponse(c, result))
	require.JSONEq(t, responseBody, recorder.Body.String())
}

func TestForwardCustomVideoContentFetchesContentDirectly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-abc123/content", nil)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"video/mp4"}},
		Body:          io.NopCloser(strings.NewReader("mp4-binary-content")),
		ContentLength: int64(len("mp4-binary-content")),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideoContent, "task-abc123", nil, "",
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	// Custom content retrieval is one direct request. Grok's preliminary status
	// lookup must not run for this platform.
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodGet, upstream.lastReq.Method)
	require.Equal(t, "https://custom.example.test/proxy/v1/videos/task-abc123/content", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer custom-secret", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "*/*", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	require.Equal(t, "mp4-binary-content", recorder.Body.String())
}

func TestForwardCustomVideoContentRetriesTransientBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDelay := grokMediaVideoContentRetryDelay
	grokMediaVideoContentRetryDelay = 0
	defer func() { grokMediaVideoContentRetryDelay = originalDelay }()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-abc123/content", nil)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("content is still being copied"))},
		{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("content is still being copied"))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("mp4-after-retry"))},
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideoContent, "task-abc123", nil, "",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "mp4-after-retry", recorder.Body.String())
}

func TestForwardCustomVideoContentStopsAfterSixBadGatewayResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDelay := grokMediaVideoContentRetryDelay
	grokMediaVideoContentRetryDelay = 0
	defer func() { grokMediaVideoContentRetryDelay = originalDelay }()

	responses := make([]*http.Response, grokMediaVideoContentMaxAttempts)
	for i := range responses {
		responses[i] = &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("content is still being copied")),
		}
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-abc123/content", nil)
	upstream := &httpUpstreamRecorder{responses: responses}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardGrokMedia(
		context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideoContent, "task-abc123", nil, "",
	)

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Len(t, upstream.requests, grokMediaVideoContentMaxAttempts)
}

func TestCustomVideoBillingResolutionFromModelSuffix(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{model: "videos-mini-480p", want: VideoBillingResolution480P},
		{model: "videos-standard-720p", want: VideoBillingResolution720P},
		{model: "seedance-2-5-1080p", want: VideoBillingResolution1080P},
		{model: "provider/wan-3.0-4k", want: VideoBillingResolution4K},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, NormalizeVideoBillingResolutionForModelOrDefault(tt.model, "", ""))
		})
	}
}

func TestCustomVideoBillingDurationUsesModelLimit(t *testing.T) {
	require.Equal(t, 30, NormalizeVideoBillingDurationSecondsForModelOrDefault("wan-3.0-720p", 30))
	require.Equal(t, 30, NormalizeVideoBillingDurationSecondsForModelOrDefault("provider/wan-3.0-pro-4k", 60))
	require.Equal(t, 15, NormalizeVideoBillingDurationSecondsForModelOrDefault("videos-mini-480p", 30))
	require.Equal(t, 15, NormalizeVideoBillingDurationSecondsForModelOrDefault("grok-imagine-video", 30))
}

func TestCustomVideoReferencePriceMultiplier(t *testing.T) {
	require.InDelta(t, 1.3, VideoReferencePriceMultiplier("videos-standard-720p", true), 1e-12)
	require.InDelta(t, 1.4, VideoReferencePriceMultiplier("provider/seedance-2-5-720p", true), 1e-12)
	require.InDelta(t, 1.0, VideoReferencePriceMultiplier("wan-3.0-720p", true), 1e-12)
	require.InDelta(t, 1.0, VideoReferencePriceMultiplier("videos-standard-720p", false), 1e-12)
}

func TestCustomVideoPreservesUpstreamErrorsAndMarksFailoverErrorsRaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"videos-mini-480p","prompt":"hello","duration":4}`)

	t.Run("non failover", func(t *testing.T) {
		responseBody := []byte(`{"error":{"type":"vendor_validation","message":"bad video input"},"vendor_code":19}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusUnprocessableEntity,
			Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}}
		svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

		result, err := svc.ForwardGrokMedia(
			context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
		)

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
		require.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
		require.Equal(t, responseBody, recorder.Body.Bytes())
	})

	t.Run("failover", func(t *testing.T) {
		responseBody := []byte(`{"error":{"type":"vendor_capacity","message":"busy"}}`)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}}
		svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}

		result, err := svc.ForwardGrokMedia(
			context.Background(), c, customVideoTestAccount(), GrokMediaEndpointVideosGenerations, "", body, "application/json",
		)

		require.Nil(t, result)
		var failoverErr *UpstreamFailoverError
		require.True(t, errors.As(err, &failoverErr))
		require.True(t, failoverErr.RawResponsePassthrough)
		require.Equal(t, responseBody, failoverErr.ResponseBody)
		require.Empty(t, recorder.Body.Bytes())
	})
}

func TestCustomVideoChannelBillingModeControlsUnits(t *testing.T) {
	const (
		model      = "videos-standard-720p"
		unitPrice  = 0.25
		videoCount = 2
		duration   = 4
		groupID    = int64(710)
	)
	referenceMultiplier := VideoReferencePriceMultiplier(model, true)
	require.InDelta(t, 1.3, referenceMultiplier, 1e-12)

	tests := []struct {
		name       string
		mode       BillingMode
		wantTotal  float64
		wantActual float64
	}{
		{
			name:       "video mode bills each generated second",
			mode:       BillingModeVideo,
			wantTotal:  unitPrice * videoCount * duration,
			wantActual: unitPrice * videoCount * duration * 1.3,
		},
		{
			name:       "per request mode bills each completed clip",
			mode:       BillingModePerRequest,
			wantTotal:  unitPrice * videoCount,
			wantActual: unitPrice * videoCount * 1.3,
		},
		{
			name:       "image mode bills each completed clip",
			mode:       BillingModeImage,
			wantTotal:  unitPrice * videoCount,
			wantActual: unitPrice * videoCount * 1.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			billingService := NewBillingService(&config.Config{}, nil)
			resolver := newCustomVideoChannelPricingResolverForTest(groupID, model, tt.mode, unitPrice)
			svc := &OpenAIGatewayService{billingService: billingService, resolver: resolver}
			apiKey := &APIKey{
				GroupID: testInt64Pointer(groupID),
				Group:   &Group{ID: groupID, Platform: PlatformComposite},
			}
			result := &OpenAIForwardResult{
				VideoCount:           videoCount,
				VideoResolution:      VideoBillingResolution720P,
				VideoDurationSeconds: duration,
			}

			cost := svc.calculateOpenAIVideoCost(
				context.Background(), model, apiKey, result, referenceMultiplier,
			)
			require.NotNil(t, cost)
			require.Equal(t, string(tt.mode), cost.BillingMode)
			require.InDelta(t, tt.wantTotal, cost.TotalCost, 1e-12)
			require.InDelta(t, tt.wantActual, cost.ActualCost, 1e-12)
		})
	}
}

func TestCustomVideoEmptyResolutionDoesNotUseGroup480Price(t *testing.T) {
	groupID := int64(714)
	group480Price := 0.25
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group: &Group{
			ID:             groupID,
			Platform:       PlatformComposite,
			VideoPrice480P: &group480Price,
		},
	}
	billingService := NewBillingService(&config.Config{}, nil)
	svc := &OpenAIGatewayService{billingService: billingService}

	cost := svc.calculateOpenAIVideoCost(
		context.Background(),
		"videos-standard-720p",
		apiKey,
		&OpenAIForwardResult{
			VideoCount:           1,
			VideoResolution:      "",
			VideoDurationSeconds: 4,
		},
		1,
	)

	require.NotNil(t, cost)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
	require.Zero(t, cost.TotalCost)
	require.Zero(t, cost.ActualCost)
}

func TestCompositeGroupGrokAccountKeepsGrokResolutionFallback(t *testing.T) {
	groupID := int64(715)
	group720Price := 0.35
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group: &Group{
			ID:             groupID,
			Platform:       PlatformComposite,
			VideoPrice720P: &group720Price,
		},
	}
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}

	cost := svc.calculateOpenAIVideoCost(
		context.Background(),
		"videos-standard-720p",
		apiKey,
		&OpenAIForwardResult{
			VideoCount:           1,
			VideoResolution:      "",
			VideoDurationSeconds: 4,
		},
		1,
		&Account{Platform: PlatformGrok},
	)

	require.NotNil(t, cost)
	require.Equal(t, string(BillingModeVideo), cost.BillingMode)
	require.InDelta(t, group720Price*4, cost.TotalCost, 1e-12)
}

func TestCompositeGroupUsageClassificationFollowsSelectedAccount(t *testing.T) {
	groupID := int64(716)
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group:   &Group{ID: groupID, Platform: PlatformComposite},
	}

	require.False(t, isCustomVideoUsageRequest(apiKey, &Account{Platform: PlatformGrok}))
	require.True(t, isCustomVideoUsageRequest(apiKey, &Account{Platform: PlatformCustom}))
}

func TestValidateCustomVideoPricingRejectsUnpricedModels(t *testing.T) {
	const groupID = int64(711)
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group:   &Group{ID: groupID, Platform: PlatformComposite},
	}
	svc := &OpenAIGatewayService{}

	err := svc.ValidateCustomVideoPricing(context.Background(), apiKey, "videos-mini-720p", VideoBillingResolution720P)
	require.ErrorIs(t, err, ErrCustomVideoPricingUnavailable)
}

func TestValidateCustomVideoPricingAcceptsChannelAndGroupPrices(t *testing.T) {
	const (
		groupID = int64(712)
		model   = "videos-mini-720p"
	)
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group:   &Group{ID: groupID, Platform: PlatformComposite},
	}
	svc := &OpenAIGatewayService{
		resolver: newCustomVideoChannelPricingResolverForTest(groupID, model, BillingModeVideo, 0.25),
	}
	require.NoError(t, svc.ValidateCustomVideoPricing(context.Background(), apiKey, model, VideoBillingResolution720P))

	groupPrice := 0.2
	apiKey.Group.VideoPrice720P = &groupPrice
	svc.resolver = nil
	require.NoError(t, svc.ValidateCustomVideoPricing(context.Background(), apiKey, model, VideoBillingResolution720P))
}

func TestValidateCustomVideoPricingRejectsOtherGroupModelOnSharedChannel(t *testing.T) {
	const groupID = int64(714)
	price := 0.2
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group: &Group{ID: groupID, Platform: PlatformComposite,
			ModelPricing: []ChannelModelPricing{{Platform: PlatformCustom, BillingMode: BillingModeVideo, Models: []string{"video-a"}, PerRequestPrice: &price}},
		},
	}
	svc := &OpenAIGatewayService{
		resolver: newCustomVideoChannelPricingResolverForTest(groupID, "video-b", BillingModeVideo, 0.25),
	}
	require.NoError(t, svc.ValidateCustomVideoPricing(context.Background(), apiKey, "video-a", VideoBillingResolution720P))
	require.ErrorIs(t, svc.ValidateCustomVideoPricing(context.Background(), apiKey, "video-b", VideoBillingResolution720P), ErrCustomVideoPricingUnavailable)
	apiKey.Group.VideoPrice720P = &price
	require.ErrorIs(t, svc.ValidateCustomVideoPricing(context.Background(), apiKey, "video-b", VideoBillingResolution720P), ErrCustomVideoPricingUnavailable)
}

func TestValidateCustomVideoPricingDoesNotUse480PGroupPriceFor4K(t *testing.T) {
	const groupID = int64(713)
	groupPrice := 0.2
	apiKey := &APIKey{
		GroupID: testInt64Pointer(groupID),
		Group: &Group{
			ID:             groupID,
			Platform:       PlatformComposite,
			VideoPrice480P: &groupPrice,
		},
	}
	svc := &OpenAIGatewayService{}

	err := svc.ValidateCustomVideoPricing(context.Background(), apiKey, "videos-ultra-4k", VideoBillingResolution4K)
	require.ErrorIs(t, err, ErrCustomVideoPricingUnavailable)
}

func customVideoTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	return cfg
}

func customVideoTestAccount() *Account {
	return &Account{
		ID:          901,
		Name:        "custom-video",
		Platform:    PlatformCustom,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "custom-secret",
			"base_url": "https://custom.example.test/proxy/v1",
		},
	}
}

type videoBindingCache struct {
	bindings map[string]int64
	lastTTL  time.Duration
}

func (c *videoBindingCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	accountID, ok := c.bindings[sessionHash]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return accountID, nil
}

func (c *videoBindingCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if c.bindings == nil {
		c.bindings = make(map[string]int64)
	}
	c.bindings[sessionHash] = accountID
	c.lastTTL = ttl
	return nil
}

func (c *videoBindingCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, ttl time.Duration) error {
	c.lastTTL = ttl
	return nil
}

func (c *videoBindingCache) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	delete(c.bindings, sessionHash)
	return nil
}

func newCustomVideoChannelPricingResolverForTest(groupID int64, model string, mode BillingMode, price float64) *ModelPricingResolver {
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformCustom,
		model:    model,
	}] = &ChannelModelPricing{
		Platform:        PlatformCustom,
		Models:          []string{model},
		BillingMode:     mode,
		PerRequestPrice: testFloat64Pointer(price),
		Intervals: []PricingInterval{{
			TierLabel:       VideoBillingResolution720P,
			PerRequestPrice: testFloat64Pointer(price),
		}},
	}
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformComposite
	cache.loadedAt = time.Now()

	channelService := &ChannelService{}
	channelService.cache.Store(cache)
	return NewModelPricingResolver(channelService, NewBillingService(&config.Config{}, nil))
}

func testInt64Pointer(value int64) *int64 {
	return &value
}

func testFloat64Pointer(value float64) *float64 {
	return &value
}
