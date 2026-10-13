package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

var videoContentDownloadHTTPClient = newSSRFSafeHTTPClient(15 * time.Minute)

func validateVideoContentDownloadURL(target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Fragment != "" || validateImageDownloadURL(target) != nil {
		return fmt.Errorf("unsafe video content URL")
	}
	return validateVideoContentDownloadPath(target)
}

func validateVideoContentDownloadPath(target *url.URL) error {
	// Parse already decodes one escaping layer. Refuse remaining escapes and
	// separators inside a segment so proxies cannot reinterpret the route.
	if target == nil || strings.ContainsAny(target.Path, "\\%") {
		return fmt.Errorf("unsafe video content path")
	}
	for _, part := range strings.Split(target.EscapedPath(), "/") {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/\\%") {
			return fmt.Errorf("unsafe video content path")
		}
	}
	return nil
}

func resolveVideoContentDownloadURL(raw string, base *url.URL) (*url.URL, error) {
	reference, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(raw) == "" || len(raw) > 8192 || base == nil || validateVideoContentDownloadPath(reference) != nil {
		return nil, fmt.Errorf("unsafe video content URL")
	}
	target := base.ResolveReference(reference)
	if err := validateVideoContentDownloadURL(target); err != nil {
		return nil, err
	}
	return target, nil
}

