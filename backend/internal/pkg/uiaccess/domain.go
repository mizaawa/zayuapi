// Package uiaccess validates the optional hostname used for browser UI access.
package uiaccess

import (
	"errors"
	"net"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

var ErrInvalidDomain = errors.New("UI access domain must be a bare DNS domain without a scheme, port, path or wildcard")

// NormalizeDomain returns the canonical ASCII DNS name. An empty value disables
// the restriction. IP literals and single-label hosts are deliberately excluded.
func NormalizeDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "/\\:@?#*") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", ErrInvalidDomain
	}
	domain, err := idna.Lookup.ToASCII(value)
	if err != nil {
		return "", ErrInvalidDomain
	}
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return "", ErrInvalidDomain
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidDomain
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return "", ErrInvalidDomain
			}
		}
	}
	// Browsers interpret numeric (including hexadecimal) final labels as IPv4
	// candidates. They cannot be the public DNS suffix of the official UI host.
	top := labels[len(labels)-1]
	if top[0] >= '0' && top[0] <= '9' {
		return "", ErrInvalidDomain
	}
	return domain, nil
}

// RequestHostMatches uses the HTTP authority supplied to the application. It
// never consults forwarding headers, which arbitrary clients can also send.
func RequestHostMatches(authority, domain string) bool {
	host := authority
	if strings.Contains(authority, ":") {
		var err error
		host, _, err = net.SplitHostPort(authority)
		if err != nil {
			return false
		}
	}
	if host != strings.TrimSpace(host) {
		return false
	}
	normalized, err := NormalizeDomain(host)
	return err == nil && normalized != "" && normalized == domain
}
