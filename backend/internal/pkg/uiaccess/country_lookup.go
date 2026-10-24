package uiaccess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	CountryLookupTimeout   = 6500 * time.Millisecond
	countryProviderTimeout = 3 * time.Second
	uiCountryLookupURL     = "https://get.geojs.io/v1/ip/geo/"
	uiCountryFallbackURL   = "https://api.country.is/"
	uiCountryCacheTTL      = 10 * time.Minute
	uiCountryCacheMax      = 1024
)

// CountryLookupResult is safe to expose only through an authenticated admin
// endpoint. Provider failures use bounded codes, never upstream response bodies.
type CountryLookupResult struct {
	IP        string                 `json:"ip"`
	Country   string                 `json:"country"`
	Provider  string                 `json:"provider"`
	Cached    bool                   `json:"cached"`
	ErrorCode string                 `json:"error_code,omitempty"`
	Attempts  []CountryLookupAttempt `json:"attempts,omitempty"`
}

type CountryLookupAttempt struct {
	Provider  string `json:"provider"`
	ErrorCode string `json:"error_code,omitempty"`
}

type countryLookupError struct{ code string }

func (e *countryLookupError) Error() string { return "visitor country unavailable: " + e.code }

// CountryLookupErrorCode reports a stable, non-sensitive reason to operators.
func CountryLookupErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var lookupErr *countryLookupError
	if errors.As(err, &lookupErr) {
		return lookupErr.code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "lookup_unavailable"
}

func countryFailure(code string) error { return &countryLookupError{code: code} }

type uiCountryEntry struct {
	result  CountryLookupResult
	expires time.Time
}
type uiCountryFlight struct {
	done   chan struct{}
	result CountryLookupResult
	err    error
}

// CountryResolver calls only two fixed HTTPS providers with a parsed public IP.
// It sends no browser URL, credentials, cookies or untrusted country headers.
type CountryResolver struct {
	client           *http.Client
	endpoint         string
	fallbackEndpoint string
	mu               sync.Mutex
	cache            map[string]uiCountryEntry
	inflight         map[string]*uiCountryFlight
	capacity         chan struct{}
}

func NewCountryResolver() *CountryResolver {
	return &CountryResolver{
		client:   &http.Client{Timeout: countryProviderTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		endpoint: uiCountryLookupURL, fallbackEndpoint: uiCountryFallbackURL,
		cache: make(map[string]uiCountryEntry), inflight: make(map[string]*uiCountryFlight), capacity: make(chan struct{}, 8),
	}
}

var defaultCountryResolver = NewCountryResolver()

// DetectCountry shares the UI resolver and its bounded cache with admin
// diagnostics. A refresh retires the previous decision before a new lookup.
func DetectCountry(ctx context.Context, rawIP string, refresh bool) (CountryLookupResult, error) {
	return defaultCountryResolver.Detect(ctx, rawIP, refresh)
}

func (r *CountryResolver) Country(ctx context.Context, rawIP string, refresh bool) (string, error) {
	result, err := r.Detect(ctx, rawIP, refresh)
	return result.Country, err
}

func (r *CountryResolver) Detect(ctx context.Context, rawIP string, refresh bool) (CountryLookupResult, error) {
	result := CountryLookupResult{}
	address, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		result.ErrorCode = "invalid_ip"
		return result, countryFailure(result.ErrorCode)
	}
	address = address.Unmap()
	result.IP = address.String()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		result.ErrorCode = "non_public_ip"
		return result, countryFailure(result.ErrorCode)
	}
	ip := result.IP
	r.mu.Lock()
	if refresh {
		delete(r.cache, ip)
	}
	if entry, ok := r.cache[ip]; !refresh && ok && time.Now().Before(entry.expires) {
		r.mu.Unlock()
		result = entry.result
		result.Cached = true
		return result, nil
	}
	if flight, ok := r.inflight[ip]; ok {
		r.mu.Unlock()
		select {
		case <-flight.done:
			return flight.result, flight.err
		case <-ctx.Done():
			result.ErrorCode = CountryLookupErrorCode(ctx.Err())
			return result, ctx.Err()
		}
	}
	select {
	case r.capacity <- struct{}{}:
	default:
		r.mu.Unlock()
		result.ErrorCode = "busy"
		return result, countryFailure(result.ErrorCode)
	}
	flight := &uiCountryFlight{done: make(chan struct{})}
	r.inflight[ip] = flight
	r.mu.Unlock()

	lookupCtx, cancel := context.WithTimeout(ctx, CountryLookupTimeout)
	result, lookupErr := r.lookup(lookupCtx, ip)
	cancel()
	r.mu.Lock()
	delete(r.inflight, ip)
	if lookupErr == nil {
		if len(r.cache) >= uiCountryCacheMax {
			for key, entry := range r.cache {
				if time.Now().After(entry.expires) {
					delete(r.cache, key)
				}
			}
			if len(r.cache) >= uiCountryCacheMax {
				for key := range r.cache {
					delete(r.cache, key)
					break
				}
			}
		}
		r.cache[ip] = uiCountryEntry{result: result, expires: time.Now().Add(uiCountryCacheTTL)}
	}
	flight.result, flight.err = result, lookupErr
	close(flight.done)
	<-r.capacity
	r.mu.Unlock()
	return result, lookupErr
}

func (r *CountryResolver) lookup(ctx context.Context, ip string) (CountryLookupResult, error) {
	result := CountryLookupResult{IP: ip}
	for _, provider := range []struct{ name, endpoint, suffix, countryField string }{
		{"geojs", r.endpoint, ".json", "country_code"}, {"country_is", r.fallbackEndpoint, "", "country"},
	} {
		if provider.endpoint == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			result.ErrorCode = CountryLookupErrorCode(err)
			return result, err
		}
		country, err := r.lookupProvider(ctx, provider.endpoint+url.PathEscape(ip)+provider.suffix, provider.countryField, ip)
		result.Attempts = append(result.Attempts, CountryLookupAttempt{Provider: provider.name, ErrorCode: CountryLookupErrorCode(err)})
		if err == nil {
			result.Country, result.Provider = country, provider.name
			return result, nil
		}
	}
	result.ErrorCode = "providers_unavailable"
	if ctx.Err() != nil {
		result.ErrorCode = CountryLookupErrorCode(ctx.Err())
	}
	return result, countryFailure(result.ErrorCode)
}

func (r *CountryResolver) lookupProvider(ctx context.Context, endpoint, countryField, ip string) (string, error) {
	providerCtx, cancel := context.WithTimeout(ctx, countryProviderTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(providerCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", countryFailure("request_invalid")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-UI-Access-Control")
	response, err := r.client.Do(req)
	if err != nil {
		if providerCtx.Err() != nil {
			return "", providerCtx.Err()
		}
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) && netErr.Timeout() {
			return "", countryFailure("timeout")
		}
		return "", countryFailure("network_error")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", countryFailure("provider_http_error")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return "", countryFailure("response_invalid")
	}
	var location map[string]json.RawMessage
	if json.Unmarshal(data, &location) != nil {
		return "", countryFailure("response_invalid")
	}
	var returnedIPText, country string
	if json.Unmarshal(location["ip"], &returnedIPText) != nil || json.Unmarshal(location[countryField], &country) != nil {
		return "", countryFailure("response_invalid")
	}
	returnedIP, err := netip.ParseAddr(returnedIPText)
	if err != nil || returnedIP.Unmap().String() != ip {
		return "", countryFailure("response_ip_mismatch")
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	if !ValidCountryCode(country) {
		return "", countryFailure("country_invalid")
	}
	return country, nil
}
