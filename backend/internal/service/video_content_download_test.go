//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type videoContentDownloadTransport func(*http.Request) (*http.Response, error)

func (f videoContentDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func videoContentDownloadTestResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {contentType}},
		Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)),
	}
}

func TestCustomVideoContentDownloadsCompletedMetadataURL(t *testing.T) {
	for _, protocol := range []string{"sora", "openai"} {
		t.Run(protocol, func(t *testing.T) {
			account := customVideoTestAccount()
			account.Credentials["base_url"] = "https://relay.example/v1"
			account.Credentials["media_protocol"] = protocol
			upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
				videoContentDownloadTestResponse(403, "application/json", `{"error":{"message":"unsupported protocol"}}`),
				grokMediaContentStatusResponse(`{"status":"completed","metadata":{"url":"/v1/videos/internal-id/content"}}`),
				videoContentDownloadTestResponse(206, "video/mp4", "video-payload"),
			}}
			svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
			c, recorder := grokMediaContentTestContext(http.MethodGet, "/v1/videos/public-id/content", map[string]string{"Range": "bytes=0-12"})

			_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoContent, "public-id", nil, "")

			require.NoError(t, err)
			require.Equal(t, http.StatusPartialContent, recorder.Code)
			require.Equal(t, "video-payload", recorder.Body.String())
			require.Len(t, upstream.requests, 3)
			require.Equal(t, "https://relay.example/v1/videos/internal-id/content", upstream.requests[2].URL.String())
			for _, req := range upstream.requests {
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "Bearer custom-secret", req.Header.Get("Authorization"))
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
			}
			require.Equal(t, "bytes=0-12", upstream.requests[2].Header.Get("Range"))
		})
	}
}

func TestCustomVideoContentBrokenProviderStillFails(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["base_url"] = "https://relay.example/v1"
	account.Credentials["media_protocol"] = "sora"
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		videoContentDownloadTestResponse(403, "application/json", `{"error":{"message":"unsupported protocol"}}`),
		grokMediaContentStatusResponse(`{"status":"completed","metadata":{"url":"/v1/videos/internal-id/content"}}`),
		videoContentDownloadTestResponse(404, "application/json", `{"error":{"message":"Task not found"}}`),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	c, recorder := grokMediaContentTestContext(http.MethodGet, "/v1/videos/public-id/content", nil)

	_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoContent, "public-id", nil, "")

	require.Error(t, err)
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Task not found")
	require.False(t, c.GetBool(GrokMediaContentCompleteKey))
	require.Len(t, upstream.requests, 3)
	for _, req := range upstream.requests {
		require.Equal(t, http.MethodGet, req.Method)
	}
}

func TestCustomVideoContentJSONURLAndRedirectDoNotLeakCredentials(t *testing.T) {
	for _, firstResponse := range []*http.Response{
		grokMediaContentStatusResponse(`{"url":"https://cdn.example/first.mp4"}`),
		{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://cdn.example/first.mp4"}}, Body: io.NopCloser(strings.NewReader(""))},
	} {
		account := customVideoTestAccount()
		account.Credentials["media_protocol"] = "openai"
		account.Credentials[credKeyHeaderOverrideEnabled] = true
		account.Credentials[credKeyHeaderOverrides] = map[string]any{"user-agent": "private-agent", "x-api-key": "private-key"}
		upstream := &grokMediaContentUpstreamStub{response: firstResponse}
		svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
		var downloads []*http.Request
		oldTransport := videoContentDownloadHTTPClient.Transport
		videoContentDownloadHTTPClient.Transport = videoContentDownloadTransport(func(req *http.Request) (*http.Response, error) {
			downloads = append(downloads, req)
			if len(downloads) == 1 {
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://other-cdn.example/final.mp4"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return videoContentDownloadTestResponse(200, "video/mp4", "cdn-video"), nil
		})
		c, recorder := grokMediaContentTestContext(http.MethodGet, "/v1/videos/public-id/content", map[string]string{"Range": "bytes=0-12"})
		_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoContent, "public-id", nil, "")
		videoContentDownloadHTTPClient.Transport = oldTransport

		require.NoError(t, err)
		require.Equal(t, "cdn-video", recorder.Body.String())
		require.Len(t, upstream.requests, 1)
		require.Len(t, downloads, 2)
		for _, req := range downloads {
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("X-API-Key"))
			require.Empty(t, req.Header.Get("User-Agent"))
			require.Equal(t, "bytes=0-12", req.Header.Get("Range"))
		}
	}
}

