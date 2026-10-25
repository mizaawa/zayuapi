package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

type AntigravityOAuthService struct {
	proxyRepo ProxyRepository
}

func NewAntigravityOAuthService(proxyRepo ProxyRepository) *AntigravityOAuthService {
	return &AntigravityOAuthService{
		proxyRepo: proxyRepo,
	}
}

// AntigravityTokenInfo token 信息
type AntigravityTokenInfo struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	ExpiresAt        int64  `json:"expires_at"`
	TokenType        string `json:"token_type"`
	Email            string `json:"email,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	ProjectIDMissing bool   `json:"-"`
	PlanType         string `json:"-"`
	PrivacyMode      string `json:"-"`
}

// RefreshToken 刷新 token
func (s *AntigravityOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*AntigravityTokenInfo, error) {
	var lastErr error

	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)
		}

		client, err := antigravity.NewClient(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("create antigravity client failed: %w", err)
		}
		tokenResp, err := client.RefreshToken(ctx, refreshToken)
		if err == nil {
			now := time.Now()
			expiresAt := now.Unix() + tokenResp.ExpiresIn - 300
			fmt.Printf("[AntigravityOAuth] Token refreshed: expires_in=%d, expires_at=%d (%s)\n",
				tokenResp.ExpiresIn, expiresAt, time.Unix(expiresAt, 0).Format("2006-01-02 15:04:05"))
			return &AntigravityTokenInfo{
				AccessToken:  tokenResp.AccessToken,
				RefreshToken: tokenResp.RefreshToken,
				ExpiresIn:    tokenResp.ExpiresIn,
				ExpiresAt:    expiresAt,
				TokenType:    tokenResp.TokenType,
			}, nil
		}

		if isNonRetryableAntigravityOAuthError(err) {
			return nil, err
		}
		// 代理连接错误（TCP 超时、连接拒绝、DNS 失败）不重试，立即返回
		if antigravity.IsConnectionError(err) {
			return nil, fmt.Errorf("proxy unavailable: %w", err)
		}
		lastErr = err
	}

	return nil, fmt.Errorf("token 刷新失败 (重试后): %w", lastErr)
}

func isNonRetryableAntigravityOAuthError(err error) bool {
	msg := err.Error()
	nonRetryable := []string{
		"invalid_grant",
		"invalid_client",
		"unauthorized_client",
		"access_denied",
	}
	for _, needle := range nonRetryable {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// RefreshAccountToken 刷新账户的 token
func (s *AntigravityOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*AntigravityTokenInfo, error) {
	if account.Platform != PlatformAntigravity || account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("非 Antigravity OAuth 账户")
	}

	refreshToken := account.GetCredential("refresh_token")
	if strings.TrimSpace(refreshToken) == "" {
		return nil, fmt.Errorf("无可用的 refresh_token")
	}

	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	tokenInfo, err := s.RefreshToken(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
	}

	// 保留原有的 email
	existingEmail := strings.TrimSpace(account.GetCredential("email"))
	if existingEmail != "" {
		tokenInfo.Email = existingEmail
	}

	// 每次刷新都调用 LoadCodeAssist 获取 project_id + plan_type，失败时重试
	existingProjectID := strings.TrimSpace(account.GetCredential("project_id"))
	loadResult, loadErr := s.loadProjectIDWithRetry(ctx, tokenInfo.AccessToken, proxyURL, 3)

	if loadErr != nil {
		tokenInfo.ProjectID = existingProjectID
		if existingProjectID == "" {
			tokenInfo.ProjectIDMissing = true
		}
	}
	if loadResult != nil {
		if loadResult.ProjectID != "" {
			tokenInfo.ProjectID = loadResult.ProjectID
		}
		if loadResult.Subscription != nil {
			tokenInfo.PlanType = loadResult.Subscription.PlanType
		}
	}

	return tokenInfo, nil
}

// loadCodeAssistResult 封装 loadProjectIDWithRetry 的返回结果，
// 同时携带从 LoadCodeAssist 响应中提取的 plan_type 信息。
type loadCodeAssistResult struct {
	ProjectID    string
	Subscription *AntigravitySubscriptionResult
}

// loadProjectIDWithRetry 带重试机制获取 project_id，同时从响应中提取 plan_type。
func (s *AntigravityOAuthService) loadProjectIDWithRetry(ctx context.Context, accessToken, proxyURL string, maxRetries int) (*loadCodeAssistResult, error) {
	var lastErr error
	var lastSubscription *AntigravitySubscriptionResult

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 8*time.Second {
				backoff = 8 * time.Second
			}
			time.Sleep(backoff)
		}

		client, err := antigravity.NewClient(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("create antigravity client failed: %w", err)
		}
		loadResp, loadRaw, err := client.LoadCodeAssist(ctx, accessToken)

		if loadResp != nil {
			sub := NormalizeAntigravitySubscription(loadResp)
			lastSubscription = &sub
		}

		if err == nil && loadResp != nil && loadResp.CloudAICompanionProject != "" {
			return &loadCodeAssistResult{
				ProjectID:    loadResp.CloudAICompanionProject,
				Subscription: lastSubscription,
			}, nil
		}

		if err == nil {
			if projectID, onboardErr := tryOnboardProjectID(ctx, client, accessToken, loadRaw); onboardErr == nil && projectID != "" {
				return &loadCodeAssistResult{
					ProjectID:    projectID,
					Subscription: lastSubscription,
				}, nil
			} else if onboardErr != nil {
				lastErr = onboardErr
				continue
			}
		}

		if err != nil {
			lastErr = err
		} else if loadResp == nil {
			lastErr = fmt.Errorf("LoadCodeAssist 返回空响应")
		} else {
			lastErr = fmt.Errorf("LoadCodeAssist 返回空 project_id")
		}
	}

	if lastSubscription != nil {
		return &loadCodeAssistResult{Subscription: lastSubscription}, fmt.Errorf("获取 project_id 失败 (重试 %d 次后): %w", maxRetries, lastErr)
	}
	return nil, fmt.Errorf("获取 project_id 失败 (重试 %d 次后): %w", maxRetries, lastErr)
}

func tryOnboardProjectID(ctx context.Context, client *antigravity.Client, accessToken string, loadRaw map[string]any) (string, error) {
	tierID := resolveDefaultTierID(loadRaw)
	if tierID == "" {
		return "", fmt.Errorf("loadCodeAssist 未返回可用的默认 tier")
	}

	projectID, err := client.OnboardUser(ctx, accessToken, tierID)
	if err != nil {
		return "", fmt.Errorf("onboardUser 失败 (tier=%s): %w", tierID, err)
	}
	return projectID, nil
}

func resolveDefaultTierID(loadRaw map[string]any) string {
	if len(loadRaw) == 0 {
		return ""
	}

	rawTiers, ok := loadRaw["allowedTiers"]
	if !ok {
		return ""
	}

	tiers, ok := rawTiers.([]any)
	if !ok {
		return ""
	}

	for _, rawTier := range tiers {
		tier, ok := rawTier.(map[string]any)
		if !ok {
			continue
		}
		if isDefault, _ := tier["isDefault"].(bool); !isDefault {
			continue
		}
		if id, ok := tier["id"].(string); ok {
			id = strings.TrimSpace(id)
			if id != "" {
				return id
			}
		}
	}

	return ""
}

// FillProjectID 仅获取 project_id，不刷新 OAuth token
func (s *AntigravityOAuthService) FillProjectID(ctx context.Context, account *Account, accessToken string) (string, error) {
	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}
	result, err := s.loadProjectIDWithRetry(ctx, accessToken, proxyURL, 3)
	if result != nil {
		return result.ProjectID, err
	}
	return "", err
}

// BuildAccountCredentials 构建账户凭证
func (s *AntigravityOAuthService) BuildAccountCredentials(tokenInfo *AntigravityTokenInfo) map[string]any {
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   strconv.FormatInt(tokenInfo.ExpiresAt, 10),
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.TokenType != "" {
		creds["token_type"] = tokenInfo.TokenType
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.ProjectID != "" {
		creds["project_id"] = tokenInfo.ProjectID
	}
	if tokenInfo.PlanType != "" {
		creds["plan_type"] = tokenInfo.PlanType
	}
	return creds
}
