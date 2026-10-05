//go:build unit

package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthManagedMonitorRequiresAttestationAndBypassesCreatorBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, cfg, svc, attestor := managedMonitorAuthFixture(t, service.PlatformAnthropic)
	body := []byte(`{"model":"claude-test","messages":[]}`)

	request := func(sign bool, corrupt bool) (*httptest.ResponseRecorder, AuthSubject, []byte) {
		var subject AuthSubject
		var gotBody []byte
		router := gin.New()
		router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		router.POST("/v1/messages", func(c *gin.Context) {
			require.Empty(t, c.GetHeader(service.ChannelMonitorTimestampHeader))
			require.Empty(t, c.GetHeader(service.ChannelMonitorSignatureHeader))
			subject, _ = GetAuthSubjectFromContext(c)
			gotBody, _ = io.ReadAll(c.Request.Body)
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+key.Key)
		if sign {
			require.NoError(t, attestor.SignRequest(req, key.Key, body))
		}
		if corrupt {
			req.Header.Set(service.ChannelMonitorSignatureHeader, "invalid")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w, subject, gotBody
	}

	for _, tc := range []struct {
		name    string
		sign    bool
		corrupt bool
	}{
		{name: "missing"},
		{name: "invalid", sign: true, corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _, _ := request(tc.sign, tc.corrupt)
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}

	w, subject, gotBody := request(true, false)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, key.User.ID, subject.UserID)
	require.Zero(t, subject.Concurrency, "managed probes must bypass creator user concurrency")
	require.Equal(t, body, gotBody, "attestation must restore the body for the gateway handler")
}

func TestGoogleAPIKeyAuthManagedMonitorRequiresAttestationAndBypassesCreatorBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key, cfg, svc, attestor := managedMonitorAuthFixture(t, service.PlatformGemini)
	body := []byte(`{"contents":[{"parts":[{"text":"ping"}]}]}`)

	run := func(sign bool) (*httptest.ResponseRecorder, AuthSubject) {
		var subject AuthSubject
		router := gin.New()
		router.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
		router.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
			require.Empty(t, c.GetHeader(service.ChannelMonitorTimestampHeader))
			require.Empty(t, c.GetHeader(service.ChannelMonitorSignatureHeader))
			subject, _ = GetAuthSubjectFromContext(c)
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:generateContent", strings.NewReader(string(body)))
		req.Header.Set("x-goog-api-key", key.Key)
		if sign {
			require.NoError(t, attestor.SignRequest(req, key.Key, body))
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w, subject
	}

	w, _ := run(false)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	w, subject := run(true)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, key.User.ID, subject.UserID)
	require.Zero(t, subject.Concurrency)
}

func TestManagedMonitorAuthRejectsNonAdminOwnerAndSpoofedOrdinaryKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, scenario := range []string{"demoted owner", "owner mismatch", "ordinary key with monitor name and signature"} {
			t.Run(fmt.Sprintf("google=%t/%s", google, scenario), func(t *testing.T) {
				key, cfg, svc, attestor := managedMonitorAuthFixture(t, service.PlatformGemini)
				wantStatus := http.StatusUnauthorized
				switch scenario {
				case "demoted owner":
					key.User.Role = service.RoleUser
				case "owner mismatch":
					key.UserID++
				case "ordinary key with monitor name and signature":
					key.Purpose = ""
					key.Name = "[channel-monitor] spoofed"
					key.Group.SubscriptionType = service.SubscriptionTypeStandard
					key.Group.IsExclusive = false
					key.User.BlockedGroups = nil
					wantStatus = http.StatusForbidden
				}
				router := gin.New()
				if google {
					router.Use(APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg))
				} else {
					router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
				}
				router.POST("/v1/messages", func(c *gin.Context) {
					t.Error("untrusted request reached gateway handler")
					c.Status(http.StatusOK)
				})
				body := []byte(`{"model":"test"}`)
				req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
				req.Header.Set("Authorization", "Bearer "+key.Key)
				require.NoError(t, attestor.SignRequest(req, key.Key, body))
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				require.Equal(t, wantStatus, w.Code, w.Body.String())
			})
		}
	}
}

func TestAPIKeyAuthManagedCustomStillRejectsInactiveKeyAndGroup(t *testing.T) {
	body := []byte(`{"model":"custom-model","messages":[]}`)
	tests := []struct {
		name       string
		mutate     func(*service.APIKey)
		wantStatus int
	}{
		{
			name: "disabled key",
			mutate: func(key *service.APIKey) {
				key.Status = service.StatusAPIKeyDisabled
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "quota exhausted key",
			mutate: func(key *service.APIKey) {
				key.Status = service.StatusAPIKeyQuotaExhausted
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "disabled group",
			mutate: func(key *service.APIKey) {
				key.Group.Status = service.StatusDisabled
			},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, cfg, svc, attestor := managedMonitorAuthFixture(t, service.PlatformComposite)
			tt.mutate(key)

			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
			router.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+key.Key)
			require.NoError(t, attestor.SignRequest(req, key.Key, body))

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func managedMonitorAuthFixture(t *testing.T, platform string) (*service.APIKey, *config.Config, *service.APIKeyService, *service.ChannelMonitorAttestor) {
	t.Helper()
	group := &service.Group{
		ID:               42,
		Name:             "exclusive-subscription",
		Platform:         platform,
		Status:           service.StatusActive,
		Hydrated:         true,
		IsExclusive:      true,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	user := &service.User{
		ID:            7,
		Role:          service.RoleAdmin,
		Status:        service.StatusActive,
		Balance:       0,
		Concurrency:   9,
		AllowedGroups: nil,
		BlockedGroups: []int64{group.ID},
	}
	key := &service.APIKey{
		ID:      100,
		UserID:  user.ID,
		Key:     "sk-managed-monitor",
		Purpose: service.APIKeyPurposeChannelMonitor,
		Status:  service.StatusActive,
		User:    user,
		Group:   group,
		GroupID: &group.ID,
	}
	repo := &stubApiKeyRepo{getByKey: func(_ context.Context, raw string) (*service.APIKey, error) {
		if raw != key.Key {
			return nil, service.ErrAPIKeyNotFound
		}
		clone := *key
		userClone := *user
		groupClone := *group
		clone.User = &userClone
		clone.Group = &groupClone
		return &clone, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Totp.EncryptionKey = strings.Repeat("42", 32)
	attestor, err := service.NewChannelMonitorAttestor(cfg.Totp.EncryptionKey)
	require.NoError(t, err)
	return key, cfg, service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg), attestor
}
