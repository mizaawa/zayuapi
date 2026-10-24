//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUIAccessDomainAdminReadUpdateAndOmission(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	invalidations := 0
	h.settingService.SetOnUpdateCallback(func() { invalidations++ })
	for _, test := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"ui_access_domain": " Example.COM. "}, "example.com"},
		{map[string]any{"risk_control_enabled": true}, "example.com"},
		{map[string]any{"ui_access_domain": "例子.中国"}, "xn--fsqu00a.xn--fiqs8s"},
		{map[string]any{"ui_access_domain": ""}, ""},
	} {
		response := doUpdateSettings(t, h, test.body, nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, test.want, repo.values[service.SettingKeyUIAccessDomain])
		var payload struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
		require.Equal(t, test.want, payload.Data["ui_access_domain"])
		read := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(read)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(ctx)
		require.Equal(t, http.StatusOK, read.Code)
		require.NoError(t, json.Unmarshal(read.Body.Bytes(), &payload))
		require.Equal(t, test.want, payload.Data["ui_access_domain"])
	}
	require.Equal(t, 4, invalidations)
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{UIAccessDomain: "example.com"}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "ui_access_domain")
}

func TestUIAccessDomainAdminRejectsInvalidBeforeAnyWrite(t *testing.T) {
	for _, domain := range []any{"https://example.com", "example.com:443", "user@example.com", "*.example.com", "example.com/path", "localhost", "127.0.0.1", nil, 123} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyUIAccessDomain: "old.example.com", service.SettingKeySiteName: "unchanged"})
		response := doUpdateSettings(t, h, map[string]any{"ui_access_domain": domain, "site_name": "must-not-write"}, nil)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Equal(t, "old.example.com", repo.values[service.SettingKeyUIAccessDomain])
		require.Equal(t, "unchanged", repo.values[service.SettingKeySiteName])
		require.Nil(t, repo.lastUpdates)
	}
}

func TestUIAccessControlsAdminPartialUpdatesPreservePolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		stored      map[string]string
		body        map[string]any
		domain      string
		domainOn    bool
		countries   string
		countriesOn bool
	}{
		{
			name:   "legacy domain survives unrelated save",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "example.com"},
			body:   map[string]any{"risk_control_enabled": true}, domain: "example.com", domainOn: true,
		},
		{
			name:   "off switch preserves configured domain and country list",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "true", service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "CN,US"},
			body:   map[string]any{"ui_access_domain_enabled": false, "ui_country_restriction_enabled": false}, domain: "example.com", countries: "CN,US",
		},
		{
			name:   "on switch uses previously configured values",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "false", service.SettingKeyUIBlockedCountries: "CN,US"},
			body:   map[string]any{"ui_access_domain_enabled": true, "ui_country_restriction_enabled": true}, domain: "example.com", domainOn: true, countries: "CN,US", countriesOn: true,
		},
		{
			name:   "domain and list edit keep disabled switches disabled",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "old.example.com", service.SettingKeyUIAccessDomainEnabled: "false", service.SettingKeyUICountryRestrictionEnabled: "false", service.SettingKeyUIBlockedCountries: "HK"},
			body:   map[string]any{"ui_access_domain": " New.Example.COM. ", "ui_blocked_countries": " us, cn,US "}, domain: "new.example.com", countries: "US,CN",
		},
		{
			name:   "domain and list edit keep enabled switches enabled",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "old.example.com", service.SettingKeyUIAccessDomainEnabled: "true", service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "HK"},
			body:   map[string]any{"ui_access_domain": " New.Example.COM. ", "ui_blocked_countries": " us, cn,US "}, domain: "new.example.com", domainOn: true, countries: "US,CN", countriesOn: true,
		},
		{
			name:   "unrelated request keeps both enabled policies",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "true", service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "CN,US"},
			body:   map[string]any{"site_name": "new name"}, domain: "example.com", domainOn: true, countries: "CN,US", countriesOn: true,
		},
		{
			name:   "explicitly disabled policy allows clearing values",
			stored: map[string]string{service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "true", service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "CN,US"},
			body:   map[string]any{"ui_access_domain_enabled": false, "ui_access_domain": "", "ui_country_restriction_enabled": false, "ui_blocked_countries": ""},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, test.stored)
			response := doUpdateSettings(t, h, test.body, nil)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Equal(t, test.domain, repo.values[service.SettingKeyUIAccessDomain])
			require.Equal(t, test.countries, repo.values[service.SettingKeyUIBlockedCountries])
			require.Equal(t, test.domainOn, repo.values[service.SettingKeyUIAccessDomainEnabled] == "true")
			require.Equal(t, test.countriesOn, repo.values[service.SettingKeyUICountryRestrictionEnabled] == "true")
			var payload struct {
				Data map[string]any `json:"data"`
			}
			for _, read := range []bool{false, true} {
				if read {
					response = httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(response)
					ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
					h.GetSettings(ctx)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
				require.Equal(t, test.domain, payload.Data["ui_access_domain"])
				require.Equal(t, test.domainOn, payload.Data["ui_access_domain_enabled"])
				require.Equal(t, test.countries, payload.Data["ui_blocked_countries"])
				require.Equal(t, test.countriesOn, payload.Data["ui_country_restriction_enabled"])
			}
		})
	}
}

func TestUIAccessControlsAdminRejectsInvalidBeforeAnyWrite(t *testing.T) {
	cases := []map[string]any{
		{"ui_access_domain_enabled": true, "ui_access_domain": ""},
		{"ui_access_domain": ""},
		{"ui_country_restriction_enabled": true, "ui_blocked_countries": ""},
		{"ui_blocked_countries": ""},
	}
	for _, key := range []string{"ui_access_domain_enabled", "ui_country_restriction_enabled"} {
		for _, value := range []any{nil, "true", 1} {
			cases = append(cases, map[string]any{key: value})
		}
	}
	for _, countries := range []any{nil, []string{"CN"}, 123, "XX", "ZZ", "XK", "USA", "840", "CN,", "CN,,US", "CN US", "CN;US"} {
		cases = append(cases, map[string]any{"ui_blocked_countries": countries})
	}
	for _, body := range cases {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyUIAccessDomain: "example.com", service.SettingKeyUIAccessDomainEnabled: "true",
			service.SettingKeyUICountryRestrictionEnabled: "true", service.SettingKeyUIBlockedCountries: "CN,US", service.SettingKeySiteName: "unchanged",
		})
		body["site_name"] = "must-not-write"
		response := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Equal(t, "unchanged", repo.values[service.SettingKeySiteName])
		require.Equal(t, "example.com", repo.values[service.SettingKeyUIAccessDomain])
		require.Equal(t, "CN,US", repo.values[service.SettingKeyUIBlockedCountries])
		require.Nil(t, repo.lastUpdates)
	}
	for _, key := range []string{"ui_access_domain_enabled", "ui_country_restriction_enabled"} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
		response := doUpdateSettings(t, h, map[string]any{key: true, "site_name": "must-not-write"}, nil)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.Nil(t, repo.lastUpdates)
	}
}

func TestUIAccessControlsAuditTracksEveryPolicyField(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{
		UIAccessDomain: "example.com", UIAccessDomainEnabled: true,
		UICountryRestrictionEnabled: true, UIBlockedCountries: "CN,US",
	}, nil, nil, UpdateSettingsRequest{})
	for _, key := range []string{"ui_access_domain", "ui_access_domain_enabled", "ui_country_restriction_enabled", "ui_blocked_countries"} {
		require.Contains(t, changed, key)
	}
}
