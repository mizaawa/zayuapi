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
	body := uiNoticeHeader(policy, "网页访问受限", "") + `<div class="notice-body">` + uiNoticeScene("domain") +
		`<div class="eyebrow">官方站点访问</div><h1 id="notice-title">请通过官方合规域名进行站点访问</h1><p class="description">您当前访问的是 API 服务域名。请前往官方站点，继续使用完整的控制台与账户服务。</p>` +
		`<div class="destination"><span class="destination-label">官方访问地址</span><span class="address">` + uiNoticeLockIcon + `<strong>` + html.EscapeString(domain) + `</strong><span class="verified-mark" aria-label="已配置的官方域名">` + uiNoticeCheckIcon + `</span></span></div>` +
		`<a class="primary-action" href="` + html.EscapeString(link) + `"><span>前往官方站点</span>` + uiNoticeArrowIcon + `</a>` +
		`<ul class="facts"><li>仅限制网页访问</li><li>API 调用不受影响</li></ul></div>`
	serveUINotice(c, http.StatusForbidden, body)
	return false
}

func serveUIUnavailable(c *gin.Context) {
	// An empty href reloads the same document, so a retry never leaves the page the visitor asked for.
	serveUINotice(c, http.StatusServiceUnavailable, uiNoticeHeader(uiAccessPolicy{}, "配置暂不可用", "is-quiet")+`<div class="notice-body">`+uiNoticeScene("retry")+
		`<div class="eyebrow">访问状态确认</div><h1 id="notice-title">站点配置暂不可用</h1><p class="description">暂时无法读取站点的访问配置，请稍后刷新页面重试。</p>`+
		`<a class="primary-action" href="">`+uiNoticeRetryIcon+`<span>刷新页面</span></a></div>`)
}

