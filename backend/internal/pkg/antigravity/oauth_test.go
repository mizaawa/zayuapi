//go:build unit

package antigravity

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// getClientSecret
// ---------------------------------------------------------------------------

func TestGetClientSecret_环境变量设置(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })
	t.Setenv(AntigravityOAuthClientSecretEnv, "my-secret-value")

	// 需要重新触发 init 逻辑：手动从环境变量读取
	defaultClientSecret = os.Getenv(AntigravityOAuthClientSecretEnv)

	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("获取 client_secret 失败: %v", err)
	}
	if secret != "my-secret-value" {
		t.Errorf("client_secret 不匹配: got %s, want my-secret-value", secret)
	}
}

func TestGetClientSecret_环境变量为空(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("defaultClientSecret 为空时应返回错误")
	}
	if !strings.Contains(err.Error(), AntigravityOAuthClientSecretEnv) {
		t.Errorf("错误信息应包含环境变量名: got %s", err.Error())
	}
}

func TestGetClientSecret_环境变量未设置(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = ""
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("defaultClientSecret 为空时应返回错误")
	}
}

func TestGetClientSecret_环境变量含空格(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "   "
	t.Cleanup(func() { defaultClientSecret = old })

	_, err := getClientSecret()
	if err == nil {
		t.Fatal("defaultClientSecret 仅含空格时应返回错误")
	}
}

func TestGetClientSecret_环境变量有前后空格(t *testing.T) {
	old := defaultClientSecret
	defaultClientSecret = "  valid-secret  "
	t.Cleanup(func() { defaultClientSecret = old })

	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("获取 client_secret 失败: %v", err)
	}
	if secret != "valid-secret" {
		t.Errorf("应去除前后空格: got %q, want %q", secret, "valid-secret")
	}
}

// ---------------------------------------------------------------------------
// ForwardBaseURLs
// ---------------------------------------------------------------------------

func TestForwardBaseURLs_Daily优先(t *testing.T) {
	urls := ForwardBaseURLs()
	if len(urls) == 0 {
		t.Fatal("ForwardBaseURLs 返回空列表")
	}

	// daily URL 应排在第一位
	if urls[0] != antigravityDailyBaseURL {
		t.Errorf("第一个 URL 应为 daily: got %s, want %s", urls[0], antigravityDailyBaseURL)
	}

	// 应包含所有 URL
	if len(urls) != len(BaseURLs) {
		t.Errorf("URL 数量不匹配: got %d, want %d", len(urls), len(BaseURLs))
	}

	// 验证 prod URL 也在列表中
	found := false
	for _, u := range urls {
		if u == antigravityProdBaseURL {
			found = true
			break
		}
	}
	if !found {
		t.Error("ForwardBaseURLs 中缺少 prod URL")
	}
}

func TestForwardBaseURLs_不修改原切片(t *testing.T) {
	originalFirst := BaseURLs[0]
	_ = ForwardBaseURLs()
	// 确保原始 BaseURLs 未被修改
	if BaseURLs[0] != originalFirst {
		t.Errorf("ForwardBaseURLs 不应修改原始 BaseURLs: got %s, want %s", BaseURLs[0], originalFirst)
	}
}

// ---------------------------------------------------------------------------
// URLAvailability
// ---------------------------------------------------------------------------

func TestNewURLAvailability(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	if ua == nil {
		t.Fatal("NewURLAvailability 返回 nil")
	}
	if ua.ttl != 5*time.Minute {
		t.Errorf("TTL 不匹配: got %v, want 5m", ua.ttl)
	}
	if ua.unavailable == nil {
		t.Error("unavailable map 不应为 nil")
	}
}

func TestURLAvailability_MarkUnavailable(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	testURL := "https://example.com"

	ua.MarkUnavailable(testURL)

	if ua.IsAvailable(testURL) {
		t.Error("标记为不可用后 IsAvailable 应返回 false")
	}
}

func TestURLAvailability_MarkSuccess(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	testURL := "https://example.com"

	// 先标记为不可用
	ua.MarkUnavailable(testURL)
	if ua.IsAvailable(testURL) {
		t.Error("标记为不可用后应不可用")
	}

	// 标记成功后应恢复可用
	ua.MarkSuccess(testURL)
	if !ua.IsAvailable(testURL) {
		t.Error("MarkSuccess 后应恢复可用")
	}

	// 验证 lastSuccess 被设置
	ua.mu.RLock()
	if ua.lastSuccess != testURL {
		t.Errorf("lastSuccess 不匹配: got %s, want %s", ua.lastSuccess, testURL)
	}
	ua.mu.RUnlock()
}

func TestURLAvailability_IsAvailable_TTL过期(t *testing.T) {
	// 使用极短的 TTL
	ua := NewURLAvailability(1 * time.Millisecond)
	testURL := "https://example.com"

	ua.MarkUnavailable(testURL)
	// 等待 TTL 过期
	time.Sleep(5 * time.Millisecond)

	if !ua.IsAvailable(testURL) {
		t.Error("TTL 过期后 URL 应恢复可用")
	}
}

