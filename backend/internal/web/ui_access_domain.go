//go:build embed

package web

import (
	"fmt"
	"html"
	"net/http"
	"net/url"

	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/gin-gonic/gin"
)

// allowUIHost is called only for the SPA document, never for API or asset paths.
func allowUIHost(c *gin.Context, configured string) bool {
	domain, err := uiaccess.NormalizeDomain(configured)
	if err != nil {
		serveUIUnavailable(c)
		return false
	}
	if domain == "" {
		return true
	}
	// The actual authority is checked before ETag handling. Forwarded host
	// headers cannot grant UI access on an API/CDN hostname.
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Host")
	if uiaccess.RequestHostMatches(c.Request.Host, domain) {
		return true
	}
	// A scheme-relative root URL preserves HTTP/HTTPS in the browser, including
	// TLS terminated at a proxy, without copying any incoming path/query/token.
	link := (&url.URL{Host: domain, Path: "/"}).String()
	body := `<h1>请通过官方合规域名进行站点访问</h1><p>当前域名仅用于 API 服务，请使用官方域名访问站点。</p><a href="` + html.EscapeString(link) + `">前往官方站点：` + html.EscapeString(domain) + `</a>`
	serveUINotice(c, http.StatusForbidden, body)
	return false
}

func serveUIUnavailable(c *gin.Context) {
	serveUINotice(c, http.StatusServiceUnavailable, `<h1>站点配置暂不可用</h1><p>请稍后重试。</p>`)
}

func serveUINotice(c *gin.Context, status int, body string) {
	serveUINoticeWithPolicy(c, status, body, "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

func serveUINoticeWithPolicy(c *gin.Context, status int, body, policy string) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Host")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", policy)
	c.Header("ETag", "")
	content := fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>站点访问提示</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f6f7fb;color:#182230;font-family:system-ui,sans-serif}main{max-width:40rem;margin:2rem;padding:2rem;background:#fff;border:1px solid #e4e7ec;border-radius:1rem}h1{font-size:1.4rem;line-height:1.5}p{line-height:1.7;color:#475467}a,button{display:inline-block;padding:.8rem 1rem;border:0;border-radius:.5rem;background:#175cd3;color:#fff;font:inherit;text-decoration:none;overflow-wrap:anywhere;cursor:pointer}button:hover,a:hover{background:#1849a9}button:focus-visible,a:focus-visible{outline:3px solid #84adff;outline-offset:3px}.brand{display:flex;align-items:center;gap:.7rem;font-weight:650}.brand img{object-fit:contain;border-radius:.5rem}.notice-symbol{font-size:2.8rem;color:#175cd3;margin-top:1.6rem}small{display:block;margin-top:1rem;color:#667085;line-height:1.6}@media(prefers-color-scheme:dark){body{background:#101828;color:#f2f4f7}main{background:#1d2939;border-color:#344054}p,small{color:#d0d5dd}}</style></head><body><main>%s</main></body></html>`, body)
	c.Data(status, "text/html; charset=utf-8", []byte(content))
	c.Abort()
}
