//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUIAccessDomainSettingsReadAndPublicInjection(t *testing.T) {
	for _, domain := range []string{"", "example.com"} {
		t.Run(domain, func(t *testing.T) {
			values := map[string]string{}
			if domain != "" {
				values[SettingKeyUIAccessDomain] = domain
			}
			admin := NewSettingService(&settingGetAllRepoStub{values: values}, &config.Config{})
			all, err := admin.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, domain, all.UIAccessDomain)
			require.Equal(t, domain != "", all.UIAccessDomainEnabled)
			require.False(t, all.UICountryRestrictionEnabled)
			require.Empty(t, all.UIBlockedCountries)
			public := NewSettingService(&settingPublicRepoStub{values: values}, &config.Config{})
			settings, err := public.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, domain, settings.UIAccessDomain)
			require.Equal(t, domain != "", settings.UIAccessDomainEnabled)
			injected, err := public.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			payload, err := json.Marshal(injected)
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(payload, &data))
			require.Equal(t, domain, data["ui_access_domain"])
			require.Equal(t, domain != "", data["ui_access_domain_enabled"])
			require.Equal(t, false, data["ui_country_restriction_enabled"])
			require.Equal(t, "", data["ui_blocked_countries"])
		})
	}
}

func TestUIAccessControlsReadExplicitSwitchesAndLegacyDomain(t *testing.T) {
	for _, test := range []struct {
		name      string
		values    map[string]string
		enabled   bool
		countries bool
	}{
		{"missing settings", map[string]string{}, false, false},
		{"legacy nonempty domain", map[string]string{SettingKeyUIAccessDomain: "example.com"}, true, false},
		{"legacy blank domain", map[string]string{SettingKeyUIAccessDomain: "  "}, false, false},
		{"explicitly disabled domain", map[string]string{SettingKeyUIAccessDomain: "example.com", SettingKeyUIAccessDomainEnabled: "false"}, false, false},
		{"explicitly enabled domain", map[string]string{SettingKeyUIAccessDomain: "example.com", SettingKeyUIAccessDomainEnabled: "true"}, true, false},
		{"country enabled", map[string]string{SettingKeyUICountryRestrictionEnabled: "true", SettingKeyUIBlockedCountries: "CN,US"}, false, true},
		{"country list alone", map[string]string{SettingKeyUIBlockedCountries: "CN,US"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			admin := NewSettingService(&settingGetAllRepoStub{values: test.values}, &config.Config{})
			all, err := admin.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, test.enabled, all.UIAccessDomainEnabled)
			require.Equal(t, test.countries, all.UICountryRestrictionEnabled)
			require.Equal(t, test.values[SettingKeyUIBlockedCountries], all.UIBlockedCountries)
			public := NewSettingService(&settingPublicRepoStub{values: test.values}, &config.Config{})
			settings, err := public.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, test.enabled, settings.UIAccessDomainEnabled)
			require.Equal(t, test.countries, settings.UICountryRestrictionEnabled)
			require.Equal(t, all.UIBlockedCountries, settings.UIBlockedCountries)
			injected, err := public.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			payload, err := json.Marshal(injected)
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, json.Unmarshal(payload, &data))
			require.Equal(t, test.enabled, data[SettingKeyUIAccessDomainEnabled])
			require.Equal(t, test.countries, data[SettingKeyUICountryRestrictionEnabled])
			require.Equal(t, all.UIBlockedCountries, data[SettingKeyUIBlockedCountries])
		})
	}
}

func TestUIAccessControlsPersistNormalizeAndValidate(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repo := &settingUpdateRepoStub{}
		svc := NewSettingService(repo, &config.Config{})
		settings := &SystemSettings{UIAccessDomain: " Example.COM. ", UIAccessDomainEnabled: enabled, UICountryRestrictionEnabled: enabled, UIBlockedCountries: "cn, US,cn, HK"}
		require.NoError(t, svc.UpdateSettings(context.Background(), settings))
		require.Equal(t, "example.com", repo.updates[SettingKeyUIAccessDomain])
		require.Equal(t, "CN,US,HK", repo.updates[SettingKeyUIBlockedCountries])
		require.Equal(t, enabled, repo.updates[SettingKeyUIAccessDomainEnabled] == "true")
		require.Equal(t, enabled, repo.updates[SettingKeyUICountryRestrictionEnabled] == "true")
	}
	for _, settings := range []*SystemSettings{
		{UIAccessDomainEnabled: true},
		{UIAccessDomainEnabled: true, UIAccessDomain: "   "},
		{UICountryRestrictionEnabled: true},
		{UICountryRestrictionEnabled: true, UIBlockedCountries: " \t"},
		{UIBlockedCountries: "XX"},
		{UIBlockedCountries: "XK"},
		{UIBlockedCountries: "CN,,US"},
		{UIBlockedCountries: "CN,"},
	} {
		repo := &settingUpdateRepoStub{}
		svc := NewSettingService(repo, &config.Config{})
		require.Error(t, svc.UpdateSettings(context.Background(), settings))
		require.Nil(t, repo.updates)
	}
}

func TestUIAccessDomainSettingsPersistenceAndInvalidation(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	invalidations := 0
	svc.SetOnUpdateCallback(func() { invalidations++ })
	for _, test := range []struct{ input, want string }{{" Example.COM. ", "example.com"}, {"例子.中国", "xn--fsqu00a.xn--fiqs8s"}, {"", ""}} {
		settings := &SystemSettings{UIAccessDomain: test.input}
		require.NoError(t, svc.UpdateSettings(context.Background(), settings))
		require.Equal(t, test.want, settings.UIAccessDomain)
		require.Equal(t, test.want, repo.updates[SettingKeyUIAccessDomain])
	}
	require.Equal(t, 3, invalidations)
	for _, domain := range []string{"https://example.com", "example.com:443", "*.example.com", "user@example.com", "localhost", "127.0.0.1"} {
		repo.updates = nil
		require.Error(t, svc.UpdateSettings(context.Background(), &SystemSettings{UIAccessDomain: domain}))
		require.Nil(t, repo.updates)
	}
	require.Equal(t, 3, invalidations, "rejected writes must not invoke an update callback")
}

type uiAccessDomainRefreshFailureRepo struct{ settingUpdateRepoStub }

func (r *uiAccessDomainRefreshFailureRepo) GetAll(context.Context) (map[string]string, error) {
	return nil, errors.New("refresh unavailable")
}

func TestUIAccessDomainSuccessfulPartialWriteStillInvalidatesOnRefreshFailure(t *testing.T) {
	repo := &uiAccessDomainRefreshFailureRepo{}
	svc := NewSettingService(repo, &config.Config{})
	invalidations := 0
	svc.SetOnUpdateCallback(func() { invalidations++ })
	err := svc.UpdateSettingsOmitting(context.Background(), &SystemSettings{UIAccessDomain: "example.com"}, OmittedSettingKeys{SettingKeySiteName: {}})
	require.NoError(t, err)
	require.Equal(t, "example.com", repo.updates[SettingKeyUIAccessDomain])
	require.Equal(t, 1, invalidations)
}
