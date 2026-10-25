package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// OpenAIOAuthClient interface for OpenAI OAuth operations
type OpenAIOAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error)
	RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error)
}

// GrokOAuthClient interface for xAI/Grok OAuth operations.
type GrokOAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*xai.TokenResponse, error)
}

// GrokOAuthTokenService is the narrow refresh port used by Grok token providers.
type GrokOAuthTokenService interface {
	RefreshAccountToken(ctx context.Context, account *Account) (*GrokTokenInfo, error)
	BuildAccountCredentials(tokenInfo *GrokTokenInfo) map[string]any
}

// ClaudeOAuthClient handles HTTP requests for Claude OAuth flows
type ClaudeOAuthClient interface {
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error)
}

// OAuthService handles OAuth authentication flows
type OAuthService struct {
	proxyRepo   ProxyRepository
	oauthClient ClaudeOAuthClient
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(proxyRepo ProxyRepository, oauthClient ClaudeOAuthClient) *OAuthService {
	return &OAuthService{
		proxyRepo:   proxyRepo,
		oauthClient: oauthClient,
	}
}

// TokenInfo represents the token information stored in credentials
type TokenInfo struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	OrgUUID      string `json:"org_uuid,omitempty"`
	AccountUUID  string `json:"account_uuid,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
}

// RefreshToken refreshes an OAuth token
func (s *OAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyURL string) (*TokenInfo, error) {
	tokenResp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
	}

	return &TokenInfo{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Unix() + tokenResp.ExpiresIn,
		RefreshToken: tokenResp.RefreshToken,
		Scope:        tokenResp.Scope,
	}, nil
}

// RefreshAccountToken refreshes token for an account
func (s *OAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*TokenInfo, error) {
	refreshToken := account.GetCredential("refresh_token")
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	return s.RefreshToken(ctx, refreshToken, proxyURL)
}
