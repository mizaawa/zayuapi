//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failoverAuthStateRepo struct {
	*stubApiKeyRepo
	state *service.APIKey
}

func (r *failoverAuthStateRepo) GetAPIKeyFailover(context.Context, int64) (*service.APIKey, error) {
	clone := *r.state
	return &clone, nil
}

func (r *failoverAuthStateRepo) StartAPIKeyFailoverCooldown(context.Context, *service.APIKey, time.Time) (*time.Time, error) {
	return nil, service.ErrAPIKeyFailoverChanged
}

func TestAPIKeyAuthFailoverPreservesLocalKeyRejections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		status string
		code   int
		error  string
	}{
		{service.StatusAPIKeyQuotaExhausted, http.StatusTooManyRequests, "insufficient_quota"},
		{service.StatusAPIKeyExpired, http.StatusForbidden, "API_KEY_EXPIRED"},
		{service.StatusAPIKeyDisabled, http.StatusUnauthorized, "API_KEY_DISABLED"},
	} {
		for _, stale := range []bool{false, true} {
			name := test.status
			if stale {
				name += "/stale_auth_snapshot"
			}
			t.Run(name, func(t *testing.T) {
				user := &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}
				group := &service.Group{ID: 10, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
				fallbackID := int64(20)
				until := time.Now().Add(time.Minute)
				state := &service.APIKey{
					ID: 1, UserID: user.ID, Key: "sk-local-failover-status", Status: test.status,
					User: user, Group: group, GroupID: &group.ID,
					FailoverEnabled: true, FailoverGroupID: &fallbackID, FailoverCooldownUntil: &until,
					FailoverMaxRetries: 3, FailoverCooldownSeconds: 300,
				}
				cached := *state
				if stale {
					cached.Status = service.StatusActive
				}
				repo := &failoverAuthStateRepo{
					state: state,
					stubApiKeyRepo: &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
						clone := cached
						return &clone, nil
					}},
				}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
				router := gin.New()
				router.POST("/v1/responses", gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)), func(c *gin.Context) {
					t.Error("locally rejected key reached the upstream handler")
					c.Status(http.StatusOK)
				})
				request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test"}`))
				request.Header.Set("x-api-key", state.Key)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)

				require.Equal(t, test.code, response.Code)
				require.Contains(t, response.Body.String(), test.error)
				require.NotContains(t, response.Body.String(), "API_KEY_FAILOVER_UNAVAILABLE")
			})
		}
	}
}