func TestURLAvailability_IsAvailable_未标记的URL(t *testing.T) {
	ua := NewURLAvailability(5 * time.Minute)
	if !ua.IsAvailable("https://never-marked.com") {
		t.Error("未标记的 URL 应默认可用")
	}
}

func TestURLAvailability_GetAvailableURLs(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)

	// 默认所有 URL 都可用
	urls := ua.GetAvailableURLs()
	if len(urls) != len(BaseURLs) {
		t.Errorf("可用 URL 数量不匹配: got %d, want %d", len(urls), len(BaseURLs))
	}
}

func TestURLAvailability_GetAvailableURLs_标记一个不可用(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)

	if len(BaseURLs) < 2 {
		t.Skip("BaseURLs 少于 2 个，跳过此测试")
	}

	ua.MarkUnavailable(BaseURLs[0])
	urls := ua.GetAvailableURLs()

	// 标记的 URL 不应出现在可用列表中
	for _, u := range urls {
		if u == BaseURLs[0] {
			t.Errorf("被标记不可用的 URL 不应出现在可用列表中: %s", BaseURLs[0])
		}
	}
}

func TestURLAvailability_GetAvailableURLsWithBase(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com", "https://c.com"}

	urls := ua.GetAvailableURLsWithBase(customURLs)
	if len(urls) != 3 {
		t.Errorf("可用 URL 数量不匹配: got %d, want 3", len(urls))
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccess优先(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com", "https://c.com"}

	ua.MarkSuccess("https://c.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	if len(urls) != 3 {
		t.Fatalf("可用 URL 数量不匹配: got %d, want 3", len(urls))
	}
	// c.com 应排在第一位
	if urls[0] != "https://c.com" {
		t.Errorf("lastSuccess 应排在第一位: got %s, want https://c.com", urls[0])
	}
	// 其余按原始顺序
	if urls[1] != "https://a.com" {
		t.Errorf("第二个应为 a.com: got %s", urls[1])
	}
	if urls[2] != "https://b.com" {
		t.Errorf("第三个应为 b.com: got %s", urls[2])
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccess不可用(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com"}

	ua.MarkSuccess("https://b.com")
	ua.MarkUnavailable("https://b.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	// b.com 被标记不可用，不应出现
	if len(urls) != 1 {
		t.Fatalf("可用 URL 数量不匹配: got %d, want 1", len(urls))
	}
	if urls[0] != "https://a.com" {
		t.Errorf("仅 a.com 应可用: got %s", urls[0])
	}
}

func TestURLAvailability_GetAvailableURLsWithBase_LastSuccess不在列表中(t *testing.T) {
	ua := NewURLAvailability(10 * time.Minute)
	customURLs := []string{"https://a.com", "https://b.com"}

	ua.MarkSuccess("https://not-in-list.com")

	urls := ua.GetAvailableURLsWithBase(customURLs)
	// lastSuccess 不在自定义列表中，不应被添加
	if len(urls) != 2 {
		t.Fatalf("可用 URL 数量不匹配: got %d, want 2", len(urls))
	}
}

// ---------------------------------------------------------------------------
// SessionStore
// ---------------------------------------------------------------------------

func TestConstants_值正确(t *testing.T) {
	if AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("AuthorizeURL 不匹配: got %s", AuthorizeURL)
	}
	if TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL 不匹配: got %s", TokenURL)
	}
	if UserInfoURL != "https://www.googleapis.com/oauth2/v2/userinfo" {
		t.Errorf("UserInfoURL 不匹配: got %s", UserInfoURL)
	}
	if ClientID != "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com" {
		t.Errorf("ClientID 不匹配: got %s", ClientID)
	}
	secret, err := getClientSecret()
	if err != nil {
		t.Fatalf("getClientSecret 应返回默认值，但报错: %v", err)
	}
	if secret != "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" {
		t.Errorf("默认 client_secret 不匹配: got %s", secret)
	}
	if RedirectURI != "http://localhost:8085/callback" {
		t.Errorf("RedirectURI 不匹配: got %s", RedirectURI)
	}
	if GetUserAgent() != "antigravity/1.23.2 windows/amd64" {
		t.Errorf("UserAgent 不匹配: got %s", GetUserAgent())
	}
	if SessionTTL != 30*time.Minute {
		t.Errorf("SessionTTL 不匹配: got %v", SessionTTL)
	}
	if URLAvailabilityTTL != 5*time.Minute {
		t.Errorf("URLAvailabilityTTL 不匹配: got %v", URLAvailabilityTTL)
	}
}

func TestScopes_包含必要范围(t *testing.T) {
	expectedScopes := []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
		"https://www.googleapis.com/auth/cclog",
		"https://www.googleapis.com/auth/experimentsandconfigs",
	}

	for _, scope := range expectedScopes {
		if !strings.Contains(Scopes, scope) {
			t.Errorf("Scopes 缺少 %s", scope)
		}
	}
}
