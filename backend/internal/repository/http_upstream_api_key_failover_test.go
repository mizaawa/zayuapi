package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamAPIKeyFailoverDisablesGrokProxyRetry(t *testing.T) {
	calls := 0
	transport := &grokAccessDeniedFallbackTransport{base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"Access denied"}`)),
			Request:    req,
		}, nil
	})}
	request, err := http.NewRequestWithContext(service.WithAPIKeyFailoverAttempt(t.Context()), http.MethodPost, "https://cli-chat-proxy.grok.com/v1/responses", strings.NewReader(`{"model":"grok-test"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, calls, "proxy fallback must not add an upstream call outside the key's attempt limit")
}

func TestHTTPUpstreamAPIKeyFailoverStopsAfterFirstFailure(t *testing.T) {
	for _, tlsProfile := range []*tlsfingerprint.Profile{nil, {Name: "plain-http-delegation"}} {
		name := "DoWithTLSNil"
		if tlsProfile != nil {
			name = "DoWithTLSPlainHTTP"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) <= 2 {
					_, _ = w.Write([]byte("success"))
					return
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"Upstream capacity exhausted"}}`))
			}))
			t.Cleanup(server.Close)
			upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
			ctx := service.WithAPIKeyFailoverAttempt(t.Context())
			for i := 0; i < 3; i++ {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
				require.NoError(t, err)
				resp, err := upstream.DoWithTLS(req, "", 10, 1, tlsProfile)
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				if i == 2 {
					require.Contains(t, string(body), "Upstream capacity exhausted")
				}
			}
			require.True(t, service.APIKeyFailoverAttemptFailed(ctx))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			_, err = upstream.DoWithTLS(req, "", 10, 1, tlsProfile)
			require.ErrorIs(t, err, service.ErrAPIKeyFailoverAttemptExhausted)
			require.Equal(t, int64(3), calls.Load(), "successful steps are unlimited; retries after a failure are blocked")
			for _, entry := range upstream.clients {
				require.Zero(t, atomic.LoadInt64(&entry.inFlight), "error-body replay must retain tracked Close")
			}

			req = req.WithContext(service.WithAPIKeyFailoverAttempt(t.Context()))
			resp, err := upstream.Do(req, "", 10, 1)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, int64(4), calls.Load(), "the next handler invocation gets a fresh attempt")
		})
	}
}

func TestHTTPUpstreamAPIKeyFailoverTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	ctx := service.WithAPIKeyFailoverAttempt(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	upstream := NewHTTPUpstream(nil)
	_, err = upstream.Do(req, "", 10, 1)
	require.Error(t, err)
	require.True(t, service.APIKeyFailoverAttemptFailed(ctx))
	_, err = upstream.Do(req, "", 10, 1)
	require.ErrorIs(t, err, service.ErrAPIKeyFailoverAttemptExhausted)
}

func TestHTTPUpstreamAPIKeyFailoverPreservesMissingModelDecision(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"model_not_found","message":"No such model"}}`))
	}))
	t.Cleanup(server.Close)
	ctx := service.WithAPIKeyFailoverAttempt(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	upstream := NewHTTPUpstream(nil)
	resp, err := upstream.Do(req, "", 1, 1)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.True(t, service.APIKeyFailoverAttemptModelUnavailable(ctx))

	tlsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://unused.example", nil)
	require.NoError(t, err)
	_, err = upstream.DoWithTLS(tlsReq, "", 1, 1, &tlsfingerprint.Profile{Name: "guard-before-dial"})
	require.ErrorIs(t, err, service.ErrAPIKeyFailoverAttemptExhausted)
	require.Equal(t, int64(1), calls.Load())

	for i := 0; i < 2; i++ {
		resp, err = upstream.Do(req.WithContext(t.Context()), "", 1, 1)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.Equal(t, int64(3), calls.Load(), "unconfigured keys retain existing retry behavior")
}

func TestHTTPUpstreamFailoverFailureInspectionPreservesFullBody(t *testing.T) {
	body := `{"error":{"code":"model_not_found"}}` + strings.Repeat("x", 100<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	for _, enabled := range []bool{false, true} {
		ctx := context.Background()
		if enabled {
			ctx = service.WithAPIKeyFailoverAttempt(ctx)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := NewHTTPUpstream(nil).Do(req, "", 1, 1)
		require.NoError(t, err)
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(got))
		require.NoError(t, resp.Body.Close())
		require.Equal(t, enabled, service.APIKeyFailoverAttemptFailed(ctx))
	}
}
