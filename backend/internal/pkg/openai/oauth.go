package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// OpenAI OAuth Constants (from CRS project - Codex CLI client)
const (
	// OAuth Client ID for OpenAI (Codex CLI official)
	ClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// OAuth endpoints
	AuthorizeURL = "https://auth.openai.com/oauth/authorize"
	TokenURL     = "https://auth.openai.com/oauth/token"

	// Default redirect URI (can be customized)
	DefaultRedirectURI = "http://localhost:1455/auth/callback"

	// Scopes
	DefaultScopes = "openid profile email offline_access"
	// RefreshScopes - scope for token refresh (without offline_access, aligned with CRS project)
	RefreshScopes = "openid profile email"

	// Session TTL
	SessionTTL = 30 * time.Minute
)

const (
	// OAuthPlatformOpenAI uses OpenAI Codex-compatible OAuth client.
	OAuthPlatformOpenAI = "openai"
)

// TokenResponse represents the token response from OpenAI OAuth
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// RefreshTokenRequest represents the refresh token request
type RefreshTokenRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	Scope        string `json:"scope"`
}

// IDTokenClaims represents the claims from OpenAI ID Token
type IDTokenClaims struct {
	// Standard claims
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Iss           string   `json:"iss"`
	Aud           []string `json:"aud"` // OpenAI returns aud as an array
	Exp           int64    `json:"exp"`
	Iat           int64    `json:"iat"`

	// OpenAI specific claims (nested under https://api.openai.com/auth)
	OpenAIAuth *OpenAIAuthClaims `json:"https://api.openai.com/auth,omitempty"`
}

// OpenAIAuthClaims represents the OpenAI specific auth claims
type OpenAIAuthClaims struct {
	ChatGPTAccountID string              `json:"chatgpt_account_id"`
	ChatGPTUserID    string              `json:"chatgpt_user_id"`
	ChatGPTPlanType  string              `json:"chatgpt_plan_type"`
	UserID           string              `json:"user_id"`
	POID             string              `json:"poid"` // organization ID in access_token JWT
	Organizations    []OrganizationClaim `json:"organizations"`
}

// OrganizationClaim represents an organization in the ID Token
type OrganizationClaim struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Title     string `json:"title"`
	IsDefault bool   `json:"is_default"`
}

// BuildRefreshTokenRequest creates a refresh token request for OpenAI
func BuildRefreshTokenRequest(refreshToken string) *RefreshTokenRequest {
	return &RefreshTokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
		ClientID:     ClientID,
		Scope:        RefreshScopes,
	}
}

// ToFormData converts RefreshTokenRequest to URL-encoded form data
func (r *RefreshTokenRequest) ToFormData() string {
	params := url.Values{}
	params.Set("grant_type", r.GrantType)
	params.Set("client_id", r.ClientID)
	params.Set("refresh_token", r.RefreshToken)
	params.Set("scope", r.Scope)
	return params.Encode()
}

// DecodeIDToken decodes the ID Token JWT payload without validating expiration.
// Use this for best-effort extraction (e.g., during data import) where the token may be expired.
func DecodeIDToken(idToken string) (*IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (second part)
	payload := parts[1]
	// Add padding if necessary
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try standard encoding
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
		}
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &claims, nil
}

// ParseIDToken parses the ID Token JWT and extracts claims.
// 注意：当前仅解码 payload 并校验 exp，未验证 JWT 签名。
// 生产环境如需用 ID Token 做授权决策，应通过 OpenAI 的 JWKS 端点验证签名：
//
//	https://auth.openai.com/.well-known/jwks.json
func ParseIDToken(idToken string) (*IDTokenClaims, error) {
	claims, err := DecodeIDToken(idToken)
	if err != nil {
		return nil, err
	}

	// 校验 ID Token 是否已过期（允许 2 分钟时钟偏差，防止因服务器时钟略有差异误判刚颁发的令牌）
	const clockSkewTolerance = 120 // 秒
	now := time.Now().Unix()
	if claims.Exp > 0 && now > claims.Exp+clockSkewTolerance {
		return nil, fmt.Errorf("id_token has expired (exp: %d, now: %d, skew_tolerance: %ds)", claims.Exp, now, clockSkewTolerance)
	}

	return claims, nil
}

// UserInfo represents user information extracted from ID Token claims.
type UserInfo struct {
	Email            string
	ChatGPTAccountID string
	ChatGPTUserID    string
	PlanType         string
	UserID           string
	OrganizationID   string
	Organizations    []OrganizationClaim
}

// GetUserInfo extracts user info from ID Token claims
func (c *IDTokenClaims) GetUserInfo() *UserInfo {
	info := &UserInfo{
		Email: c.Email,
	}

	if c.OpenAIAuth != nil {
		info.ChatGPTAccountID = c.OpenAIAuth.ChatGPTAccountID
		info.ChatGPTUserID = c.OpenAIAuth.ChatGPTUserID
		info.PlanType = c.OpenAIAuth.ChatGPTPlanType
		info.UserID = c.OpenAIAuth.UserID
		info.Organizations = c.OpenAIAuth.Organizations

		// Get default organization ID
		for _, org := range c.OpenAIAuth.Organizations {
			if org.IsDefault {
				info.OrganizationID = org.ID
				break
			}
		}
		// If no default, use first org
		if info.OrganizationID == "" && len(c.OpenAIAuth.Organizations) > 0 {
			info.OrganizationID = c.OpenAIAuth.Organizations[0].ID
		}
	}

	return info
}
