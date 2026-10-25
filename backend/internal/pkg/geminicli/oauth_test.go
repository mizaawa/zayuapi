package geminicli

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 现存账户刷新配置测试
// ---------------------------------------------------------------------------

func TestHasRestrictedScope(t *testing.T) {
	tests := []struct {
		scope    string
		expected bool
	}{
		// 受限 scope
		{"https://www.googleapis.com/auth/generative-language", true},
		{"https://www.googleapis.com/auth/generative-language.retriever", true},
		{"https://www.googleapis.com/auth/generative-language.tuning", true},
		{"https://www.googleapis.com/auth/drive", true},
		{"https://www.googleapis.com/auth/drive.readonly", true},
		{"https://www.googleapis.com/auth/drive.file", true},
		// 非受限 scope
		{"https://www.googleapis.com/auth/cloud-platform", false},
		{"https://www.googleapis.com/auth/userinfo.email", false},
		{"https://www.googleapis.com/auth/userinfo.profile", false},
		// 边界情况
		{"", false},
		{"random-scope", false},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := hasRestrictedScope(tt.scope)
			if got != tt.expected {
				t.Errorf("hasRestrictedScope(%q) = %v，期望 %v", tt.scope, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildAuthorizationURL 测试
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// EffectiveOAuthConfig 测试 - 原有测试
// ---------------------------------------------------------------------------

func TestEffectiveOAuthConfig_GoogleOne(t *testing.T) {
	// 内置的 Gemini CLI client secret 不嵌入在此仓库中。
	// 测试通过环境变量设置一个假的 secret 来模拟运维配置。
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	tests := []struct {
		name         string
		input        OAuthConfig
		oauthType    string
		wantClientID string
		wantScopes   string
		wantErr      bool
	}{
		{
			name:         "Google One 使用内置客户端（空配置）",
			input:        OAuthConfig{},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name: "Google One 使用自定义客户端（传入自定义凭据时使用自定义）",
			input: OAuthConfig{
				ClientID:     "custom-client-id",
				ClientSecret: "custom-client-secret",
			},
			oauthType:    "google_one",
			wantClientID: "custom-client-id",
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name: "Google One 内置客户端 + 自定义 scopes（应过滤受限 scopes）",
			input: OAuthConfig{
				Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
			},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   "https://www.googleapis.com/auth/cloud-platform",
			wantErr:      false,
		},
		{
			name: "Google One 内置客户端 + 仅受限 scopes（应回退到默认）",
			input: OAuthConfig{
				Scopes: "https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
			},
			oauthType:    "google_one",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
		{
			name:         "Code Assist 使用内置客户端",
			input:        OAuthConfig{},
			oauthType:    "code_assist",
			wantClientID: GeminiCLIOAuthClientID,
			wantScopes:   DefaultCodeAssistScopes,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EffectiveOAuthConfig(tt.input, tt.oauthType)
			if (err != nil) != tt.wantErr {
				t.Errorf("EffectiveOAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.ClientID != tt.wantClientID {
				t.Errorf("EffectiveOAuthConfig() ClientID = %v, want %v", got.ClientID, tt.wantClientID)
			}
			if got.Scopes != tt.wantScopes {
				t.Errorf("EffectiveOAuthConfig() Scopes = %v, want %v", got.Scopes, tt.wantScopes)
			}
		})
	}
}

func TestEffectiveOAuthConfig_ScopeFiltering(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// 测试 Google One + 内置客户端过滤受限 scopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly https://www.googleapis.com/auth/userinfo.profile",
	}, "google_one")

	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}

	// 应仅包含 cloud-platform、userinfo.email 和 userinfo.profile
	// 不应包含 generative-language 或 drive scopes
	if strings.Contains(cfg.Scopes, "generative-language") {
		t.Errorf("使用内置客户端时 Scopes 不应包含 generative-language，实际: %v", cfg.Scopes)
	}
	if strings.Contains(cfg.Scopes, "drive") {
		t.Errorf("使用内置客户端时 Scopes 不应包含 drive，实际: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("Scopes 应包含 cloud-platform，实际: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.email") {
		t.Errorf("Scopes 应包含 userinfo.email，实际: %v", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.profile") {
		t.Errorf("Scopes 应包含 userinfo.profile，实际: %v", cfg.Scopes)
	}
}

// ---------------------------------------------------------------------------
// EffectiveOAuthConfig 测试 - 新增分支覆盖
// ---------------------------------------------------------------------------

func TestEffectiveOAuthConfig_OnlyClientID_NoSecret(t *testing.T) {
	// 只提供 clientID 不提供 secret 应报错
	_, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID: "some-client-id",
	}, "code_assist")
	if err == nil {
		t.Error("只提供 ClientID 不提供 ClientSecret 应该报错")
	}
	if !strings.Contains(err.Error(), "client_id") || !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("错误消息应提及 client_id 和 client_secret，实际: %v", err)
	}
}

func TestEffectiveOAuthConfig_OnlyClientSecret_NoID(t *testing.T) {
	// 只提供 secret 不提供 clientID 应报错
	_, err := EffectiveOAuthConfig(OAuthConfig{
		ClientSecret: "some-client-secret",
	}, "code_assist")
	if err == nil {
		t.Error("只提供 ClientSecret 不提供 ClientID 应该报错")
	}
	if !strings.Contains(err.Error(), "client_id") || !strings.Contains(err.Error(), "client_secret") {
		t.Errorf("错误消息应提及 client_id 和 client_secret，实际: %v", err)
	}
}

func TestEffectiveOAuthConfig_AIStudio_DefaultScopes_BuiltinClient(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// ai_studio 类型，使用内置客户端，scopes 为空 -> 应使用 DefaultCodeAssistScopes（因为内置客户端不能请求 generative-language scope）
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("ai_studio + 内置客户端应使用 DefaultCodeAssistScopes，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_AIStudio_DefaultScopes_CustomClient(t *testing.T) {
	// ai_studio 类型，使用自定义客户端，scopes 为空 -> 应使用 DefaultAIStudioScopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultAIStudioScopes {
		t.Errorf("ai_studio + 自定义客户端应使用 DefaultAIStudioScopes，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_AIStudio_ScopeNormalization(t *testing.T) {
	// ai_studio 类型，旧的 generative-language scope 应被归一化为 generative-language.retriever
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/generative-language https://www.googleapis.com/auth/cloud-platform",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if strings.Contains(cfg.Scopes, "auth/generative-language ") || strings.HasSuffix(cfg.Scopes, "auth/generative-language") {
		// 确保不包含未归一化的旧 scope（仅 generative-language 而非 generative-language.retriever）
		parts := strings.Fields(cfg.Scopes)
		for _, p := range parts {
			if p == "https://www.googleapis.com/auth/generative-language" {
				t.Errorf("ai_studio 应将 generative-language 归一化为 generative-language.retriever，实际 scopes: %q", cfg.Scopes)
			}
		}
	}
	if !strings.Contains(cfg.Scopes, "generative-language.retriever") {
		t.Errorf("ai_studio 归一化后应包含 generative-language.retriever，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_CommaSeparatedScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// 逗号分隔的 scopes 应被归一化为空格分隔
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/userinfo.email",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	// 应该用空格分隔，而非逗号
	if strings.Contains(cfg.Scopes, ",") {
		t.Errorf("逗号分隔的 scopes 应被归一化为空格分隔，实际: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("归一化后应包含 cloud-platform，实际: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "userinfo.email") {
		t.Errorf("归一化后应包含 userinfo.email，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_MixedCommaAndSpaceScopes(t *testing.T) {
	// 混合逗号和空格分隔的 scopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/cloud-platform, https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	parts := strings.Fields(cfg.Scopes)
	if len(parts) != 3 {
		t.Errorf("归一化后应有 3 个 scope，实际: %d，scopes: %q", len(parts), cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_WhitespaceTriming(t *testing.T) {
	// 输入中的前后空白应被清理
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "  custom-id  ",
		ClientSecret: "  custom-secret  ",
		Scopes:       "  https://www.googleapis.com/auth/cloud-platform  ",
	}, "code_assist")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.ClientID != "custom-id" {
		t.Errorf("ClientID 应去除前后空白，实际: %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "custom-secret" {
		t.Errorf("ClientSecret 应去除前后空白，实际: %q", cfg.ClientSecret)
	}
	if cfg.Scopes != "https://www.googleapis.com/auth/cloud-platform" {
		t.Errorf("Scopes 应去除前后空白，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_NoEnvSecret(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "")

	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "code_assist")
	if err != nil {
		t.Fatalf("不设置环境变量时应回退到内置 secret，实际报错: %v", err)
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		t.Error("ClientSecret 不应为空")
	}
	if cfg.ClientID != GeminiCLIOAuthClientID {
		t.Errorf("ClientID 应回退为内置客户端 ID，实际: %q", cfg.ClientID)
	}
}

func TestEffectiveOAuthConfig_AIStudio_BuiltinClient_CustomScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// ai_studio + 内置客户端 + 自定义 scopes -> 应过滤受限 scopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		Scopes: "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever",
	}, "ai_studio")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	// 内置客户端应过滤 generative-language.retriever
	if strings.Contains(cfg.Scopes, "generative-language") {
		t.Errorf("ai_studio + 内置客户端应过滤受限 scopes，实际: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "cloud-platform") {
		t.Errorf("应保留 cloud-platform scope，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_UnknownOAuthType_DefaultScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// 未知的 oauthType 应回退到默认的 code_assist scopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "unknown_type")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("未知 oauthType 应使用 DefaultCodeAssistScopes，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_EmptyOAuthType_DefaultScopes(t *testing.T) {
	t.Setenv(GeminiCLIOAuthClientSecretEnv, "test-built-in-secret")

	// 空的 oauthType 应走 default 分支，使用 DefaultCodeAssistScopes
	cfg, err := EffectiveOAuthConfig(OAuthConfig{}, "")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	if cfg.Scopes != DefaultCodeAssistScopes {
		t.Errorf("空 oauthType 应使用 DefaultCodeAssistScopes，实际: %q", cfg.Scopes)
	}
}

func TestEffectiveOAuthConfig_CustomClient_NoScopeFiltering(t *testing.T) {
	// 自定义客户端 + google_one + 包含受限 scopes -> 不应被过滤（因为不是内置客户端）
	cfg, err := EffectiveOAuthConfig(OAuthConfig{
		ClientID:     "custom-id",
		ClientSecret: "custom-secret",
		Scopes:       "https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive.readonly",
	}, "google_one")
	if err != nil {
		t.Fatalf("EffectiveOAuthConfig() error = %v", err)
	}
	// 自定义客户端不应过滤任何 scope
	if !strings.Contains(cfg.Scopes, "generative-language.retriever") {
		t.Errorf("自定义客户端不应过滤 generative-language.retriever，实际: %q", cfg.Scopes)
	}
	if !strings.Contains(cfg.Scopes, "drive.readonly") {
		t.Errorf("自定义客户端不应过滤 drive.readonly，实际: %q", cfg.Scopes)
	}
}