func TestCustomVideoContentRejectsUnsafeURLAndTraversal(t *testing.T) {
	canonical, err := url.Parse("https://relay.example/v1/videos/public-id/content")
	require.NoError(t, err)
	for _, raw := range []string{
		"http://cdn.example/video.mp4", "file:///etc/passwd", "https://localhost/video.mp4",
		"https://127.0.0.1/video.mp4", "https://169.254.169.254/video.mp4", "https://[::1]/video.mp4",
		"https://user:secret@relay.example/v1/videos/id/content", "/v1/videos/../admin/content",
		"/v1/videos/%2e%2e/content", "/v1/videos/%252e%252e/content", "/v1/videos/id%2f..%2fadmin/content",
		"/v1/videos/id%5c..%5cadmin/content", "https://cdn.example/video.mp4#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"status": "completed", "metadata": map[string]string{"url": raw}})
			require.NoError(t, err)
			_, _, err = customMediaVideoContentURL(body, canonical, true)
			require.Error(t, err)
		})
	}
}

func TestCustomVideoContentRecognizesURLFieldsAndExactCredentialBoundary(t *testing.T) {
	canonical, err := url.Parse("https://relay.example/v1/videos/public-id/content")
	require.NoError(t, err)
	for _, body := range []string{
		`{"metadata":{"url":"/v1/videos/internal-id/content"}}`,
		`{"video":{"url":"/v1/videos/internal-id/content"}}`,
		`{"video_url":"/v1/videos/internal-id/content"}`,
		`{"download_url":"/v1/videos/internal-id/content"}`,
		`{"url":"/v1/videos/internal-id/content"}`,
		`{"data":{"metadata":{"url":"/v1/videos/internal-id/content"}}}`,
		`{"data":{"video":{"url":"/v1/videos/internal-id/content"}}}`,
		`{"data":{"download_url":"/v1/videos/internal-id/content"}}`,
		`{"data":[{"url":"/v1/videos/internal-id/content"}]}`,
	} {
		target, authenticated, err := customMediaVideoContentURL([]byte(body), canonical, false)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example/v1/videos/internal-id/content", target)
		require.True(t, authenticated)
	}
	for _, raw := range []string{"https://relay.example/asset/video.mp4", "https://relay.example:444/v1/videos/id/content", "https://cdn.example/v1/videos/id/content", "https://relay.example/v1/videos/id/content/"} {
		target, err := url.Parse(raw)
		require.NoError(t, err)
		require.False(t, isSameProviderVideoContentURL(target, canonical), raw)
	}
	_, _, err = customMediaVideoContentURL([]byte(`{"status":"failed","metadata":{"url":"https://cdn.example/video.mp4"}}`), canonical, true)
	require.NoError(t, err)
}

func TestCustomVideoContentRedirectLoopIsRejected(t *testing.T) {
	account := customVideoTestAccount()
	account.Credentials["base_url"] = "https://relay.example/v1"
	upstream := &grokMediaContentUpstreamStub{response: &http.Response{
		StatusCode: http.StatusFound, Header: http.Header{"Location": {"/v1/videos/public-id/content"}}, Body: io.NopCloser(strings.NewReader("")),
	}}
	svc := &OpenAIGatewayService{cfg: customVideoTestConfig(), httpUpstream: upstream}
	original, err := http.NewRequest(http.MethodGet, "https://relay.example/v1/videos/public-id/content", nil)
	require.NoError(t, err)

	_, err = svc.customMediaVideoContentDownload(context.Background(), account, "secret", original, "/v1/videos/internal-id/content")

	require.ErrorContains(t, err, "repeated video content URL")
	require.Len(t, upstream.requests, 1)
}
