package admin

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// A Sub2API deployment has a dashboard mount (/api/v1 underneath it) and
// a model gateway mount (/v1 and /v1beta underneath it). They need not match.
var providerMountPath = regexp.MustCompile("^(/[A-Za-z0-9._~-]+)*/?$")

func normalizeSub2APIMount(raw string, suffixes ...string) (string, error) {
	raw = strings.TrimSpace(raw)
	invalid := errors.New("use a public HTTPS base URL with a plain mount path and no credentials, query or fragment")
	// Check the original path before ValidateHTTPSURL decodes escapes and trims slashes.
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.User != nil || strings.ContainsAny(raw, "%?#\\") || !providerMountPath.MatchString(u.Path) {
		return "", invalid
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return "", invalid
		}
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !allowedProviderIP(ip) {
		return "", invalid
	}
	if _, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{}); err != nil {
		return "", invalid
	}
	u.Path = strings.TrimRight(u.Path, "/")
	for _, suffix := range suffixes {
		if strings.HasSuffix(u.Path, suffix) {
			u.Path = strings.TrimSuffix(u.Path, suffix)
			break
		}
	}
	return u.String(), nil
}

func normalizeDashboardURL(raw, siteType string) (string, error) {
	if siteType == "sub2api" {
		return normalizeSub2APIMount(raw, "/api/v1")
	}
	return normalizeProviderURL(raw) // New API accepts a site origin as before.
}

func normalizeGatewayURL(raw string) (string, error) {
	return normalizeSub2APIMount(raw, "/v1", "/v1beta")
}

// Only an explicit administrator setting may send model traffic to another
// origin or tenant. A URL advertised by public settings is not authorization.
func (r providerRecord) modelGateway() string {
	if r.SiteType == "sub2api" {
		if r.GatewayURL != "" {
			return r.GatewayURL
		}
		if r.AdvertisedGatewayURL != "" && strings.HasPrefix(r.AdvertisedGatewayURL+"/", r.BaseURL+"/") {
			return r.AdvertisedGatewayURL
		}
	}
	return r.BaseURL
}

// A successful read (including an empty advertised URL) replaces the cache.
// Transient failures leave it alone. Older sites without settings use their
// original dashboard mount. Never visit a URL from this response.
func sub2APIPublishedGateway(ctx context.Context, client *http.Client, panel string) *string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	anonymous := &http.Client{Transport: client.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	remote := providerRemote{base: panel, client: anonymous}
	data, err := remote.request(ctx, http.MethodGet, "/api/v1/settings/public", nil)
	gateway := ""
	if err != nil {
		var status *providerRemoteError
		if errors.As(err, &status) && (status.status == http.StatusNotFound || status.status == http.StatusMethodNotAllowed) {
			return &gateway
		}
		return nil
	}
	if raw, ok := data["api_base_url"].(string); ok && raw != "" {
		gateway, _ = normalizeGatewayURL(raw)
	}
	return &gateway
}
