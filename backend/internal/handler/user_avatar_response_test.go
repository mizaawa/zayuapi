//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const storedAvatarDataURL = "data:image/webp;base64,UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAQAcJaQAA3AA/v3AgAA="

// splitAvatarRepoStub stores users the way the real repository does: the users
// table carries no avatar and only GetUserAvatar (user_avatars) returns it, so a
// response shows the avatar only when the handler loaded it.
type splitAvatarRepoStub struct {
	*userHandlerRepoStub
	avatar *service.UserAvatar
}

func newSplitAvatarRepoStub(user *service.User) *splitAvatarRepoStub {
	stub := &splitAvatarRepoStub{userHandlerRepoStub: &userHandlerRepoStub{user: user}}
	if user.AvatarURL != "" {
		stub.avatar = &service.UserAvatar{
			StorageProvider: user.AvatarSource,
			URL:             user.AvatarURL,
			ContentType:     user.AvatarMIME,
		}
	}
	withoutAvatar(user)
	return stub
}

func withoutAvatar(user *service.User) *service.User {
	if user == nil {
		return nil
	}
	user.AvatarURL = ""
	user.AvatarSource = ""
	user.AvatarMIME = ""
	user.AvatarByteSize = 0
	user.AvatarSHA256 = ""
	return user
}

func (s *splitAvatarRepoStub) GetByID(ctx context.Context, id int64) (*service.User, error) {
	user, err := s.userHandlerRepoStub.GetByID(ctx, id)
	return withoutAvatar(user), err
}

func (s *splitAvatarRepoStub) GetByEmail(ctx context.Context, email string) (*service.User, error) {
	user, err := s.userHandlerRepoStub.GetByEmail(ctx, email)
	return withoutAvatar(user), err
}

func (s *splitAvatarRepoStub) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	if s.avatar == nil {
		return nil, nil
	}
	cloned := *s.avatar
	return &cloned, nil
}

func (s *splitAvatarRepoStub) UpsertUserAvatar(_ context.Context, _ int64, input service.UpsertUserAvatarInput) (*service.UserAvatar, error) {
	s.avatar = &service.UserAvatar{
		StorageProvider: input.StorageProvider,
		URL:             input.URL,
		ContentType:     input.ContentType,
		ByteSize:        input.ByteSize,
		SHA256:          input.SHA256,
	}
	cloned := *s.avatar
	return &cloned, nil
}

func (s *splitAvatarRepoStub) DeleteUserAvatar(context.Context, int64) error {
	s.avatar = nil
	return nil
}

func avatarTestUser(id int64) *service.User {
	return &service.User{
		ID:           id,
		Email:        "avatar-owner@example.com",
		Username:     "avatar-owner",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		AvatarURL:    storedAvatarDataURL,
		AvatarSource: "inline",
		AvatarMIME:   "image/webp",
	}
}

func decodeAvatarResponse(t *testing.T, body []byte) (avatarURL string, username string) {
	t.Helper()
	var resp struct {
		Code int `json:"code"`
		Data struct {
			AvatarURL string `json:"avatar_url"`
			Username  string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Equal(t, 0, resp.Code)
	return resp.Data.AvatarURL, resp.Data.Username
}

func TestUserHandlerUpdateProfileKeepsStoredAvatarWhenRenaming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newSplitAvatarRepoStub(avatarTestUser(41))
	handler := NewUserHandler(service.NewUserService(repo, nil, nil, nil), nil, nil, nil, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/user", bytes.NewReader([]byte(`{"username":"renamed"}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 41})

	handler.UpdateProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	avatarURL, username := decodeAvatarResponse(t, recorder.Body.Bytes())
	require.Equal(t, "renamed", username)
	require.Equal(t, storedAvatarDataURL, avatarURL)
}

func TestUserHandlerUpdateProfileKeepsStoredAvatarWhenChangingBalanceNotify(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newSplitAvatarRepoStub(avatarTestUser(42))
	handler := NewUserHandler(service.NewUserService(repo, nil, nil, nil), nil, nil, nil, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/user", bytes.NewReader([]byte(`{"balance_notify_enabled":false}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	handler.UpdateProfile(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	avatarURL, _ := decodeAvatarResponse(t, recorder.Body.Bytes())
	require.Equal(t, storedAvatarDataURL, avatarURL)
}

func TestAuthHandlerGetCurrentUserLoadsAvatarFromAvatarStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newSplitAvatarRepoStub(avatarTestUser(43))
	handler := &AuthHandler{userService: service.NewUserService(repo, nil, nil, nil)}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 43})

	handler.GetCurrentUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	avatarURL, _ := decodeAvatarResponse(t, recorder.Body.Bytes())
	require.Equal(t, storedAvatarDataURL, avatarURL)
}

func TestRespondWithTokenPairIncludesStoredAvatar(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newSplitAvatarRepoStub(avatarTestUser(44))
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "avatar-login-secret", ExpireHour: 1}}
	authService := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	loginUser, err := repo.GetByEmail(context.Background(), "avatar-owner@example.com")
	require.NoError(t, err)
	require.Empty(t, loginUser.AvatarURL, "login reads the users table, which has no avatar")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)

	respondWithTokenPair(c, authService, loginUser)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
			User        struct {
				ID        int64  `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.AccessToken)
	require.Equal(t, int64(44), resp.Data.User.ID)
	require.Equal(t, storedAvatarDataURL, resp.Data.User.AvatarURL)
}

func TestRespondWithTokenPairOmitsAvatarWhenNoneStored(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := avatarTestUser(45)
	user.AvatarURL = ""
	repo := newSplitAvatarRepoStub(user)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "avatar-login-secret", ExpireHour: 1}}
	authService := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	loginUser, err := repo.GetByID(context.Background(), 45)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)

	respondWithTokenPair(c, authService, loginUser)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Data struct {
			User map[string]any `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	_, hasAvatar := resp.Data.User["avatar_url"]
	require.False(t, hasAvatar)
}

func TestUserHandlerBindEmailIdentityKeepsStoredAvatar(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := avatarTestUser(46)
	user.Email = "legacy-avatar" + service.LinuxDoConnectSyntheticEmailDomain
	repo := newSplitAvatarRepoStub(user)
	emailCache := &userHandlerEmailCacheStub{
		data: &service.VerificationCodeData{
			Code:      "123456",
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		},
	}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "test-secret", ExpireHour: 1}}
	emailService := service.NewEmailService(nil, emailCache)
	authService := service.NewAuthService(nil, repo, nil, nil, cfg, nil, emailService, nil, nil, nil, nil, nil, nil)
	handler := NewUserHandler(service.NewUserService(repo, nil, nil, nil), authService, nil, nil, nil, nil)

	body := []byte(`{"email":"bound-avatar@example.com","verify_code":"123456","password":"new-password"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/user/account-bindings/email", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 46})

	handler.BindEmailIdentity(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	avatarURL, _ := decodeAvatarResponse(t, recorder.Body.Bytes())
	require.Equal(t, storedAvatarDataURL, avatarURL)
}
