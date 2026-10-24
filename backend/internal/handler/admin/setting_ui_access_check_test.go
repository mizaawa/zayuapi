//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clientip "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUICountryDiagnosticUsesConfiguredClientIPAndRefresh(t *testing.T) {
	for _, test := range []struct {
		name   string
		compat bool
		want   string
	}{
		{"forwarded_compatibility", true, "223.5.5.5"},
		{"strict_trusted_proxy_mode", false, "172.18.0.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			require.NoError(t, router.SetTrustedProxies(nil))
			router.POST("/check", func(c *gin.Context) {
				clientip.SetForwardedIPSettings(c, test.compat, []string{"X-Visitor-IP"})
				checkUICountry(c, func(ctx context.Context, ip string, refresh bool) (uiaccess.CountryLookupResult, error) {
					require.Equal(t, test.want, ip)
					require.True(t, refresh)
					_, bounded := ctx.Deadline()
					require.True(t, bounded)
					return uiaccess.CountryLookupResult{Country: "CN", Provider: "country.is"}, nil
				})
			})
			req := httptest.NewRequest(http.MethodPost, "/check?ip=8.8.8.8", strings.NewReader(`{"ip":"8.8.8.8"}`))
			req.RemoteAddr = "172.18.0.1:1234"
			req.Header.Set("X-Visitor-IP", "223.5.5.5")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
			var body struct {
				Data struct {
					uiaccess.CountryLookupResult
					ElapsedMS int64 `json:"elapsed_ms"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, test.want, body.Data.IP)
			require.Equal(t, "CN", body.Data.Country)
			require.Equal(t, "country.is", body.Data.Provider)
			require.GreaterOrEqual(t, body.Data.ElapsedMS, int64(0))
		})
	}
}

func TestUICountryDiagnosticHidesRawProviderErrorAndStaleCountry(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/check", nil)
	c.Request.RemoteAddr = "223.5.5.5:1234"
	checkUICountry(c, func(context.Context, string, bool) (uiaccess.CountryLookupResult, error) {
		return uiaccess.CountryLookupResult{Country: "US"}, errors.New("provider internal secret details")
	})
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"error_code":"lookup_failed"`)
	require.Contains(t, w.Body.String(), `"country":""`)
	require.NotContains(t, w.Body.String(), "secret")
}