func serveUINotice(c *gin.Context, status int, body string) {
	serveUINoticeWithPolicy(c, status, body, "default-src 'none'; img-src 'self' https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

// Match the product's light-only Monet palette in style.css. Keeping this
// document self-contained avoids downloading the application on API domains.
// All motion is CSS-only, so the notice does not need a script CSP exception,
// and no url() is used, so nothing beyond the brand image is fetched.
const uiNoticeCSS = `:root{color-scheme:light;--background:#fffbe8;--surface:#fffef8;--container:#fff3bd;--line:#eee5c9;--ink:#3f3420;--muted:#6b6045;--primary:#e9b824;--green:#477b54;--alert:#b45309}
*{box-sizing:border-box}body{margin:0;min-height:100vh;min-height:100svh;display:grid;place-items:center;padding:40px 20px;background:radial-gradient(1100px 520px at 50% -14%,#fff0b0,transparent 70%),var(--background);color:var(--ink);font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",sans-serif;-webkit-font-smoothing:antialiased;position:relative;isolation:isolate}body:before{content:"";position:fixed;inset:0;z-index:-1;background-image:radial-gradient(rgba(126,86,0,.14) 1px,transparent 1.5px);background-size:22px 22px;-webkit-mask-image:radial-gradient(ellipse 70% 60% at 50% 38%,#000,transparent);mask-image:radial-gradient(ellipse 70% 60% at 50% 38%,#000,transparent)}.ambient{position:fixed;inset:0;overflow:hidden;pointer-events:none;z-index:-2}.ambient:before,.ambient:after{content:"";position:absolute;width:min(56vw,620px);aspect-ratio:1;border-radius:50%;filter:blur(70px);opacity:.5;animation:ambient-drift 14s ease-in-out infinite alternate}.ambient:before{top:-24%;right:-12%;background:#ffe78a}.ambient:after{left:-18%;bottom:-32%;background:#c7dfc0;animation-delay:-7s}
.notice-card{width:100%;max-width:520px;overflow:hidden;border:1px solid rgba(215,193,106,.55);border-radius:28px;background:rgba(255,254,248,.96);box-shadow:0 30px 80px -40px rgba(101,70,11,.38),0 2px 10px rgba(63,52,32,.05);animation:card-enter .6s cubic-bezier(.16,1,.3,1) both}.notice-top{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 18px;border-bottom:1px solid var(--line);background:linear-gradient(120deg,#fff3bd,#fffbe8 72%)}.brand{display:flex;align-items:center;gap:10px;min-width:0}.brand-logo{display:grid;place-items:center;flex:none;width:38px;height:38px;padding:3px;border-radius:12px;background:var(--surface);box-shadow:0 2px 8px rgba(63,52,32,.09)}.brand img{display:block;width:100%;height:100%;object-fit:contain;border-radius:9px}.brand-name{font-size:15px;line-height:1.35;font-weight:750;letter-spacing:-.01em;overflow-wrap:anywhere;text-align:left}.status-chip{display:inline-flex;align-items:center;gap:7px;flex:none;padding:5px 11px;border-radius:999px;background:var(--surface);color:var(--alert);font-size:12px;font-weight:650;box-shadow:inset 0 0 0 1px rgba(215,193,106,.55)}.status-chip i{width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 0 3px rgba(180,83,9,.16);animation:blink 2.4s ease-in-out infinite}.status-chip.is-quiet{color:var(--muted)}.status-chip.is-quiet i{box-shadow:0 0 0 3px rgba(107,96,69,.14)}
.notice-body{padding:30px 36px 28px;text-align:center}.scene{position:relative;display:grid;place-items:center;width:136px;height:136px;margin:0 auto 22px}.scene:before{content:"";position:absolute;inset:16px;border-radius:50%;background:radial-gradient(circle at 34% 28%,#fffef8,#fff3bd 58%,#ffe78a);box-shadow:inset 0 0 0 1px rgba(215,193,106,.55)}.orbit{position:absolute;inset:0;border:1px dashed rgba(201,154,18,.5);border-radius:50%;animation:spin 18s linear infinite}.orbit i{position:absolute;top:12px;left:17px;width:13px;height:13px;border:3px solid var(--surface);border-radius:50%;background:var(--primary);box-shadow:0 2px 6px rgba(126,86,0,.22)}.pulse{position:absolute;inset:34px;border:2px solid rgba(194,65,12,.4);border-radius:50%;animation:pulse 2.6s ease-out infinite}.pulse+.pulse{animation-delay:1.3s}.tile{position:relative;display:grid;place-items:center;width:72px;height:72px;border-radius:22px;background:var(--surface);color:var(--green);box-shadow:0 12px 26px -10px rgba(126,86,0,.38),inset 0 1px 0 #fff;animation:symbol-float 5s ease-in-out infinite}.tile svg{width:36px;height:36px}.is-country .tile{color:#c2410c}.is-retry .tile{color:var(--muted)}.is-retry .tile svg{animation:spin 2.6s linear infinite}
.eyebrow{display:inline-block;margin-bottom:12px;padding:3px 11px;border-radius:999px;background:#e4f0df;color:var(--green);font-size:12px;font-weight:650;letter-spacing:.14em}h1{margin:0 auto;max-width:430px;font-size:clamp(22px,4.6vw,27px);line-height:1.42;font-weight:750;letter-spacing:-.02em;overflow-wrap:anywhere;text-wrap:balance}.description{margin:14px auto 22px;max-width:410px;font-size:14px;line-height:1.85;color:var(--muted);text-wrap:pretty}.destination{margin:0 0 14px;padding:12px 12px 13px;border-radius:18px;background:#fffbe8;box-shadow:inset 0 0 0 1px var(--line);text-align:left}.destination-label{display:block;margin:0 0 8px 4px;font-size:11px;font-weight:600;color:var(--muted)}.address{display:flex;align-items:center;gap:10px;padding:10px 10px 10px 14px;border-radius:13px;background:var(--surface);box-shadow:0 1px 2px rgba(63,52,32,.06),inset 0 0 0 1px rgba(215,193,106,.5)}.address>svg{flex:none;width:16px;height:16px;color:var(--green)}.address strong{flex:1;min-width:0;font:600 15px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;overflow-wrap:anywhere}.verified-mark{display:grid;place-items:center;flex:none;width:24px;height:24px;border-radius:50%;background:#e4f0df;color:var(--green)}.verified-mark svg{width:14px;height:14px}.detected{display:inline-flex;align-items:center;gap:10px;margin:0 0 18px;padding:5px 5px 5px 14px;border-radius:999px;background:#fffbe8;box-shadow:inset 0 0 0 1px var(--line);font-size:12px;color:var(--muted)}.detected strong{padding:3px 11px;border-radius:999px;background:var(--surface);color:var(--ink);font:700 13px/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;letter-spacing:.1em;box-shadow:0 1px 3px rgba(63,52,32,.1)}
form{margin:0}.primary-action{display:flex;align-items:center;justify-content:center;gap:10px;width:100%;min-height:50px;padding:13px 18px;border:1px solid #d8a71d;border-radius:16px;background:linear-gradient(180deg,#f0c43d,var(--primary));color:var(--ink);font-family:inherit;font-size:14px;font-weight:650;line-height:1.5;text-decoration:none;cursor:pointer;box-shadow:inset 0 1px 0 rgba(255,255,255,.45),0 8px 18px -10px rgba(151,104,0,.55);transition:transform .18s ease,box-shadow .18s ease}.primary-action svg{width:18px;height:18px;flex:none;transition:transform .18s ease}.primary-action:hover{transform:translateY(-1px);box-shadow:inset 0 1px 0 rgba(255,255,255,.5),0 12px 22px -10px rgba(151,104,0,.6)}a.primary-action:hover svg{transform:translateX(3px)}.primary-action:active{transform:translateY(0)}.primary-action:focus-visible{outline:3px solid #a5cc9f;outline-offset:4px}.facts{display:flex;flex-wrap:wrap;justify-content:center;gap:8px;margin:16px 0 0;padding:0;list-style:none}.facts li{display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border-radius:999px;background:#faf6e8;color:var(--muted);font-size:12px}.facts li:before,.notice-footer span:before{content:"";width:5px;height:5px;border-radius:50%;background:#79b37b}.footnote{margin:16px 0 0;font-size:12px;line-height:1.8;color:#716443}.notice-footer{display:flex;justify-content:center;padding:14px 18px;border-top:1px solid var(--line);background:#fffdf6;color:#716443;font-size:11px;letter-spacing:.08em}.notice-footer span{display:inline-flex;align-items:center;gap:6px}
@keyframes card-enter{from{opacity:0;transform:translateY(18px) scale(.985)}}@keyframes symbol-float{0%,100%{transform:translateY(0)}50%{transform:translateY(-6px)}}@keyframes spin{to{transform:rotate(360deg)}}@keyframes pulse{from{transform:scale(.72);opacity:.9}to{transform:scale(1.4);opacity:0}}@keyframes blink{50%{opacity:.45}}@keyframes ambient-drift{to{transform:translate(35px,24px)}}@media(max-width:480px){body{padding:18px 12px}.notice-top{padding:12px 14px}.brand-name{font-size:14px}.status-chip{padding:4px 9px;font-size:11px}.notice-body{padding:24px 20px 22px}.scene{width:120px;height:120px;margin-bottom:18px}}@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation:none!important;transition:none!important}.primary-action:hover{transform:none}}`

const uiNoticeArrowIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14m-6-6 6 6-6 6"/></svg>`
const uiNoticeCheckIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 12 4 4 8-8"/></svg>`
const uiNoticeRetryIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 6.1a8 8 0 0 1 13.4 3.4M4.5 14.5a8 8 0 0 0 13.4 3.4"/></svg>`
const uiNoticeLockIcon = `<svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="11" width="14" height="9" rx="2.5"/><path d="M8.5 11V8a3.5 3.5 0 0 1 7 0v3"/></svg>`

// uiNoticeHeader renders the brand strip with a short, fixed status label.
// Only the site name and logo are dynamic, and both are escaped.
func uiNoticeHeader(policy uiAccessPolicy, status, tone string) string {
	name := strings.TrimSpace(policy.SiteName)
	if name == "" {
		name = "Sub2API"
	}
	logo := safeImageURL(policy.SiteLogo)
	if logo == "" {
		logo = "/logo.svg"
	}
	chipClass := "status-chip"
	if tone != "" {
		chipClass += " " + tone
	}
	return `<header class="notice-top"><div class="brand"><span class="brand-logo"><img width="32" height="32" alt="站点图标" referrerpolicy="no-referrer" src="` + html.EscapeString(logo) + `"></span><span class="brand-name">` + html.EscapeString(name) + `</span></div><span class="` + chipClass + `"><i aria-hidden="true"></i>` + status + `</span></header>`
}

// uiNoticeScene draws the orbiting illustration: a globe for the domain
// notice, a pin with pulse rings for a blocked region and a turning arrow
// while detection or configuration is unavailable.
func uiNoticeScene(kind string) string {
	icon := `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.3 4 6.3 4 9s-1 5.7-4 9c-3-3.3-4-6.3-4-9s1-5.7 4-9Z"/></svg>`
	extra := ""
	switch kind {
	case "country":
		icon = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M19 10c0 5-7 11-7 11S5 15 5 10a7 7 0 1 1 14 0Z"/><circle cx="12" cy="10" r="2.5"/></svg>`
		extra = `<span class="pulse"></span><span class="pulse"></span>`
	case "retry":
		icon = uiNoticeRetryIcon
	}
	return `<div class="scene is-` + kind + `" aria-hidden="true"><span class="orbit"><i></i></span>` + extra + `<span class="tile">` + icon + `</span></div>`
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
