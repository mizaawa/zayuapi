package payment

import (
	"net/url"
	"strings"
)

// SanitizeURL accepts web links and the payment app schemes produced by our providers.
// It is also applied when reading legacy stored payment details.
func SanitizeURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Hostname() == "" {
			return ""
		}
	case "weixin", "alipays", "alipay", "wxp":
	default:
		return ""
	}
	return value
}
