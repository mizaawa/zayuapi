//go:build embed

package web

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/gin-gonic/gin"
)

// allowUIHost is called only for the SPA document, never for API or asset paths.
func allowUIHost(c *gin.Context, policy uiAccessPolicy) bool {
	domain, err := uiaccess.NormalizeDomain(policy.Domain)
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
	body := uiNoticeBrand(policy) + uiNoticeIllustration("domain") + `<div class="eyebrow">官方站点访问</div><h1 id="notice-title">请通过官方合规域名进行站点访问</h1><p class="description">您当前访问的是 API 服务域名。请前往官方站点，继续使用完整的控制台与账户服务。</p><div class="destination"><span class="destination-label">官方访问地址</span><strong>` + html.EscapeString(domain) + `</strong><span class="verified-mark" aria-label="已配置的官方域名">` + uiNoticeCheckIcon + `</span></div><a class="primary-action" href="` + html.EscapeString(link) + `"><span>前往官方站点</span>` + uiNoticeArrowIcon + `</a><p class="footnote">此限制仅针对网页访问，API 调用不受影响。</p>`
	serveUINotice(c, http.StatusForbidden, body)
	return false
}

func serveUIUnavailable(c *gin.Context) {
	serveUINotice(c, http.StatusServiceUnavailable, uiNoticeBrand(uiAccessPolicy{})+uiNoticeIllustration("retry")+`<div class="eyebrow">访问状态确认</div><h1 id="notice-title">站点配置暂不可用</h1><p class="description">暂时无法读取站点的访问配置，请稍后刷新页面重试。</p>`)
}

