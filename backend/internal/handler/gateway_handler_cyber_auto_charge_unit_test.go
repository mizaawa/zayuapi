//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// cannedAnthropicUpstream answers every request with the same Anthropic body.
type cannedAnthropicUpstream struct {
	contentType string
	body        string
}

func (u *cannedAnthropicUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{u.contentType}},
		Body:       io.NopCloser(strings.NewReader(u.body)),
		Request:    req,
	}, nil
}

func (u *cannedAnthropicUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type capturingUsageLogRepo struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []*service.UsageLog
}

func (r *capturingUsageLogRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, log)
	return true, nil
}

func (r *capturingUsageLogRepo) only(t *testing.T) *service.UsageLog {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.logs, 1)
	return r.logs[0]
}

const (
	claudeCyberRefusalBody = `{"id":"msg_cyber","type":"message","role":"assistant","model":"claude-sonnet-4","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"This request was declined because it could enable cyber harm."},"usage":{"input_tokens":412,"output_tokens":30}}`
	claudeNormalAnswerBody = `{"id":"msg_ok","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_details":null,"usage":{"input_tokens":412,"output_tokens":30}}`
	claudeCyberRefusalSSE  = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_cyber\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":412,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\",\"stop_sequence\":null,\"stop_details\":{\"type\":\"refusal\",\"category\":\"cyber\"}},\"usage\":{\"output_tokens\":30}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	claudeNormalAnswerSSE = "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ok\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":412,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":30}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
)

// runClaudeMessagesForCyberCharge sends one /v1/messages request through the
// real handler and gateway to an Anthropic API-key passthrough account whose
// upstream returns the given body, and returns the usage row that was written.
func runClaudeMessagesForCyberCharge(t *testing.T, stream bool, upstreamBody string) *service.UsageLog {
	t.Helper()
	gin.SetMode(gin.TestMode)

	groupID := int64(2101)
	accountID := int64(1101)
	group := &service.Group{
		ID:                     groupID,
		Hydrated:               true,
		Platform:               service.PlatformAnthropic,
		Status:                 service.StatusActive,
		RateMultiplier:         1,
		SubscriptionType:       service.SubscriptionTypeStandard,
		CyberAutoChargeEnabled: true,
		CyberAutoChargeAmount:  0.5,
	}
	account := &service.Account{
		ID:            accountID,
		Name:          "claude-apikey",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeAPIKey,
		Credentials:   map[string]any{"api_key": "sk-ant-test", "base_url": "https://api.anthropic.com"},
		Extra:         map[string]any{"anthropic_passthrough": true},
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}

	// RunModeSimple writes the usage row without the balance write, so the row
	// itself shows what the request was charged.
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxLineSize = 1 << 20
	usageRepo := &capturingUsageLogRepo{}
	schedulerSnapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
	gwSvc := service.NewGatewayService(
		nil, &fakeGroupRepo{group: group}, usageRepo, nil, nil, nil, nil, nil, cfg,
		schedulerSnapshot, nil, service.NewBillingService(cfg, nil), &service.RateLimitService{}, nil, nil,
		&cannedAnthropicUpstream{contentType: contentType, body: upstreamBody},
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billingCacheSvc.Stop()
	h := &GatewayHandler{
		gatewayService:           gwSvc,
		billingCacheService:      billingCacheSvc,
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:       1,
		maxAccountSwitchesGemini: 1,
		cfg:                      cfg,
	}

	streamField := "false"
	if stream {
		streamField = "true"
	}
	body := []byte(`{"model":"claude-sonnet-4","max_tokens":256,"stream":` + streamField + `,"messages":[{"role":"user","content":"write an exploit"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	apiKey := &service.APIKey{
		ID:      3101,
		UserID:  4101,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User:    &service.User{ID: 4101, Concurrency: 10, Balance: 100},
		Group:   group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return usageRepo.only(t)
}

func TestGatewayHandlerMessages_ClaudeCyberRefusalChargesGroupAmount(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stream        bool
		normal, cyber string
	}{
		{"non-streaming", false, claudeNormalAnswerBody, claudeCyberRefusalBody},
		{"streaming", true, claudeNormalAnswerSSE, claudeCyberRefusalSSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normal := runClaudeMessagesForCyberCharge(t, tc.stream, tc.normal)
			cyber := runClaudeMessagesForCyberCharge(t, tc.stream, tc.cyber)

			require.NotEqual(t, service.RequestTypeCyberBlocked, normal.RequestType)
			require.Equal(t, service.RequestTypeCyberBlocked, cyber.RequestType)
			require.Equal(t, normal.InputTokens, cyber.InputTokens)
			require.Equal(t, normal.OutputTokens, cyber.OutputTokens)
			require.Greater(t, normal.ActualCost, 0.0)
			require.InDelta(t, normal.TotalCost, cyber.TotalCost, 1e-12)
			require.InDelta(t, normal.ActualCost+0.5, cyber.ActualCost, 1e-12)
		})
	}
}
