package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamErrorBodyTimeoutUnblocksStalledHTTPResponse(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzip header stalls"}[compressed], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if compressed {
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(http.StatusGatewayTimeout)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			req.Header.Set("Accept-Encoding", "gzip")
			resp, err := server.Client().Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			boundUpstreamErrorBody(resp, 40*time.Millisecond)
			done := make(chan struct{})
			go func() {
				decompressResponseBody(resp)
				_, _ = io.ReadAll(resp.Body)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = resp.Body.Close()
				t.Fatal("stalled upstream error body blocked failover")
			}
			require.Equal(t, http.StatusGatewayTimeout, resp.StatusCode)
		})
	}
}

func TestUpstreamErrorBodyDeadlinePreservesCompleteErrorsAndSuccessfulResponses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTemporaryRedirect, http.StatusBadRequest, http.StatusGatewayTimeout} {
		original := io.NopCloser(strings.NewReader("provider response"))
		resp := &http.Response{StatusCode: status, Body: original}
		boundUpstreamErrorBody(resp, time.Second)
		if status < http.StatusMultipleChoices {
			require.True(t, original == resp.Body, "successful long streams must not have an error deadline")
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "provider response", string(body))
		require.NoError(t, resp.Body.Close())
	}
}

func TestUpstreamBodyDeadlinePrecedesRedirectDrainAndGrokInspection(t *testing.T) {
	for _, grok := range []bool{false, true} {
		t.Run(map[bool]string{false: "redirect drain", true: "Grok error inspection"}[grok], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if grok {
					w.WriteHeader(http.StatusForbidden)
				} else {
					w.Header().Set("Location", "http://untrusted.invalid/redirect")
					w.WriteHeader(http.StatusTemporaryRedirect)
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			defaultTransport, ok := http.DefaultTransport.(*http.Transport)
			require.True(t, ok)
			transport := defaultTransport.Clone()
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			t.Cleanup(transport.CloseIdleConnections)
			bounded := &upstreamErrorBodyTransport{base: transport, timeout: 40 * time.Millisecond}
			client := &http.Client{Transport: bounded, CheckRedirect: (&httpUpstreamService{}).redirectChecker}
			target := server.URL
			if grok {
				client.Transport = &grokAccessDeniedFallbackTransport{base: bounded}
				target = "http://" + grokCLIProxyHost + "/v1/chat/completions"
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, target, strings.NewReader("{}"))
			require.NoError(t, err)
			req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
			req.Header.Set("Authorization", "Bearer test-only")
			done := make(chan error, 1)
			go func() {
				resp, err := client.Do(req)
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				if grok {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "different origin")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("upstream stalled before the body deadline was installed")
			}
		})
	}
}

func TestUpstreamProxyLogValueDoesNotExposeCredentials(t *testing.T) {
	for _, raw := range []string{
		"http://proxy-user:proxy-password@proxy.example:8080/private?token=secret#fragment",
		"socks5://proxy-user:proxy-password@proxy.example:1080",
		"invalid%proxy-password",
		"direct",
		"",
	} {
		value := upstreamProxyLogValue(raw)
		for _, secret := range []string{"proxy-user", "proxy-password", "token", "secret", "fragment", "/private"} {
			require.NotContains(t, value, secret)
		}
	}
	require.Equal(t, "https://proxy.example:8443", upstreamProxyLogValue("https://user:password@proxy.example:8443"))
}
