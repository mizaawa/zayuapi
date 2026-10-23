//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWeChatPaymentOAuthExclusiveAccessIsHiddenAndRevocationApplies(t *testing.T) {
	t.Setenv("PAYMENT_RESUME_SIGNING_KEY", "0123456789abcdef0123456789abcdef")
	gin.SetMode(gin.TestMode)
	originalURL := wechatOAuthAccessTokenURL
	t.Cleanup(func() { wechatOAuthAccessTokenURL = originalURL })
	exchanges := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"upstream","openid":"openid-authorized","scope":"snsapi_base"}`))
	}))
	defer upstream.Close()
	wechatOAuthAccessTokenURL = upstream.URL
	settings := wechatOAuthTestSettings("mp", "wx-mp-app", "secret", "/auth/wechat/callback")
	settings[service.SettingPaymentExclusiveEnabled] = "true"
	h, client := newWeChatOAuthTestHandlerWithSettings(t, false, settings)
	defer client.Close()
	ctx := context.Background()
	u, err := client.User.Create().SetEmail("oauth-exclusive@example.com").SetPasswordHash("hash").SetExclusiveRechargeEnabled(true).Save(ctx)
	require.NoError(t, err)
	token, err := h.wechatPaymentResumeService().CreateWeChatPaymentAccessToken(u.ID)
	require.NoError(t, err)
	router := gin.New()
	router.GET("/start", h.WeChatPaymentOAuthStart)
	router.GET("/callback", h.WeChatPaymentOAuthCallback)
	request := func(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		router.ServeHTTP(r, req)
		return r
	}
	unknown := request("/unknown")
	assertHidden := func(r *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, unknown.Code, r.Code)
		require.Equal(t, unknown.Body.String(), r.Body.String())
		require.Equal(t, unknown.Header().Get("Content-Type"), r.Header().Get("Content-Type"))
		require.Empty(t, r.Header().Get("Location"))
	}
	assertHidden(request("/start"))
	assertHidden(request("/start?access_token=" + url.QueryEscape(token+"x")))
	assertHidden(request("/callback?code=private&state=private"))
	start := request("/start?payment_type=wxpay&access_token=" + url.QueryEscape(token))
	require.Equal(t, http.StatusFound, start.Code)
	require.Contains(t, start.Header().Get("Location"), "open.weixin.qq.com")
	accessCookie := findCookie(start.Result().Cookies(), wechatPaymentOAuthAccess)
	require.NotNil(t, accessCookie)
	require.True(t, accessCookie.HttpOnly)
	require.Equal(t, "no-referrer", start.Header().Get("Referrer-Policy"))
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	callbackPath := "/callback?code=valid&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
	callback := request(callbackPath, start.Result().Cookies()...)
	require.Equal(t, http.StatusFound, callback.Code)
	redirect, err := url.Parse(callback.Header().Get("Location"))
	require.NoError(t, err)
	fragment, err := url.ParseQuery(redirect.Fragment)
	require.NoError(t, err)
	resume, err := h.wechatPaymentResumeService().ParseWeChatPaymentResumeToken(fragment.Get("wechat_resume_token"))
	require.NoError(t, err)
	require.Equal(t, "openid-authorized", resume.OpenID)
	require.Equal(t, 1, exchanges)
	require.NoError(t, client.User.UpdateOneID(u.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	assertHidden(request("/start?payment_type=wxpay&access_token=" + url.QueryEscape(token)))
	// A valid browser handoff issued before revocation cannot exchange a code.
	assertHidden(request("/callback?code=private&state=private", accessCookie))
	require.Equal(t, 1, exchanges)
	require.NoError(t, client.User.UpdateOneID(u.ID).SetExclusiveRechargeEnabled(true).SetStatus("disabled").Exec(ctx))
	assertHidden(request("/start?payment_type=wxpay&access_token=" + url.QueryEscape(token)))
}
