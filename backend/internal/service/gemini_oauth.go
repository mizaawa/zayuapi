package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
)

// GeminiOAuthClient refreshes credentials for existing Gemini accounts.
type GeminiOAuthClient interface {
	RefreshToken(ctx context.Context, oauthType, refreshToken, proxyURL string) (*geminicli.TokenResponse, error)
}