func serveUINotice(c *gin.Context, status int, body string) {
	serveUINoticeWithPolicy(c, status, body, "default-src 'none'; img-src 'self' https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

// Match the product's light-only Monet palette in style.css. Keeping this
// document self-contained avoids downloading the application on API domains.
// All motion is CSS-only, so the notice does not need a script CSP exception.
const uiNoticeCSS = `:root{color-scheme:light;--background:#fffbe8;--surface:#fffef8;--container:#fff3bd;--outline:#d7c16a;--ink:#3f3420;--muted:#6b6045;--primary:#e9b824;--green:#477b54}
*{box-sizing:border-box}body{margin:0;min-height:100vh;min-height:100svh;display:grid;place-items:center;padding:40px 20px;background:var(--background);color:var(--ink);font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;-webkit-font-smoothing:antialiased;position:relative;isolation:isolate}.ambient{position:fixed;inset:0;overflow:hidden;pointer-events:none;z-index:-1}.ambient:before,.ambient:after{content:"";position:absolute;width:min(56vw,640px);aspect-ratio:1;border-radius:50%;filter:blur(65px);opacity:.48;animation:ambient-drift 14s ease-in-out infinite alternate}.ambient:before{top:-23%;right:-10%;background:#ffe78a}.ambient:after{left:-18%;bottom:-32%;background:#c7dfc0;animation-delay:-7s}.notice-card{width:100%;max-width:560px;padding:32px 40px 28px;border:1px solid rgba(215,193,106,.6);border-radius:28px;background:rgba(255,254,248,.95);box-shadow:0 20px 70px -32px rgba(101,70,11,.24),0 2px 8px rgba(63,52,32,.035);text-align:center;animation:card-enter .65s cubic-bezier(.16,1,.3,1) both}.brand{display:flex;align-items:center;justify-content:center;gap:12px;min-width:0}.brand-logo{display:grid;place-items:center;flex:none;width:46px;height:46px;padding:4px;border:1px solid #eee5c9;border-radius:14px;background:var(--surface);box-shadow:0 3px 10px rgba(63,52,32,.06)}.brand img{display:block;width:100%;height:100%;object-fit:contain;border-radius:10px}.brand-name{font-size:19px;line-height:1.4;font-weight:750;letter-spacing:-.02em;overflow-wrap:anywhere;text-align:left}.illustration{position:relative;display:grid;place-items:center;width:136px;height:124px;margin:32px auto 22px}.illustration:before{content:"";position:absolute;width:110px;height:110px;border:1px solid #e8d98f;border-radius:50%;background:linear-gradient(145deg,#fffbe8,#fff3bd)}.symbol-tile{position:relative;display:grid;place-items:center;width:78px;height:78px;border:1px solid rgba(255,255,255,.9);border-radius:24px;background:var(--surface);color:var(--green);box-shadow:0 8px 22px rgba(126,86,0,.11);transform:rotate(-5deg);animation:symbol-float 5s ease-in-out infinite}.symbol-tile svg{width:40px;height:40px;transform:rotate(5deg)}.illustration-dot{position:absolute;right:13px;top:9px;width:14px;height:14px;border:4px solid var(--surface);border-radius:50%;background:var(--primary);box-shadow:0 2px 8px rgba(126,86,0,.1)}.illustration-leaf{position:absolute;left:9px;bottom:12px;width:17px;height:9px;border-radius:14px 2px 14px 2px;background:#a5cc9f;transform:rotate(-32deg)}.eyebrow{color:var(--green);font-size:12px;font-weight:650;letter-spacing:.14em;margin-bottom:12px}h1{margin:0 auto;font-size:clamp(24px,4.8vw,29px);line-height:1.45;font-weight:750;letter-spacing:-.025em;max-width:440px;overflow-wrap:anywhere;text-wrap:balance}.description{margin:16px auto 24px;max-width:420px;font-size:14px;line-height:1.9;color:var(--muted);text-wrap:pretty}.destination{position:relative;display:flex;flex-direction:column;gap:5px;align-items:flex-start;text-align:left;padding:15px 48px 15px 18px;margin:0 0 16px;border:1px solid #eee5c9;border-radius:16px;background:#fffdf6}.destination-label{font-size:11px;color:var(--muted)}.destination strong{font-size:15px;line-height:1.6;font-weight:650;overflow-wrap:anywhere;max-width:100%}.verified-mark{position:absolute;top:50%;right:17px;transform:translateY(-50%);display:grid;place-items:center;width:24px;height:24px;border-radius:50%;background:#e4f0df;color:var(--green)}.verified-mark svg{width:14px;height:14px}form{margin:0}.primary-action{display:flex;align-items:center;justify-content:center;gap:10px;width:100%;min-height:50px;padding:13px 18px;border:1px solid #d8a71d;border-radius:16px;background:var(--primary);color:var(--ink);font-family:inherit;font-size:14px;font-weight:650;line-height:1.5;text-decoration:none;cursor:pointer;transition:background .18s ease,transform .18s ease,box-shadow .18s ease}.primary-action svg{width:18px;height:18px;flex:none;transition:transform .18s ease}.primary-action:hover{background:#f0c43d;transform:translateY(-1px);box-shadow:0 5px 14px rgba(151,104,0,.12)}a.primary-action:hover svg{transform:translateX(3px)}.primary-action:active{transform:translateY(0)}.primary-action:focus-visible{outline:3px solid #a5cc9f;outline-offset:4px}.footnote{margin:18px 0 0;font-size:11px;line-height:1.8;color:#716443}.notice-footer{position:relative;margin:25px 0 0;padding-top:18px;border-top:1px solid #eee5c9;color:#716443;font-size:11px;letter-spacing:.08em}.notice-footer span{display:inline-flex;align-items:center;gap:6px}.notice-footer span:before{content:"";width:5px;height:5px;background:#79b37b;border-radius:50%}@keyframes card-enter{from{opacity:0;transform:translateY(18px) scale(.985)}to{opacity:1;transform:translateY(0) scale(1)}}@keyframes symbol-float{0%,100%{transform:translateY(0) rotate(-5deg)}50%{transform:translateY(-7px) rotate(1deg)}}@keyframes ambient-drift{from{transform:translate(0,0)}to{transform:translate(35px,24px)}}@media(max-width:480px){body{padding:24px 16px}.notice-card{padding:28px 24px 24px;border-radius:24px}.illustration{margin-top:26px;margin-bottom:18px}.brand-name{font-size:18px}.description{font-size:14px}.footnote{font-size:11px}}@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation:none!important;transition:none!important}.primary-action:hover{transform:none}}`

const uiNoticeArrowIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14m-6-6 6 6-6 6"/></svg>`
const uiNoticeCheckIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 12 4 4 8-8"/></svg>`
const uiNoticeRetryIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 6.1a8 8 0 0 1 13.4 3.4M4.5 14.5a8 8 0 0 0 13.4 3.4"/></svg>`

func uiNoticeBrand(policy uiAccessPolicy) string {
	name := strings.TrimSpace(policy.SiteName)
	if name == "" {
		name = "Sub2API"
	}
	logo := safeImageURL(policy.SiteLogo)
	if logo == "" {
		logo = "/logo.svg"
	}
	return `<header class="brand"><span class="brand-logo"><img width="38" height="38" alt="站点图标" referrerpolicy="no-referrer" src="` + html.EscapeString(logo) + `"></span><span class="brand-name">` + html.EscapeString(name) + `</span></header>`
}

func uiNoticeIllustration(kind string) string {
	icon := `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.3 4 6.3 4 9s-1 5.7-4 9c-3-3.3-4-6.3-4-9s1-5.7 4-9Z"/></svg>`
	if kind == "country" {
		icon = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M19 10c0 5-7 11-7 11S5 15 5 10a7 7 0 1 1 14 0Z"/><circle cx="12" cy="10" r="2.5"/></svg>`
	} else if kind == "retry" {
		icon = uiNoticeRetryIcon
	}
	return `<div class="illustration" aria-hidden="true"><div class="symbol-tile">` + icon + `</div><span class="illustration-dot"></span><span class="illustration-leaf"></span></div>`
}

func serveUINoticeWithPolicy(c *gin.Context, status int, body, policy string) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Host")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", policy)
	c.Header("ETag", "")
	content := fmt.Sprintf(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><title>站点访问提示</title><style>%s</style></head><body><div class="ambient" aria-hidden="true"></div><main class="notice-card" aria-labelledby="notice-title">%s<footer class="notice-footer"><span>安全访问 · 连接更安心</span></footer></main></body></html>`, uiNoticeCSS, body)
	c.Data(status, "text/html; charset=utf-8", []byte(content))
	c.Abort()
}
