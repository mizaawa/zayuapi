package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

var errProviderAuthentication = errors.New("provider authentication failed")
var errProviderResponse = errors.New("provider returned an unsupported response")

func normalizeProviderURL(raw string) (string, error) {
	base, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{})
	if err != nil {
		return "", errors.New("provider must use a public HTTPS URL")
	}
	u, err := url.Parse(base)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Hostname() == "" {
		return "", errors.New("provider URL must be an HTTPS site origin")
	}
	return strings.TrimRight(base, "/"), nil
}

// A request is bound to a validated public IP at dial time, closing the usual
// resolve-then-connect DNS rebinding gap. Redirects and environment proxies are
// disabled so credentials cannot be forwarded to another origin.
func providerHTTPClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConnsPerHost:   2,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(addresses) == 0 {
				return nil, errors.New("provider DNS lookup failed")
			}
			for _, address := range addresses {
				ip, ok := netip.AddrFromSlice(address.IP)
				if !ok || !allowedProviderIP(ip.Unmap()) {
					return nil, errors.New("provider resolves to a non-public address")
				}
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:       15 * time.Second,
		Jar:           jar,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func allowedProviderIP(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "2001:db8::/32", "fc00::/7"} {
		prefix := netip.MustParsePrefix(block)
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

type providerRemote struct {
	client *http.Client
	base   string
	token  string
}

func (r *providerRemote) request(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, reader)
	if err != nil {
		return nil, errProviderResponse
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, errProviderAuthentication
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", res.StatusCode)
	}
	var envelope map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope); err != nil {
		return nil, errProviderResponse
	}
	if success, ok := envelope["success"].(bool); ok && !success {
		return nil, errProviderAuthentication
	}
	if code, ok := number(envelope["code"]); ok && code != 0 {
		return nil, errProviderAuthentication
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return nil, errProviderResponse
	}
	return data, nil
}

func number(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func field(data map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		if value, ok := number(data[key]); ok && value >= 0 && value < 1e12 {
			return &value
		}
	}
	return nil
}

func divide(value *float64, factor float64) *float64 {
	if value == nil || factor <= 0 {
		return nil
	}
	converted := *value / factor
	return &converted
}

func probeProvider(ctx context.Context, provider providerRecord, secret string) (providerMetrics, error) {
	client, err := providerHTTPClient()
	if err != nil {
		return providerMetrics{}, err
	}
	return probeProviderWithClient(ctx, provider, secret, client)
}

func probeProviderWithClient(ctx context.Context, provider providerRecord, secret string, client *http.Client) (providerMetrics, error) {
	r := providerRemote{client: client, base: provider.BaseURL}
	switch provider.SiteType {
	case "sub2api":
		if provider.AuthMode == "password" {
			login, err := r.request(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": provider.Account, "password": secret})
			if err != nil {
				return providerMetrics{}, err
			}
			r.token, _ = login["access_token"].(string)
			if r.token == "" {
				return providerMetrics{}, errProviderResponse
			}
		} else {
			// Accept an existing dashboard access token. A model API key will
			// fail the authenticated profile check below and is never saved.
			r.token = secret
		}
		profile, err := r.request(ctx, http.MethodGet, "/api/v1/user/profile", nil)
		if err != nil {
			return providerMetrics{}, err
		}
		stats, err := r.request(ctx, http.MethodGet, "/api/v1/usage/dashboard/stats", nil)
		if err != nil {
			return providerMetrics{}, err
		}
		return providerMetrics{
			Balance:       field(profile, "balance"),
			TodaySpend:    field(stats, "today_actual_cost", "today_cost"),
			TotalRecharge: field(profile, "total_recharged"),
			TotalSpend:    field(stats, "total_actual_cost", "total_cost"),
			Currency:      "USD",
		}, nil
	case "newapi":
		if provider.AuthMode == "password" {
			_, err := r.request(ctx, http.MethodPost, "/api/user/login", map[string]string{"username": provider.Account, "password": secret})
			if err != nil {
				return providerMetrics{}, err
			}
		} else {
			r.token = secret
		}
		profile, err := r.request(ctx, http.MethodGet, "/api/user/self", nil)
		if err != nil {
			return providerMetrics{}, err
		}
		status, err := r.request(ctx, http.MethodGet, "/api/status", nil)
		if err != nil {
			return providerMetrics{}, err
		}
		quotaPerUnit, ok := number(status["quota_per_unit"])
		if !ok || quotaPerUnit <= 0 {
			return providerMetrics{}, errors.New("New API quota conversion is unavailable")
		}
		metrics := providerMetrics{
			Balance:    divide(field(profile, "quota"), quotaPerUnit),
			TotalSpend: divide(field(profile, "used_quota"), quotaPerUnit),
			Currency:   "USD",
		}
		location, _ := time.LoadLocation("Asia/Shanghai")
		now := time.Now().In(location)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Unix()
		path := fmt.Sprintf("/api/log/self/stat?type=2&start_timestamp=%d&end_timestamp=%d", start, now.Add(time.Second).Unix())
		if daily, err := r.request(ctx, http.MethodGet, path, nil); err == nil {
			metrics.TodaySpend = divide(field(daily, "quota"), quotaPerUnit)
		}
		// New API self/profile has no canonical cumulative recharge field.
		// Leave it unknown instead of presenting quota + spend as payments.
		return metrics, nil
	default:
		return providerMetrics{}, errors.New("unsupported provider type")
	}
}
