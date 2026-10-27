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
var errProviderTurnstile = errors.New("provider requires Turnstile verification; use an existing dashboard Access Token or complete verification on the provider site")
var errProviderTwoFactor = errors.New("provider requires two-factor verification; use an existing dashboard Access Token")

type providerToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
}

func validProviderTokenType(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		letter := char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
		if index == 0 && !letter {
			return false
		}
		if !letter && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func encodeProviderToken(accessToken, refreshToken, tokenType string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	refreshToken = strings.TrimSpace(refreshToken)
	tokenType = strings.TrimSpace(tokenType)
	if accessToken == "" && refreshToken == "" {
		return "", errors.New("dashboard Access Token or Refresh Token is required")
	}
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if !validProviderTokenType(tokenType) {
		return "", errors.New("invalid dashboard Token Type")
	}
	if len(accessToken) > 8192 || len(refreshToken) > 8192 {
		return "", errors.New("login credential is too long")
	}
	if !validProviderCredential(accessToken) || !validProviderCredential(refreshToken) {
		return "", errors.New("login credential contains invalid characters")
	}
	data, err := json.Marshal(providerToken{AccessToken: accessToken, RefreshToken: refreshToken, TokenType: tokenType})
	return string(data), err
}

func decodeProviderToken(secret string) (providerToken, error) {
	if strings.HasPrefix(secret, "{") {
		var token providerToken
		if err := json.Unmarshal([]byte(secret), &token); err != nil || (token.AccessToken == "" && token.RefreshToken == "") {
			return providerToken{}, errors.New("stored dashboard token is invalid")
		}
		if !validProviderTokenType(token.TokenType) {
			return providerToken{}, errors.New("stored dashboard token type is unsupported")
		}
		return token, nil
	}
	// Existing manually entered dashboard tokens from the earlier version.
	if !validProviderCredential(secret) {
		return providerToken{}, errors.New("stored dashboard token is invalid")
	}
	return providerToken{AccessToken: secret, TokenType: "Bearer"}, nil
}

func providerPublicError(err error) string {
	switch {
	case errors.Is(err, errProviderTurnstile):
		return errProviderTurnstile.Error()
	case errors.Is(err, errProviderTwoFactor):
		return errProviderTwoFactor.Error()
	case errors.Is(err, errProviderAuthentication):
		return "Provider login failed: invalid or expired dashboard credential"
	case errors.Is(err, errProviderResponse):
		return "Provider returned an unsupported response; check the provider type and site address"
	default:
		var remote *providerRemoteError
		if errors.As(err, &remote) {
			return fmt.Sprintf("Provider %s returned HTTP %d", remote.operation, remote.status)
		}
		return "Could not connect to the provider; check its HTTPS address and availability"
	}
}

type providerRemoteError struct {
	operation string
	status    int
}

func (e *providerRemoteError) Error() string {
	return fmt.Sprintf("provider %s returned HTTP %d", e.operation, e.status)
}

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

// providerBlockedPrefixes covers special-purpose ranges that IsGlobalUnicast
// accepts. Translation prefixes (NAT64, 6to4, Teredo) are blocked because they
// can embed a private IPv4 address that a gateway would then reach.
var providerBlockedPrefixes = func() []netip.Prefix {
	blocks := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:10::/28",
		"2001:20::/28", "2001:db8::/32", "2002::/16", "fc00::/7",
	}
	prefixes := make([]netip.Prefix, 0, len(blocks))
	for _, block := range blocks {
		prefixes = append(prefixes, netip.MustParsePrefix(block))
	}
	return prefixes
}()

func allowedProviderIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range providerBlockedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// validProviderCredential rejects values that would be unsafe or invalid
// inside an HTTP header, such as CR/LF or other control characters.
func validProviderCredential(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

type providerRemote struct {
	client    *http.Client
	base      string
	token     string
	tokenType string
	userID    string
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
	if r.token != "" && path != "/api/v1/auth/refresh" {
		tokenType := r.tokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}
		req.Header.Set("Authorization", tokenType+" "+r.token)
	}
	if r.userID != "" {
		req.Header.Set("New-Api-User", r.userID)
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	var envelope map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope); err != nil {
		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			return nil, errProviderAuthentication
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &providerRemoteError{operation: path, status: res.StatusCode}
		}
		return nil, errProviderResponse
	}
	message, _ := envelope["message"].(string)
	if envelope["reason"] == "TURNSTILE_VERIFICATION_FAILED" || envelope["code"] == "TURNSTILE_VERIFICATION_FAILED" || strings.Contains(strings.ToLower(message), "turnstile") {
		return nil, errProviderTurnstile
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, errProviderAuthentication
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &providerRemoteError{operation: path, status: res.StatusCode}
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

func probeProvider(ctx context.Context, provider providerRecord, secret string) (providerMetrics, string, error) {
	client, err := providerHTTPClient()
	if err != nil {
		return providerMetrics{}, "", err
	}
	return probeProviderWithClientAndSecret(ctx, provider, secret, client)
}

func probeProviderWithClient(ctx context.Context, provider providerRecord, secret string, client *http.Client) (providerMetrics, error) {
	metrics, _, err := probeProviderWithClientAndSecret(ctx, provider, secret, client)
	return metrics, err
}

func probeProviderWithClientAndSecret(ctx context.Context, provider providerRecord, secret string, client *http.Client) (providerMetrics, string, error) {
	r := providerRemote{client: client, base: provider.BaseURL}
	switch provider.SiteType {
	case "sub2api":
		var credential providerToken
		var rotatedSecret string
		if provider.AuthMode == "password" {
			login, err := r.request(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": provider.Account, "password": secret})
			if err != nil {
				return providerMetrics{}, "", err
			}
			r.token, _ = login["access_token"].(string)
			if r.token == "" {
				if required, _ := login["requires_2fa"].(bool); required {
					return providerMetrics{}, "", errProviderTwoFactor
				}
				return providerMetrics{}, "", errProviderResponse
			}
		} else {
			token, err := decodeProviderToken(secret)
			if err != nil {
				return providerMetrics{}, "", err
			}
			credential = token
			r.token = token.AccessToken
			r.tokenType = token.TokenType
		}
		renew := func() error {
			if credential.RefreshToken == "" {
				return errProviderAuthentication
			}
			fresh, err := r.request(ctx, http.MethodPost, "/api/v1/auth/refresh", map[string]string{"refresh_token": credential.RefreshToken})
			if err != nil {
				return err
			}
			access, _ := fresh["access_token"].(string)
			if access == "" {
				return errProviderResponse
			}
			refresh, _ := fresh["refresh_token"].(string)
			if refresh == "" {
				refresh = credential.RefreshToken
			}
			freshType, _ := fresh["token_type"].(string)
			if freshType == "" {
				freshType = credential.TokenType
			}
			rotatedSecret, err = encodeProviderToken(access, refresh, freshType)
			if err != nil {
				return err
			}
			credential.AccessToken, credential.RefreshToken = access, refresh
			r.token = access
			r.tokenType = freshType
			return nil
		}
		if provider.AuthMode != "password" && r.token == "" {
			if err := renew(); err != nil {
				return providerMetrics{}, "", err
			}
		}
		profile, err := r.request(ctx, http.MethodGet, "/api/v1/user/profile", nil)
		if errors.Is(err, errProviderAuthentication) && credential.RefreshToken != "" {
			if err = renew(); err == nil {
				profile, err = r.request(ctx, http.MethodGet, "/api/v1/user/profile", nil)
			}
		}
		if err != nil {
			return providerMetrics{}, "", err
		}
		stats, err := r.request(ctx, http.MethodGet, "/api/v1/usage/dashboard/stats", nil)
		if errors.Is(err, errProviderAuthentication) && credential.RefreshToken != "" {
			if err = renew(); err == nil {
				stats, err = r.request(ctx, http.MethodGet, "/api/v1/usage/dashboard/stats", nil)
			}
		}
		if err != nil {
			return providerMetrics{}, "", err
		}
		return providerMetrics{
			Balance:       field(profile, "balance"),
			TodaySpend:    field(stats, "today_actual_cost", "today_cost"),
			TotalRecharge: field(profile, "total_recharged"),
			TotalSpend:    field(stats, "total_actual_cost", "total_cost"),
			Currency:      "USD",
		}, rotatedSecret, nil
	case "newapi":
		if provider.AuthMode == "password" {
			login, err := r.request(ctx, http.MethodPost, "/api/user/login", map[string]string{"username": provider.Account, "password": secret})
			if err != nil {
				return providerMetrics{}, "", err
			}
			// Current New API uses an in-memory dashboard JWT after login;
			// older installations keep a session cookie instead.
			r.token, _ = login["access_token"].(string)
		} else {
			token, err := decodeProviderToken(secret)
			if err != nil {
				return providerMetrics{}, "", err
			}
			r.token = token.AccessToken
			r.tokenType = token.TokenType
			r.userID = provider.UserID
		}
		profile, err := r.request(ctx, http.MethodGet, "/api/user/self", nil)
		if err != nil {
			return providerMetrics{}, "", err
		}
		status, err := r.request(ctx, http.MethodGet, "/api/status", nil)
		if err != nil {
			return providerMetrics{}, "", err
		}
		quotaPerUnit, ok := number(status["quota_per_unit"])
		if !ok || quotaPerUnit <= 0 {
			return providerMetrics{}, "", errors.New("the New API quota conversion is unavailable")
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
		return metrics, "", nil
	default:
		return providerMetrics{}, "", errors.New("unsupported provider type")
	}
}
