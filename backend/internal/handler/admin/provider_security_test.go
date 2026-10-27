package admin

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAllowedProviderIPRejectsSpecialPurposeAndTranslationRanges(t *testing.T) {
	for _, raw := range []string{
		"10.0.0.1", "127.0.0.1", "169.254.169.254", "100.64.0.1", "0.1.2.3", "192.0.2.10",
		"198.51.100.7", "203.0.113.9", "240.0.0.1", "::ffff:10.0.0.1", "64:ff9b::a00:1",
		"2002:a00:1::1", "2001:0:4136:e378::1", "fd00::1", "fe80::1", "::1",
	} {
		require.False(t, allowedProviderIP(netip.MustParseAddr(raw)), raw)
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		require.True(t, allowedProviderIP(netip.MustParseAddr(raw)), raw)
	}
}

func TestProviderCredentialsRejectHeaderInjection(t *testing.T) {
	_, err := encodeProviderToken("token\r\nX-Injected: 1", "", "Bearer")
	require.Error(t, err)
	_, err = encodeProviderToken("", "re\nfresh", "Bearer")
	require.Error(t, err)
	_, err = encodeProviderToken("plain-token", "", "Bearer")
	require.NoError(t, err)
}

func TestProviderInputRejectsControlCharactersAndOversizedPassword(t *testing.T) {
	base := providerInput{Name: "Demo", SiteType: "sub2api", BaseURL: "https://example.com", AuthMode: "password", Account: "a@example.com", Password: "secret"}
	_, _, err := validateProviderInput(&base, nil)
	require.NoError(t, err)

	named := base
	named.Name = "Demo\nName"
	_, _, err = validateProviderInput(&named, nil)
	require.Error(t, err)

	account := base
	account.Account = "a@example.com\r\n"
	_, _, err = validateProviderInput(&account, nil)
	require.Error(t, err)

	long := base
	long.Password = string(make([]byte, 1025))
	_, _, err = validateProviderInput(&long, nil)
	require.Error(t, err)
}

func TestSameProviderIdentityTracksEveryAccountField(t *testing.T) {
	original := providerRecord{SecretEncrypted: "s", BaseURL: "https://a.example", Account: "u", SiteType: "newapi", AuthMode: "token", UserID: "1"}
	require.True(t, sameProviderIdentity(original, original))
	for _, mutate := range []func(*providerRecord){
		func(r *providerRecord) { r.SecretEncrypted = "x" },
		func(r *providerRecord) { r.BaseURL = "https://b.example" },
		func(r *providerRecord) { r.Account = "v" },
		func(r *providerRecord) { r.SiteType = "sub2api" },
		func(r *providerRecord) { r.AuthMode = "password" },
		func(r *providerRecord) { r.UserID = "2" },
	} {
		changed := original
		mutate(&changed)
		require.False(t, sameProviderIdentity(changed, original))
	}
}

func TestListProvidersRejectsInvalidFiltersBeforeLoading(t *testing.T) {
	gin.SetMode(gin.TestMode)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	for _, query := range []string{"status=unknown", "search=" + string(long)} {
		router := gin.New()
		router.GET("/providers", (&AccountHandler{}).ListProviders)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/providers?"+query, nil))
		require.Equal(t, http.StatusBadRequest, recorder.Code, query)
	}
}
