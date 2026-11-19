package admin

import (
	"context"
	"errors"
	"hash/maphash"
	"net/http"
	"strings"
	"time"
)

var (
	errProviderRefreshNeedsSave = errors.New("save the provider to verify its Refresh Token; a connection preview does not consume it")
	errProviderRefreshInvalid   = errors.New("the provider rejected the dashboard Refresh Token; it may be invalid or already used. Sign in on the provider site and save a fresh Refresh Token")
	errProviderRefreshExpired   = errors.New("the dashboard Refresh Token has expired; sign in on the provider site and save a fresh Refresh Token")
	errProviderSessionBound     = errors.New("the provider bound this session to its original login environment; use a server login method supported by the provider")
	errProviderSessionRevoked   = errors.New("the provider revoked this dashboard session; sign in again on the provider site")
)

func decodeSub2APIToken(secret string) (providerToken, error) {
	token, err := decodeProviderToken(secret)
	// Official Sub2API refresh tokens are prefixed rt_. Older saved entries can
	// put one in the access-token slot. Never send it as a Bearer credential.
	if err == nil && strings.HasPrefix(token.AccessToken, "rt_") {
		if token.RefreshToken == "" {
			token.RefreshToken = token.AccessToken
		}
		token.AccessToken = ""
	}
	return token, err
}

// Only documented codes are surfaced. Do not trust or return arbitrary error
// text, which some relays populate with the submitted credential.
func sub2APISessionError(path string, status int, envelope map[string]any) error {
	if !strings.HasPrefix(path, "/api/v1/") {
		return nil
	}
	if status == http.StatusOK {
		if code, ok := number(envelope["code"]); ok {
			status = int(code)
		}
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusBadRequest {
		return nil
	}
	reason, _ := envelope["reason"].(string)
	switch reason {
	case "REFRESH_TOKEN_INVALID", "REFRESH_TOKEN_REUSED":
		return errProviderRefreshInvalid
	case "REFRESH_TOKEN_EXPIRED":
		return errProviderRefreshExpired
	case "SESSION_BINDING_MISMATCH":
		return errProviderSessionBound
	case "TOKEN_REVOKED":
		return errProviderSessionRevoked
	}
	if status == http.StatusUnauthorized && path == "/api/v1/auth/refresh" {
		return errProviderRefreshInvalid
	}
	return nil
}

func (s *providerSession) sub2APIMetrics(ctx context.Context) (providerMetrics, error) {
	get := func(path string) (map[string]any, error) {
		data, _, err := s.call(ctx, http.MethodGet, path, nil, nil, true)
		if err != nil {
			return nil, err
		}
		object, ok := data.(map[string]any)
		if !ok {
			return nil, errProviderResponse
		}
		return object, nil
	}
	profile, err := get("/api/v1/user/profile")
	if err != nil {
		return providerMetrics{}, err
	}
	stats, err := get("/api/v1/usage/dashboard/stats")
	if err != nil {
		return providerMetrics{}, err
	}
	return providerMetrics{Balance: field(profile, "balance"), TodaySpend: field(stats, "today_actual_cost", "today_cost"), TotalRecharge: field(profile, "total_recharged"), TotalSpend: field(stats, "total_actual_cost", "total_cost"), Currency: "USD"}, nil
}

// A preview never owns a durable record for a newly supplied token. Test an
// access token if present, but do not let preview invalidate a single-use RT.
func (h *AccountHandler) previewProvider(ctx context.Context, record *providerRecord, secret string) error {
	if record.SiteType != "sub2api" || record.AuthMode == "password" {
		return h.probeRecordContext(ctx, record, secret)
	}
	if secret == "" {
		if h.providerEncryptor == nil {
			return errProviderStoredLogin
		}
		var err error
		secret, err = h.providerEncryptor.Decrypt(record.SecretEncrypted)
		if err != nil {
			return errProviderStoredLogin
		}
	}
	token, err := decodeSub2APIToken(secret)
	if err != nil {
		return errProviderStoredLogin
	}
	if token.RefreshToken == "" {
		return h.probeRecordContext(ctx, record, secret)
	}
	if token.AccessToken == "" {
		return errProviderRefreshNeedsSave
	}
	secret, err = encodeProviderToken(token.AccessToken, "", token.TokenType)
	if err != nil {
		return err
	}
	err = h.probeRecordContext(ctx, record, secret)
	if errors.Is(err, errProviderAuthentication) {
		return errProviderRefreshNeedsSave
	}
	return err
}

// Serialize all consumers of a saved RT in this process, including balance
// refresh and key import. Always take this gate before providerWriteMu. A
// bounded set of stripes avoids retaining identifiers or credential material.
var providerSessionSalt = maphash.MakeSeed()
var providerSessionGates = func() (g [64]chan struct{}) {
	for i := range g {
		g[i] = make(chan struct{}, 1)
	}
	return
}()

func lockProviderSession(ctx context.Context, id string) (func(), error) {
	gate := providerSessionGates[maphash.String(providerSessionSalt, id)%uint64(len(providerSessionGates))]
	// A free gate does not require a wait. In particular an admin edit can still
	// save the configuration when the request context canceled its remote probe.
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	default:
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r providerRecord) usesSub2APIToken() bool {
	return r.SiteType == "sub2api" && r.AuthMode != "password"
}

// Reload after acquiring the gate: another request may already have replaced
// the RT. Do not resolve a key against an identity or destination that changed.
func (h *AccountHandler) currentProviderSession(ctx context.Context, before providerRecord) (providerRecord, error) {
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	current, err := h.providerRecordByID(ctx, before.ID)
	if err != nil {
		return providerRecord{}, err
	}
	if current.BaseURL != before.BaseURL || current.Account != before.Account || current.SiteType != before.SiteType || current.AuthMode != before.AuthMode || current.modelGateway() != before.modelGateway() {
		return providerRecord{}, errors.New("provider changed; refresh the provider selection")
	}
	return current, nil
}

// Once an upstream write has consumed a credential, a browser disconnect or
// worker shutdown must not cancel the small local write that replaces it.
func providerCommitContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}
