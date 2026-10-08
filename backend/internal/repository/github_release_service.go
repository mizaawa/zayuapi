package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type githubReleaseClient struct {
	httpClient         *http.Client
	downloadHTTPClient *http.Client
	tokenResolver      func(context.Context) (string, error)
}

type githubReleaseClientError struct {
	err error
}

const maxChecksumSize = 1024 * 1024

// NewGitHubReleaseClient 创建 GitHub Release 客户端
// proxyURL 为空时直连 GitHub，支持 http/https/socks5/socks5h 协议
// 代理配置失败时行为由 allowDirectOnProxyError 控制：
//   - false（默认）：返回错误占位客户端，禁止回退到直连
//   - true：回退到直连（仅限管理员显式开启）
func NewGitHubReleaseClient(proxyURL string, allowDirectOnProxyError bool, tokenResolver func(context.Context) (string, error)) service.GitHubReleaseClient {
	// 安全说明：httpclient.GetClient 的错误链（url.Parse / proxyutil）不含明文代理凭据，
	// 但仍通过 slog 仅在服务端日志记录，不会暴露给 HTTP 响应。
	sharedClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		sharedClient = &http.Client{Timeout: 30 * time.Second}
	}
	apiClient := cloneHTTPClient(sharedClient)
	apiClient.CheckRedirect = githubAPICheckRedirect(apiClient.CheckRedirect)

	// 下载客户端需要更长的超时时间
	downloadClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  10 * time.Minute,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy download client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		downloadClient = &http.Client{Timeout: 10 * time.Minute}
	}
	downloadClient = cloneHTTPClient(downloadClient)

	return &githubReleaseClient{
		httpClient:         apiClient,
		downloadHTTPClient: downloadClient,
		tokenResolver:      tokenResolver,
	}
}

func cloneHTTPClient(client *http.Client) *http.Client {
	cloned := *client
	return &cloned
}

func isGitHubAPIURL(url *url.URL) bool {
	return url != nil && strings.EqualFold(url.Scheme, "https") && url.User == nil &&
		strings.EqualFold(url.Host, "api.github.com")
}

func githubAPICheckRedirect(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		var err error
		if previous != nil {
			err = previous(req, via)
		} else if len(via) >= 10 {
			err = fmt.Errorf("stopped after 10 redirects")
		}
		if !isGitHubAPIURL(req.URL) {
			req.Header.Del("Authorization")
		}
		return err
	}
}

func githubAssetCheckRedirect(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	checkAuthorization := githubAPICheckRedirect(previous)
	return func(req *http.Request, via []*http.Request) error {
		if err := checkAuthorization(req, via); err != nil {
			return err
		}
		if req.URL.Scheme != "https" || req.URL.User != nil ||
			(req.URL.Host != "api.github.com" && req.URL.Host != "github.com" &&
				req.URL.Host != "objects.githubusercontent.com" && req.URL.Host != "release-assets.githubusercontent.com") {
			return fmt.Errorf("release asset redirected to an untrusted URL")
		}
		return nil
	}
}

func (c *githubReleaseClient) newAPIRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Sub2API-Updater")
	if isGitHubAPIURL(req.URL) {
		token, err := c.githubToken(ctx)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return req, nil
}

func (c *githubReleaseClient) githubToken(ctx context.Context) (string, error) {
	if c.tokenResolver == nil {
		return "", nil
	}
	token, err := c.tokenResolver(ctx)
	if err != nil {
		return "", fmt.Errorf("read GitHub access token: %w", err)
	}
	return strings.TrimSpace(token), nil
}

func (c *githubReleaseClient) newAssetRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	// Older cached releases contain only browser URLs, which cannot download private assets.
	if parsedURL.Scheme == "https" && parsedURL.Host == "github.com" && parsedURL.User == nil {
		parts := strings.Split(strings.TrimPrefix(parsedURL.EscapedPath(), "/"), "/")
		if len(parts) == 6 && parts[2] == "releases" && parts[3] == "download" {
			token, err := c.githubToken(ctx)
			if err != nil {
				return nil, err
			}
			if token == "" {
				req, err := c.newAPIRequest(ctx, rawURL)
				if err == nil {
					req.Header.Set("Accept", "application/octet-stream")
				}
				return req, err
			}
			filename, err := url.PathUnescape(parts[5])
			if err != nil {
				return nil, err
			}
			apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", parts[0], parts[1], parts[4])
			req, err := c.newAPIRequest(ctx, apiURL)
			if err != nil {
				return nil, err
			}
			resp, err := c.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("GitHub release asset lookup returned %d", resp.StatusCode)
			}
			var release service.GitHubRelease
			if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
				return nil, err
			}
			found := false
			for _, asset := range release.Assets {
				if asset.Name == filename && asset.APIURL != "" {
					assetURL, err := url.Parse(asset.APIURL)
					if err != nil || !isGitHubAPIURL(assetURL) {
						return nil, fmt.Errorf("invalid GitHub release asset API URL")
					}
					rawURL = asset.APIURL
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("release asset not found: %s", filename)
			}
		}
	}
	req, err := c.newAPIRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	return req, nil
}

func (c *githubReleaseClient) doAssetRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	client = cloneHTTPClient(client)
	client.CheckRedirect = githubAssetCheckRedirect(client.CheckRedirect)
	return client.Do(req)
}

func (c *githubReleaseClientError) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*service.GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	return c.err
}

func (c *githubReleaseClientError) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	return nil, c.err
}

func (c *githubReleaseClient) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release service.GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func (c *githubReleaseClient) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*service.GitHubRelease, error) {
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100 // GitHub API hard limit
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, perPage)

	req, err := c.newAPIRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []*service.GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	return releases, nil
}

func (c *githubReleaseClient) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	req, err := c.newAssetRequest(ctx, url)
	if err != nil {
		return err
	}

	// 使用预配置的下载客户端（已包含代理配置）
	resp, err := c.doAssetRequest(c.downloadHTTPClient, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// SECURITY: Check Content-Length if available
	if resp.ContentLength > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxSize)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}

	// SECURITY: Use LimitReader to enforce max download size even if Content-Length is missing/wrong
	limited := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(out, limited)

	// Close file before attempting to remove (required on Windows)
	_ = out.Close()

	if err != nil {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return err
	}

	// Check if we hit the limit (downloaded more than maxSize)
	if written > maxSize {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return fmt.Errorf("download exceeded maximum size of %d bytes", maxSize)
	}

	return nil
}

func (c *githubReleaseClient) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	req, err := c.newAssetRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := c.doAssetRequest(c.httpClient, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if resp.ContentLength > maxChecksumSize {
		return nil, fmt.Errorf("checksum file too large (max %d bytes)", maxChecksumSize)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxChecksumSize {
		return nil, fmt.Errorf("checksum file exceeded maximum size of %d bytes", maxChecksumSize)
	}
	return data, nil
}
