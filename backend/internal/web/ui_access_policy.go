//go:build embed

package web

import (
	"context"
	"encoding/json"
	"html"
	"mime"
	"net/http"
	"strings"
	"time"

	clientip "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/gin-gonic/gin"
)

type uiAccessPolicy struct {
	DomainEnabled  *bool  `json:"ui_access_domain_enabled"`
	Domain         string `json:"ui_access_domain"`
	CountryEnabled bool   `json:"ui_country_restriction_enabled"`
	Blocked        string `json:"ui_blocked_countries"`
	SiteName       string `json:"site_name"`
	SiteLogo       string `json:"site_logo"`
}

func parseUIAccessPolicy(data []byte) (uiAccessPolicy, error) {
	var policy uiAccessPolicy
	err := json.Unmarshal(data, &policy)
	return policy, err
}

func (p uiAccessPolicy) domainEnabled() bool {
	if p.DomainEnabled != nil {
		return *p.DomainEnabled
	}
	// Existing installations enabled the restriction by entering a domain.
	return strings.TrimSpace(p.Domain) != ""
}

func (p uiAccessPolicy) restricted() bool {
	return p.domainEnabled() || p.CountryEnabled
}

// allowUIAccess runs only on document requests, before ETag handling. Denied
// visitors never receive the SPA payload, and no API request calls GeoJS.
func (s *FrontendServer) allowUIAccess(c *gin.Context, policy uiAccessPolicy) bool {
	if policy.domainEnabled() {
		if strings.TrimSpace(policy.Domain) == "" {
			serveUIUnavailable(c)
			return false
		}
		if !allowUIHost(c, policy.Domain) {
			return false
		}
	}
	retry := isUICountryRetry(c)
	if !policy.CountryEnabled {
		if retry {
			c.Header("Cache-Control", "private, no-store")
			c.Redirect(http.StatusSeeOther, safeUIReturnPath(c.Request))
			c.Abort()
			return false
		}
		return true
	}
	// With the domain switch on, only the official host reaches this point.
	// With it off, the country policy applies on every UI hostname.
	c.Header("Cache-Control", "private, no-store")
	c.Header("Vary", "Host")
	blocked, err := uiaccess.NormalizeCountryCodes(policy.Blocked)
	if err != nil || blocked == "" {
		serveUIUnavailable(c)
		return false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2500*time.Millisecond)
	defer cancel()
	resolver := s.countryResolver
	if resolver == nil {
		serveUICountryNotice(c, policy, true)
		return false
	}
	country, err := resolver.Country(ctx, clientip.GetTrustedClientIP(c), retry)
	if err != nil || !uiaccess.ValidCountryCode(country) {
		serveUICountryNotice(c, policy, true)
		return false
	}
	for _, denied := range strings.Split(blocked, ",") {
		if denied == country {
			serveUICountryNotice(c, policy, false)
			return false
		}
	}
	if retry {
		// The notice lives at the original URL. A successful POST retry returns
		// there as a GET, preserving its query without accepting a return URL.
		c.Redirect(http.StatusSeeOther, safeUIReturnPath(c.Request))
		c.Abort()
		return false
	}
	return true
}

func isUICountryRetry(c *gin.Context) bool {
	if c.Request.Method != http.MethodPost {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	return c.Request.ParseForm() == nil && c.Request.PostForm.Get("_ui_country_retry") == "1"
}

func safeUIReturnPath(request *http.Request) string {
	path := request.URL.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") {
		return "/"
	}
	uri := request.URL.RequestURI()
	if strings.ContainsAny(uri, "\r\n") {
		return "/"
	}
	return uri
}

func serveUICountryNotice(c *gin.Context, policy uiAccessPolicy, unavailable bool) {
	name := strings.TrimSpace(policy.SiteName)
	if name == "" {
		name = "Sub2API"
	}
	logo := safeImageURL(policy.SiteLogo)
	if logo == "" {
		logo = "/logo.svg"
	}
	title := "该地区不在服务范围内"
	description := "基于不同国家和地区的政策要求及本站运营策略，当前所在地区暂不提供站点访问服务。感谢您的理解与支持。如您的网络环境已发生变化，请点击重试，我们将重新检测当前访问地区。"
	status := http.StatusForbidden
	if unavailable {
		title = "暂时无法确认访问地区"
		description = "暂时无法完成访问地区检测，请稍后重试。若持续出现此提示，请联系站点管理员检查地区检测服务及可信代理配置。"
		status = http.StatusServiceUnavailable
		c.Header("Retry-After", "5")
	}
	body := `<div class="brand"><img width="44" height="44" alt="站点图标" referrerpolicy="no-referrer" src="` + html.EscapeString(logo) + `"><span>` + html.EscapeString(name) + `</span></div><div class="notice-symbol" aria-hidden="true">◎</div><h1>` + title + `</h1><p>` + description + `</p><form method="post" action=""><input type="hidden" name="_ui_country_retry" value="1"><button type="submit">重试</button></form><small>重试将重新检测；允许访问后会返回您刚才打开的页面。</small>`
	// This standalone document needs only its branding image and same-origin
	// retry form. It loads no JavaScript, CSS bundle or external font.
	serveUINoticeWithPolicy(c, status, body, "default-src 'none'; img-src 'self' https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
}
