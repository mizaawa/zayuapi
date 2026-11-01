//go:build embed

package web

import (
	"crypto/rand"
	"net/http"
	"strings"
)

// uiAccessNoCacheCookie stores nothing: Max-Age=0 makes the browser drop it at
// once. Its presence is what keeps CDNs from storing the response even when
// they cache status codes regardless of Cache-Control; Alibaba Cloud CDN, for
// example, never caches a 302, 307 or 403 that sets a cookie.
const uiAccessNoCacheCookie = "ui_access_nocache=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"

// uiAccessCacheBuster names the one-time query parameter added to a successful
// retry's redirect (and to the SPA's navigation probe). A cache that stored the
// notice at the original URL despite the headers below cannot answer the new
// URL, so the visitor is not sent straight back to that notice. The SPA drops
// the parameter from the address bar before routing.
const uiAccessCacheBuster = "_ui_access"

// preventUIAccessCaching marks a response whose content depends on the
// visitor's host or region. Shared caches key on the URL, so a stored region
// notice would be replayed to every visitor of that page, and a stored page to
// visitors who must be refused. Each header is the opt-out that one family of
// caches honors; together they also cover CDNs configured to follow the origin.
func preventUIAccessCaching(header http.Header) {
	header.Set("Cache-Control", "private, no-store, no-cache, max-age=0, must-revalidate")
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "0")
	header.Set("CDN-Cache-Control", "no-store")            // RFC 9213 targeted cache control
	header.Set("Cloudflare-CDN-Cache-Control", "no-store") // takes precedence at Cloudflare
	header.Set("Surrogate-Control", "no-store")            // Fastly, Varnish, Akamai
	header.Set("X-Accel-Expires", "0")                     // nginx proxy_cache
	header.Set("Vary", "*")
	for _, cookie := range header.Values("Set-Cookie") {
		if cookie == uiAccessNoCacheCookie {
			return
		}
	}
	header.Add("Set-Cookie", uiAccessNoCacheCookie)
}

// uiRetryReturnPath is the original document URL with a fresh cache buster;
// a buster left over from an earlier retry is replaced, other parameters are
// kept byte for byte.
func uiRetryReturnPath(request *http.Request) string {
	path, query, _ := strings.Cut(safeUIReturnPath(request), "?")
	var kept []string
	for _, part := range strings.Split(query, "&") {
		if name, _, _ := strings.Cut(part, "="); part != "" && name != uiAccessCacheBuster {
			kept = append(kept, part)
		}
	}
	kept = append(kept, uiAccessCacheBuster+"="+strings.ToLower(rand.Text()))
	return path + "?" + strings.Join(kept, "&")
}
