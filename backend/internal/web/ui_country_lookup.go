//go:build embed

package web

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

	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
)

const (
	uiCountryLookupURL = "https://get.geojs.io/v1/ip/geo/"
	uiCountryCacheTTL  = 10 * time.Minute
	uiCountryCacheMax  = 1024
)

var errUICountryUnavailable = errors.New("visitor country is unavailable")

type uiCountryResolver interface {
	Country(context.Context, string, bool) (string, error)
}

type uiCountryEntry struct {
	country string
	expires time.Time
}

type uiCountryFlight struct {
	done    chan struct{}
	country string
	err     error
}

// The endpoint is fixed, receives only a parsed public IP, and never receives
// the visited URL, cookies or credentials. Country headers from the client are
// deliberately ignored. Only the server's trusted-proxy chain selects the IP.
type geoJSCountryResolver struct {
	client   *http.Client
	endpoint string
	mu       sync.Mutex
	cache    map[string]uiCountryEntry
	inflight map[string]*uiCountryFlight
	capacity chan struct{}
}

func newUICountryResolver() *geoJSCountryResolver {
	return &geoJSCountryResolver{
		client: &http.Client{
			Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		endpoint: uiCountryLookupURL,
		cache:    make(map[string]uiCountryEntry),
		inflight: make(map[string]*uiCountryFlight),
		capacity: make(chan struct{}, 8),
	}
}

func (r *geoJSCountryResolver) Country(ctx context.Context, rawIP string, refresh bool) (string, error) {
	address, err := netip.ParseAddr(rawIP)
	if err != nil {
		return "", errUICountryUnavailable
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return "", errUICountryUnavailable
	}
	ip := address.String()
	r.mu.Lock()
	if refresh {
		// Retire the old decision before any overload/cancellation branch.
		delete(r.cache, ip)
	}
	if entry, ok := r.cache[ip]; !refresh && ok && time.Now().Before(entry.expires) {
		r.mu.Unlock()
		return entry.country, nil
	}
	if flight, ok := r.inflight[ip]; ok {
		r.mu.Unlock()
		select {
		case <-flight.done:
			return flight.country, flight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	// Do not let arbitrary IPs grow the pending-request map without a bound.
	select {
	case r.capacity <- struct{}{}:
	default:
		r.mu.Unlock()
		return "", errUICountryUnavailable
	}
	flight := &uiCountryFlight{done: make(chan struct{})}
	r.inflight[ip] = flight
	r.mu.Unlock()

	country, lookupErr := r.lookup(ctx, ip)
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
		r.cache[ip] = uiCountryEntry{country: country, expires: time.Now().Add(uiCountryCacheTTL)}
	} else if refresh {
		// A failed explicit retry must not fall back to an old allowed location.
		delete(r.cache, ip)
	}
	flight.country, flight.err = country, lookupErr
	close(flight.done)
	<-r.capacity
	r.mu.Unlock()
	return country, lookupErr
}

func (r *geoJSCountryResolver) lookup(ctx context.Context, ip string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint+url.PathEscape(ip)+".json", nil)
	if err != nil {
		return "", errUICountryUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-UI-Access-Control")
	response, err := r.client.Do(req)
	if err != nil {
		return "", errUICountryUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errUICountryUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return "", errUICountryUnavailable
	}
	var location struct {
		IP          string `json:"ip"`
		CountryCode string `json:"country_code"`
	}
	if json.Unmarshal(data, &location) != nil {
		return "", errUICountryUnavailable
	}
	returnedIP, err := netip.ParseAddr(location.IP)
	if err != nil || returnedIP.Unmap().String() != ip {
		return "", errUICountryUnavailable
	}
	country := strings.ToUpper(strings.TrimSpace(location.CountryCode))
	if !uiaccess.ValidCountryCode(country) {
		return "", errUICountryUnavailable
	}
	return country, nil
}
