package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAttestationRoundTripRestoresBody(t *testing.T) {
	attestor, err := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-test","messages":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?probe=1", strings.NewReader(string(body)))

	require.NoError(t, attestor.SignRequest(req, "sk-managed", body))
	require.True(t, attestor.ValidateRequest(req, "sk-managed"))
	restored, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, body, restored)
}

func TestChannelMonitorAttestationRejectsTamperingAndExpiry(t *testing.T) {
	attestor, err := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-test"}`)

	tests := []struct {
		name   string
		mutate func(*http.Request)
		key    string
		at     time.Time
	}{
		{name: "body", mutate: func(req *http.Request) { req.Body = io.NopCloser(strings.NewReader(`{"model":"other"}`)) }, key: "sk-managed"},
		{name: "method", mutate: func(req *http.Request) { req.Method = http.MethodPut }, key: "sk-managed"},
		{name: "path", mutate: func(req *http.Request) { req.URL.Path = "/v1/responses" }, key: "sk-managed"},
		{name: "query", mutate: func(req *http.Request) { req.URL.RawQuery = "alt=sse" }, key: "sk-managed"},
		{name: "forged signature", mutate: func(req *http.Request) { req.Header.Set(ChannelMonitorSignatureHeader, strings.Repeat("a", 43)) }, key: "sk-managed"},
		{name: "key", key: "sk-leaked-copy"},
		{name: "expired", key: "sk-managed", at: time.Now().Add(-2 * channelMonitorAttestationSkew)},
		{name: "future", key: "sk-managed", at: time.Now().Add(2 * channelMonitorAttestationSkew)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := tt.at
			if at.IsZero() {
				at = time.Now()
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
			require.NoError(t, attestor.signRequestAt(req, "sk-managed", body, at))
			if tt.mutate != nil {
				tt.mutate(req)
			}
			require.False(t, attestor.ValidateRequest(req, tt.key))
		})
	}
}

func TestChannelMonitorAttestorRejectsInvalidSecret(t *testing.T) {
	_, err := NewChannelMonitorAttestor("not-a-32-byte-hex-key")
	require.ErrorIs(t, err, ErrChannelMonitorAttestationUnavailable)
}
