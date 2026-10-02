package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type webDAVBackupStore struct {
	baseURL  *url.URL
	username string
	password string
	rootPath string
	client   *http.Client
}

func newWebDAVBackupStore(cfg *service.BackupS3Config) (*webDAVBackupStore, error) {
	parsed, err := url.Parse(strings.TrimSpace(cfg.WebDAVURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid WebDAV URL: expected an http(s) URL without embedded credentials, query, or fragment")
	}
	rootPath, err := cleanWebDAVPath(cfg.WebDAVPath)
	if err != nil {
		return nil, fmt.Errorf("invalid WebDAV storage path: %w", err)
	}
	return &webDAVBackupStore{
		baseURL:  parsed,
		username: cfg.WebDAVUsername,
		password: cfg.WebDAVPassword,
		rootPath: rootPath,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 30 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return checkWebDAVRedirect(req, via, parsed)
			},
		},
	}, nil
}

func checkWebDAVRedirect(req *http.Request, via []*http.Request, webDAVURL *url.URL) error {
	if len(via) == 0 {
		return fmt.Errorf("invalid WebDAV redirect chain")
	}
	if len(via) >= 6 {
		return fmt.Errorf("too many WebDAV redirects")
	}
	previous := via[len(via)-1]
	if req.URL.User != nil {
		return fmt.Errorf("WebDAV redirects with embedded credentials are not allowed")
	}
	if sameWebDAVOrigin(previous.URL, req.URL) {
		return nil
	}

	if previous.Method != http.MethodGet && previous.Method != http.MethodHead {
		return fmt.Errorf("cross-origin WebDAV redirects are only allowed for GET and HEAD")
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return fmt.Errorf("cross-origin WebDAV redirects are only allowed for GET and HEAD")
	}
	if !strings.EqualFold(webDAVURL.Scheme, "https") || !strings.EqualFold(previous.URL.Scheme, "https") || !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("insecure cross-origin WebDAV redirect was rejected")
	}
	if previous.URL.User != nil || !isPikPakWebDAVHost(webDAVURL.Hostname()) || !isPikPakRedirectHost(previous.URL.Hostname()) || !isPikPakRedirectHost(req.URL.Hostname()) {
		return fmt.Errorf("cross-origin WebDAV redirect to an untrusted host was rejected")
	}
	if port := req.URL.Port(); port != "" && port != "443" {
		return fmt.Errorf("cross-origin WebDAV redirect to a non-HTTPS port was rejected")
	}
	if net.ParseIP(req.URL.Hostname()) != nil {
		return fmt.Errorf("cross-origin WebDAV redirect to an IP address was rejected")
	}

	// PikPak redirects downloads to signed CDN URLs. Never forward WebDAV
	// credentials or cookies to that separate origin.
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	req.Header.Del("Cookie2")
	req.Header.Del("Proxy-Authorization")
	return nil
}

func sameWebDAVOrigin(a, b *url.URL) bool {
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(strings.TrimSuffix(a.Hostname(), "."), strings.TrimSuffix(b.Hostname(), ".")) {
		return false
	}
	return effectiveWebDAVPort(a) == effectiveWebDAVPort(b)
}

func effectiveWebDAVPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}

func isPikPakWebDAVHost(host string) bool {
	return isHostOrSubdomain(host, "pikpak.ai") || isHostOrSubdomain(host, "mypikpak.com")
}

func isPikPakRedirectHost(host string) bool {
	return isPikPakWebDAVHost(host) || isHostOrSubdomain(host, "mypikpak.net")
}

func isHostOrSubdomain(host, domain string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func cleanWebDAVPath(value string) (string, error) {
	value = strings.TrimSpace(strings.Trim(value, "/"))
	if value == "" {
		return "", nil
	}
	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("backslashes are not allowed")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("path must contain only ordinary relative segments")
		}
	}
	return value, nil
}

func (s *webDAVBackupStore) objectURL(key string) (*url.URL, error) {
	if key == "" || strings.Contains(key, "\\") {
		return nil, fmt.Errorf("invalid WebDAV object key")
	}
	for _, segment := range strings.Split(strings.Trim(key, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return nil, fmt.Errorf("invalid WebDAV object key")
		}
	}
	u := *s.baseURL
	var segments []string
	if s.rootPath != "" {
		segments = append(segments, strings.Split(s.rootPath, "/")...)
	}
	segments = append(segments, strings.Split(strings.Trim(key, "/"), "/")...)
	u.Path = path.Join(append([]string{u.Path}, segments...)...)
	// Keep Path decoded so net/url escapes each Unicode path segment exactly
	// once when the request URL is serialized.
	u.RawPath = ""
	return &u, nil
}

func (s *webDAVBackupStore) do(ctx context.Context, method, key string, body io.Reader, contentType string) (*http.Response, error) {
	target, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	return s.doTarget(ctx, method, target, body, contentType)
}

func (s *webDAVBackupStore) doTarget(ctx context.Context, method string, target *url.URL, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.username, s.password)
	req.Header.Set("User-Agent", "zayuapi-backup/1.0")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	finish := servertiming.ObserveDependency(ctx, "webdav")
	resp, err := s.client.Do(req)
	finish()
	return resp, err
}

