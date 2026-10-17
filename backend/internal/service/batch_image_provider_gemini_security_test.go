//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiBatchHTTPClientDownloadRedirectSecurity(t *testing.T) {
	for _, phase := range []string{"metadata", "download"} {
		for _, destination := range []string{"same_origin", "different_port", "different_host", "http_downgrade", "userinfo"} {
			t.Run(phase+"/"+destination, func(t *testing.T) {
				var leakedRequests atomic.Int64
				leakHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					leakedRequests.Add(1)
					w.WriteHeader(http.StatusOK)
				})
				target := httptest.NewTLSServer(leakHandler)
				t.Cleanup(target.Close)
				plainTarget := httptest.NewServer(leakHandler)
				t.Cleanup(plainTarget.Close)
				var originURL, redirectURL string
				var downloaded atomic.Int64
				writeMetadata := func(w http.ResponseWriter, path string) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{"downloadUri": originURL + path})
				}
				origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("x-goog-api-key") != "test-gemini-secret" {
						t.Error("same-origin request lost the Gemini API key")
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					switch r.URL.Path {
					case "/v1beta/files/result":
						if phase == "metadata" {
							http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
						} else {
							writeMetadata(w, "/download")
						}
					case "/download":
						http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
					case "/result":
						if phase == "metadata" {
							writeMetadata(w, "/final")
							return
						}
						fallthrough
					case "/final":
						downloaded.Add(1)
						w.Header().Set("Content-Type", "application/jsonl")
						_, _ = io.WriteString(w, "{\"key\":\"result\"}\n")
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				t.Cleanup(origin.Close)
				originURL = origin.URL
				redirectURL = origin.URL + "/result"
				switch destination {
				case "different_port":
					redirectURL = target.URL + "/result"
				case "different_host":
					redirectURL = strings.Replace(target.URL, "127.0.0.1", "localhost", 1) + "/result"
				case "http_downgrade":
					redirectURL = plainTarget.URL + "/result"
				case "userinfo":
					parsed, err := url.Parse(redirectURL)
					require.NoError(t, err)
					parsed.User = url.UserPassword("untrusted", "secret")
					redirectURL = parsed.String()
				}
				client := NewGeminiBatchHTTPClient(origin.URL, origin.Client())
				body, contentType, err := client.DownloadFile(context.Background(), "test-gemini-secret", "files/result")
				if destination == "same_origin" {
					require.NoError(t, err)
					t.Cleanup(func() { _ = body.Close() })
					data, readErr := io.ReadAll(body)
					require.NoError(t, readErr)
					require.Equal(t, "{\"key\":\"result\"}\n", string(data))
					require.Equal(t, "application/jsonl", contentType)
					require.EqualValues(t, 1, downloaded.Load())
				} else {
					require.ErrorContains(t, err, "unsafe origin")
					require.Nil(t, body)
					require.Zero(t, downloaded.Load())
				}
				require.Zero(t, leakedRequests.Load(), "a redirect must never send the API key to another origin")
			})
		}
	}
}

func TestGeminiBatchHTTPClientRedirectPolicy(t *testing.T) {
	client := NewGeminiBatchHTTPClient("", nil)
	initial, err := http.NewRequest(http.MethodGet, "https://generativelanguage.googleapis.com/start", nil)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, target string
		allowed      bool
	}{
		{"same origin", "https://generativelanguage.googleapis.com:443/result", true},
		{"different Google host", "https://storage.googleapis.com/result", false},
		{"different port", "https://generativelanguage.googleapis.com:8443/result", false},
		{"scheme downgrade", "http://generativelanguage.googleapis.com/result", false},
		{"embedded userinfo", "https://user:secret@generativelanguage.googleapis.com/result", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, parseErr := http.NewRequest(http.MethodGet, tc.target, nil)
			require.NoError(t, parseErr)
			redirectErr := client.client.CheckRedirect(target, []*http.Request{initial})
			if tc.allowed {
				require.NoError(t, redirectErr)
			} else {
				require.ErrorContains(t, redirectErr, "unsafe origin")
			}
		})
	}
	require.ErrorContains(t, client.client.CheckRedirect(initial, make([]*http.Request, 10)), "too many")
}

func TestGeminiBatchHTTPClientPreservesInjectedRedirectPolicy(t *testing.T) {
	var called bool
	injected := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		called = true
		return http.ErrUseLastResponse
	}}
	client := NewGeminiBatchHTTPClient("", injected)
	require.NotSame(t, injected, client.client)
	initial, err := http.NewRequest(http.MethodGet, "https://example.com/start", nil)
	require.NoError(t, err)
	target, err := http.NewRequest(http.MethodGet, "https://example.com/result", nil)
	require.NoError(t, err)
	require.ErrorIs(t, client.client.CheckRedirect(target, []*http.Request{initial}), http.ErrUseLastResponse)
	require.True(t, called)
	called = false
	target.URL.Host = "other.example.com"
	require.ErrorContains(t, client.client.CheckRedirect(target, []*http.Request{initial}), "unsafe origin")
	require.False(t, called, "an injected policy cannot override the credential boundary")
	require.ErrorIs(t, injected.CheckRedirect(target, []*http.Request{initial}), http.ErrUseLastResponse)
	require.True(t, called, "the injected client's original policy must remain unchanged")
}

func TestGeminiBatchHTTPClientUploadRedirectDoesNotLeak(t *testing.T) {
	var leakedRequests atomic.Int64
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leakedRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	client := NewGeminiBatchHTTPClient(origin.URL, origin.Client())
	_, err := client.UploadJSONL(context.Background(), "test-secret", "private batch", strings.NewReader("private prompt"))
	require.ErrorContains(t, err, "unsafe origin")
	require.Zero(t, leakedRequests.Load(), "neither API key nor uploaded prompts may reach another origin")
}

func TestValidateGeminiDownloadHostRejectsUnsafeOrigins(t *testing.T) {
	for _, target := range []string{
		"https://user:secret@generativelanguage.googleapis.com/result",
		"http://generativelanguage.googleapis.com/result",
		"https://custom.example.com:8443/result",
		"https://other.example.com/result",
	} {
		t.Run(target, func(t *testing.T) {
			require.Error(t, validateGeminiDownloadHost(target, "https://custom.example.com"))
		})
	}
	require.NoError(t, validateGeminiDownloadHost("https://custom.example.com:443/result", "https://custom.example.com"))
	require.NoError(t, validateGeminiDownloadHost("https://generativelanguage.googleapis.com/result", "https://custom.example.com"))
}
