//go:build embed

package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Export real middleware responses only when requested by the visual review
// workflow. Normal test runs do not create files in the source checkout.
func TestUIAccessNoticePreviewExport(t *testing.T) {
	directory := os.Getenv("UI_NOTICE_PREVIEW_DIR")
	if directory == "" {
		t.Skip("UI_NOTICE_PREVIEW_DIR is not configured")
	}
	require.True(t, filepath.IsAbs(directory), "preview output must be an absolute external path")
	require.NoError(t, os.MkdirAll(directory, 0o755))
	provider := &mockSettingsProvider{settings: map[string]any{
		"ui_access_domain_enabled": true, "ui_access_domain": "example.com",
		"ui_country_restriction_enabled": true, "ui_blocked_countries": "CN",
		"site_name": "杂鱼 API", "site_logo": "/logo.svg",
	}}
	server, router := newUIAccessDomainRouter(t, provider)
	server.countryResolver = uiCountryResolverFunc(func(context.Context, string, bool) (string, error) {
		return "CN", nil
	})
	type record struct {
		File       string      `json:"file"`
		Status     int         `json:"status"`
		Bytes      int         `json:"bytes"`
		Headers    http.Header `json:"headers"`
		SourceURL  string      `json:"source_url"`
		SourceHost string      `json:"source_host"`
	}
	var records []record
	for _, item := range []struct {
		name, host string
		status     int
	}{
		{"domain.html", "cdn.example.com", http.StatusForbidden},
		{"region.html", "example.com", http.StatusForbidden},
		{"region-unavailable.html", "example.com", http.StatusServiceUnavailable},
	} {
		if item.status == http.StatusServiceUnavailable {
			server.countryResolver = uiCountryResolverFunc(func(context.Context, string, bool) (string, error) {
				return "", errors.New("preview unavailable")
			})
		}
		response := requestUIAccessDomain(router, "/login", item.host, nil)
		require.Equal(t, item.status, response.Code)
		require.NoError(t, os.WriteFile(filepath.Join(directory, item.name), response.Body.Bytes(), 0o644))
		records = append(records, record{item.name, response.Code, response.Body.Len(), response.Header().Clone(), "/login", item.host})
	}
	logo, err := os.ReadFile(filepath.Join("..", "..", "..", "frontend", "public", "logo.svg"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "logo.svg"), logo, 0o644))
	metadata, err := json.MarshalIndent(records, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "responses.json"), metadata, 0o644))
	t.Logf("exported three actual notice responses to %s", directory)
}
