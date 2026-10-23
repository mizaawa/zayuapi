//go:build unit

package service

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestWeChatPaymentAccessTokenPurposeSignatureAndExpiry(t *testing.T) {
	s := NewPaymentResumeService([]byte("0123456789abcdef0123456789abcdef"))
	token, err := s.CreateWeChatPaymentAccessToken(42)
	require.NoError(t, err)
	claims, err := s.ParseWeChatPaymentAccessToken(token)
	require.NoError(t, err)
	require.EqualValues(t, 42, claims.UserID)
	require.EqualValues(t, 600, claims.ExpiresAt-claims.IssuedAt)
	_, err = s.ParseWeChatPaymentAccessToken(token + "x")
	require.ErrorIs(t, err, ErrPaymentAccessDenied)
	_, err = s.ParseToken(token)
	require.Error(t, err)
	now := time.Now().Unix()
	for _, invalid := range []WeChatPaymentAccessClaims{
		{TokenType: wechatPaymentAccessTokenType, UserID: 42, IssuedAt: now - 700, ExpiresAt: now - 1},
		{TokenType: wechatPaymentAccessTokenType, UserID: 42, IssuedAt: now, ExpiresAt: 0},
		{TokenType: wechatPaymentAccessTokenType, UserID: 42, IssuedAt: now, ExpiresAt: now + 601},
		{TokenType: wechatPaymentResumeTokenType, UserID: 42, IssuedAt: now, ExpiresAt: now + 60},
		{TokenType: wechatPaymentAccessTokenType, UserID: 0, IssuedAt: now, ExpiresAt: now + 60},
	} {
		token, err := s.createSignedToken(invalid)
		require.NoError(t, err)
		_, err = s.ParseWeChatPaymentAccessToken(token)
		require.ErrorIs(t, err, ErrPaymentAccessDenied)
	}
}

func TestWeChatOAuthRequiredResponseIncludesUserBoundAccess(t *testing.T) {
	t.Setenv("PAYMENT_RESUME_SIGNING_KEY", "0123456789abcdef0123456789abcdef")
	s := newWeChatPaymentOAuthTestService(map[string]string{
		SettingKeyWeChatConnectEnabled: "true", SettingKeyWeChatConnectMode: "mp",
		SettingKeyWeChatConnectAppID: "wx123456", SettingKeyWeChatConnectAppSecret: "secret",
	})
	resp, err := s.buildWeChatOAuthRequiredResponse(context.Background(), CreateOrderRequest{
		UserID: 42, PaymentType: payment.TypeWxpay, Amount: 10,
	}, 10, 10, 0)
	require.NoError(t, err)
	parsed, err := url.Parse(resp.OAuth.AuthorizeURL)
	require.NoError(t, err)
	claims, err := s.paymentResume().ParseWeChatPaymentAccessToken(parsed.Query().Get("access_token"))
	require.NoError(t, err)
	require.EqualValues(t, 42, claims.UserID)
}
