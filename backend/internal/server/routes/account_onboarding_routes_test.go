package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
)

func TestAccountOnboardingLegacyRoutesRemoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &admin.AccountHandler{}, OpenAIOAuth: &admin.OpenAIOAuthHandler{}, GrokOAuth: &admin.GrokOAuthHandler{},
	}}
	group := router.Group("/api/v1/admin")
	registerAccountRoutes(group, h, func(c *gin.Context) { c.Next() })
	registerOpenAIOAuthRoutes(group, h)
	registerGrokOAuthRoutes(group, h)
	paths := []string{
		"/accounts/import/codex-session", "/accounts/sync/crs", "/accounts/sync/crs/preview",
		"/accounts/1/apply-oauth-credentials", "/accounts/1/shadow", "/accounts/generate-auth-url",
		"/accounts/generate-setup-token-url", "/accounts/exchange-code", "/accounts/exchange-setup-token-code",
		"/accounts/cookie-auth", "/accounts/setup-token-cookie-auth",
		"/openai/generate-auth-url", "/openai/exchange-code", "/openai/refresh-token", "/openai/create-from-oauth", "/openai/create-from-codex-pat",
		"/gemini/oauth/auth-url", "/gemini/oauth/exchange-code", "/antigravity/oauth/auth-url", "/antigravity/oauth/exchange-code", "/antigravity/oauth/refresh-token",
		"/grok/oauth/auth-url", "/grok/oauth/exchange-code", "/grok/oauth/refresh-token", "/grok/oauth/create-from-oauth", "/grok/sso-to-oauth",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/admin"+path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("removed onboarding endpoint returned %d", w.Code)
			}
		})
	}
}