func (s *webDAVBackupStore) ensureParentDirectories(ctx context.Context, key string) error {
	if _, err := s.objectURL(key); err != nil {
		return err
	}
	parts := make([]string, 0)
	if s.rootPath != "" {
		parts = append(parts, strings.Split(s.rootPath, "/")...)
	}
	keyParts := strings.Split(strings.Trim(key, "/"), "/")
	parts = append(parts, keyParts[:len(keyParts)-1]...)
	parent := ""
	for _, part := range parts {
		parent = path.Join(parent, part)
		target := *s.baseURL
		target.Path = path.Join(target.Path, parent)
		target.RawPath = ""
		resp, err := s.doTarget(ctx, "MKCOL", &target, nil, "")
		if err != nil {
			return fmt.Errorf("WebDAV MKCOL: %w", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMethodNotAllowed {
			return fmt.Errorf("WebDAV MKCOL failed: %s", resp.Status)
		}
	}
	return nil
}

func (s *webDAVBackupStore) Upload(ctx context.Context, key string, body io.Reader, contentType string) (int64, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, fmt.Errorf("read backup body: %w", err)
	}
	if err := s.ensureParentDirectories(ctx, key); err != nil {
		return 0, err
	}
	resp, err := s.do(ctx, http.MethodPut, key, bytes.NewReader(data), contentType)
	if err != nil {
		return 0, fmt.Errorf("WebDAV PUT: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("WebDAV PUT failed: %s", resp.Status)
	}
	return int64(len(data)), nil
}

func (s *webDAVBackupStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.do(ctx, http.MethodGet, key, nil, "")
	if err != nil {
		return nil, fmt.Errorf("WebDAV GET: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("WebDAV GET failed: %s", resp.Status)
	}
	return resp.Body, nil
}

func (s *webDAVBackupStore) Delete(ctx context.Context, key string) error {
	resp, err := s.do(ctx, http.MethodDelete, key, nil, "")
	if err != nil {
		return fmt.Errorf("WebDAV DELETE: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WebDAV DELETE failed: %s", resp.Status)
	}
	return nil
}

func (s *webDAVBackupStore) PresignURL(context.Context, string, time.Duration) (string, error) {
	return "", fmt.Errorf("WebDAV does not support presigned URLs")
}

func (s *webDAVBackupStore) HeadBucket(ctx context.Context) error {
	// Some cloud-drive WebDAV implementations accept hidden-file PUTs but hide
	// dot-prefixed files from subsequent GET requests. Use a normal temporary
	// filename so the write/read/delete probe reflects regular backup objects.
	key := "zayuapi-connection-test-" + uuid.NewString() + ".txt"
	content := []byte("zayuapi-webdav-connection-test")
	if _, err := s.Upload(ctx, key, bytes.NewReader(content), "application/octet-stream"); err != nil {
		return fmt.Errorf("WebDAV connection test upload failed: %w", err)
	}
	deleted := false
	defer func() {
		if !deleted {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = s.Delete(cleanupCtx, key)
		}
	}()
	var got []byte
	backoffs := [...]time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond, 3200 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		resp, err := s.do(ctx, http.MethodGet, key, nil, "")
		if err != nil {
			return fmt.Errorf("WebDAV connection test download failed: %w", err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			got, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return fmt.Errorf("WebDAV connection test read failed: %w", err)
			}
			break
		}
		_ = resp.Body.Close()
		if !retryableWebDAVProbeStatus(resp, s.baseURL) || attempt >= len(backoffs) {
			return fmt.Errorf("WebDAV connection test download failed: %s", describeWebDAVGetFailure(resp, s.baseURL))
		}
		timer := time.NewTimer(backoffs[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("WebDAV connection test download failed: %w", ctx.Err())
		case <-timer.C:
		}
	}
	if !bytes.Equal(got, content) {
		return fmt.Errorf("WebDAV connection test returned unexpected data")
	}
	if err := s.Delete(ctx, key); err != nil {
		return fmt.Errorf("WebDAV connection test cleanup failed: %w", err)
	}
	deleted = true
	return nil
}

func retryableWebDAVProbeStatus(resp *http.Response, webDAVURL *url.URL) bool {
	if resp.StatusCode == http.StatusNotFound {
		return true
	}
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Request == nil || resp.Request.URL == nil {
		return false
	}
	return isHostOrSubdomain(resp.Request.URL.Hostname(), "mypikpak.net") && isPikPakWebDAVHost(webDAVURL.Hostname())
}

func describeWebDAVGetFailure(resp *http.Response, webDAVURL *url.URL) string {
	if resp.Request == nil || resp.Request.URL == nil {
		return fmt.Sprintf("WebDAV GET failed: %s", resp.Status)
	}
	host := resp.Request.URL.Hostname()
	switch {
	case strings.EqualFold(host, webDAVURL.Hostname()):
		return fmt.Sprintf("WebDAV GET failed: %s (response from WebDAV server)", resp.Status)
	case isHostOrSubdomain(host, "mypikpak.net"):
		return fmt.Sprintf("WebDAV GET failed: %s (response from PikPak download CDN)", resp.Status)
	default:
		return fmt.Sprintf("WebDAV GET failed: %s (response from %s)", resp.Status, host)
	}
}

var _ service.BackupObjectStore = (*webDAVBackupStore)(nil)