func newVideoContentDownloadRequest(ctx context.Context, target, byteRange string) (*http.Request, error) {
	parsed, err := url.Parse(target)
	if err != nil || validateVideoContentDownloadURL(parsed) != nil {
		return nil, fmt.Errorf("unsafe video content URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) // #nosec G704 -- Public HTTPS validation above; downloads also use safeDialContext.
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	return req, nil
}

func isVideoContentJSONResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// Some compatible providers publish the file only in the completed task, or
// use an internal task ID for its content route. Resolve these only after the
// canonical content route fails; never repeat a paid creation request.
func (s *OpenAIGatewayService) customMediaVideoContentFallback(ctx context.Context, account *Account, token, requestID string, original *http.Request, contentResp *http.Response) (*http.Response, error) {
	if contentResp.StatusCode >= 200 && contentResp.StatusCode < 300 && isVideoContentJSONResponse(contentResp) {
		body, err := readUpstreamResponseBodyLimited(contentResp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		_ = contentResp.Body.Close()
		contentResp.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		target, _, err := customMediaVideoContentURL(body, original.URL, false)
		if err != nil {
			return nil, err
		}
		if target != "" {
			return s.customMediaVideoContentDownload(ctx, account, token, original, target)
		}
	}
	statusURL, err := s.buildCompatibleMediaURL(account, GrokMediaEndpointVideoStatus, requestID)
	if err != nil {
		return nil, err
	}
	statusReq, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(ctx), http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	statusReq.Header.Set("Authorization", "Bearer "+token)
	statusReq.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(statusReq.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	statusResp, err := s.httpUpstream.Do(statusReq, proxyURL, account.ID, account.Concurrency)
	if err != nil || statusResp == nil {
		return nil, nil
	}
	defer func() { _ = statusResp.Body.Close() }()
	if statusResp.StatusCode < 200 || statusResp.StatusCode >= 300 {
		return nil, nil
	}
	body, err := readUpstreamResponseBodyLimited(statusResp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	if err != nil {
		return nil, fmt.Errorf("read video content fallback status: %w", err)
	}
	target, _, err := customMediaVideoContentURL(body, original.URL, true)
	if err != nil || target == "" {
		return nil, err
	}
	return s.customMediaVideoContentDownload(ctx, account, token, original, target)
}

func (s *OpenAIGatewayService) customMediaVideoContentDownload(ctx context.Context, account *Account, token string, original *http.Request, raw string) (*http.Response, error) {
	seen := map[string]bool{original.URL.String(): true}
	base := original.URL
	for attempt := 0; attempt < 5; attempt++ {
		target, err := resolveVideoContentDownloadURL(raw, base)
		if err != nil {
			return nil, err
		}
		if seen[target.String()] {
			return nil, fmt.Errorf("unsafe or repeated video content URL")
		}
		seen[target.String()] = true
		req, err := newVideoContentDownloadRequest(WithHTTPUpstreamRedirectsDisabled(ctx), target.String(), original.Header.Get("Range"))
		if err != nil {
			return nil, fmt.Errorf("invalid video content download URL")
		}
		var resp *http.Response
		if isSameProviderVideoContentURL(target, original.URL) {
			req.Header.Set("Authorization", "Bearer "+token)
			account.ApplyHeaderOverrides(req.Header)
			proxyURL := ""
			if account.ProxyID != nil && account.Proxy != nil {
				proxyURL = account.Proxy.URL()
			}
			resp, err = s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
		} else {
			// Each hop is rebuilt with only download headers. Even a same-origin
			// asset route must not receive account credentials or overrides.
			resp, err = videoContentDownloadHTTPClient.Do(req) // #nosec G704 -- Public HTTPS URL validated per hop; safeDialContext rejects private DNS results and redirects are disabled.
		}
		if err != nil || resp == nil {
			return nil, fmt.Errorf("video content download failed")
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			raw = resp.Header.Get("Location")
			base = target
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && isVideoContentJSONResponse(resp) {
			body, readErr := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			raw, _, err = customMediaVideoContentURL(body, target, false)
			if err != nil || raw == "" {
				return nil, fmt.Errorf("video content upstream returned JSON instead of a video file")
			}
			base = target
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("too many video content redirects")
}

func customMediaVideoContentURL(body []byte, canonical *url.URL, requireCompleted bool) (string, bool, error) {
	if canonical == nil || !gjson.ValidBytes(body) {
		return "", false, nil
	}
	status := strings.ToLower(firstNonEmpty(gjson.GetBytes(body, "status").String(), gjson.GetBytes(body, "data.status").String()))
	if status != "completed" && status != "succeeded" && status != "success" && status != "done" && (requireCompleted || status != "") {
		return "", false, nil
	}
	for _, field := range []string{"video.url", "video_url", "download_url", "output.url", "output.video_url", "metadata.url", "url", "data.video.url", "data.video_url", "data.download_url", "data.metadata.url", "data.url", "data.0.video.url", "data.0.video_url", "data.0.download_url", "data.0.metadata.url", "data.0.url"} {
		raw := strings.TrimSpace(gjson.GetBytes(body, field).String())
		if raw == "" {
			continue
		}
		target, err := resolveVideoContentDownloadURL(raw, canonical)
		if err != nil {
			return "", false, fmt.Errorf("unsafe video content fallback URL")
		}
		if target.String() == canonical.String() {
			continue
		}
		// Only the same provider's exact content-route family receives its API
		// key. CDN links (including same-origin asset paths) stay anonymous.
		return target.String(), isSameProviderVideoContentURL(target, canonical), nil
	}
	return "", false, nil
}

func isSameProviderVideoContentURL(target, canonical *url.URL) bool {
	if target == nil || canonical == nil || !strings.EqualFold(target.Scheme, canonical.Scheme) || !strings.EqualFold(target.Host, canonical.Host) || validateVideoContentDownloadURL(target) != nil {
		return false
	}
	canonicalParts := strings.Split(strings.TrimPrefix(canonical.EscapedPath(), "/"), "/")
	targetParts := strings.Split(strings.TrimPrefix(target.EscapedPath(), "/"), "/")
	return len(canonicalParts) >= 4 && len(targetParts) == len(canonicalParts) &&
		canonicalParts[len(canonicalParts)-3] == "videos" &&
		strings.Join(targetParts[:len(targetParts)-2], "/") == strings.Join(canonicalParts[:len(canonicalParts)-2], "/") &&
		targetParts[len(targetParts)-1] == "content" && targetParts[len(targetParts)-2] != ""
}
