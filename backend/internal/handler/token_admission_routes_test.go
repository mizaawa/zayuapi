//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type tokenHandlerReceipt struct {
	r                *service.TokenBillingReservation
	dispatched, done bool
	outcome          *service.TokenBillingQuote
}

// This double models the atomic authority separately from the intentionally
// stale positive auth/billing snapshot used by the real route handlers below.
type tokenHandlerBillingRepo struct {
	service.UsageBillingRepository
	mu              sync.Mutex
	balance, frozen float64
	receipts        map[string]*tokenHandlerReceipt
	reserved        []*service.TokenBillingReservation
	commands        []*service.UsageBillingCommand
	canceled        int
	reserveCalls    int
}

func (r *tokenHandlerBillingRepo) ReserveToken(_ context.Context, hold *service.TokenBillingReservation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserveCalls++
	if hold.Quote.Command.BalanceCost > r.balance {
		return service.ErrInsufficientBalance
	}
	if r.receipts == nil {
		r.receipts = map[string]*tokenHandlerReceipt{}
	}
	r.balance -= hold.Quote.Command.BalanceCost
	r.frozen += hold.Quote.Command.BalanceCost
	r.receipts[hold.ID] = &tokenHandlerReceipt{r: hold}
	r.reserved = append(r.reserved, hold)
	return nil
}
func (r *tokenHandlerBillingRepo) DispatchToken(_ context.Context, id string, _, _ int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.receipts[id]
	if receipt == nil || receipt.done {
		return service.ErrUsageBillingRequestConflict
	}
	receipt.dispatched = true
	return nil
}
func (r *tokenHandlerBillingRepo) StageTokenOutcome(_ context.Context, id string, q *service.TokenBillingQuote) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.receipts[id]
	if receipt == nil || receipt.done {
		return service.ErrUsageBillingRequestConflict
	}
	receipt.outcome = q
	return nil
}
func (r *tokenHandlerBillingRepo) SettleToken(_ context.Context, id string) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.receipts[id]
	if receipt == nil || receipt.outcome == nil {
		return nil, service.ErrTokenBillingOutcomePending
	}
	if !receipt.done {
		r.frozen -= receipt.r.Quote.Command.BalanceCost
		r.balance += receipt.r.Quote.Command.BalanceCost - receipt.outcome.Command.BalanceCost
		r.commands = append(r.commands, &receipt.outcome.Command)
		receipt.done = true
	}
	return &service.UsageBillingApplyResult{Applied: true}, nil
}
func (r *tokenHandlerBillingRepo) ReleaseToken(_ context.Context, id string, onlyUndispatched bool) (*service.TokenBillingReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt := r.receipts[id]
	if receipt == nil || receipt.done || (onlyUndispatched && receipt.dispatched) {
		return nil, nil
	}
	r.balance += receipt.r.Quote.Command.BalanceCost
	r.frozen -= receipt.r.Quote.Command.BalanceCost
	receipt.done = true
	if !onlyUndispatched {
		r.canceled++
	}
	return receipt.r, nil
}
func (r *tokenHandlerBillingRepo) RecoverToken(context.Context, int) ([]int64, error) {
	return nil, nil
}
func (r *tokenHandlerBillingRepo) Apply(context.Context, *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	return nil, errors.New("token admission must use reservation settlement")
}

type tokenHandlerUpstream struct {
	service.HTTPUpstream
	mu          sync.Mutex
	calls       int
	status      int
	payload     string
	contentType string
	err         error
	bodies      [][]byte
	entered     chan struct{}
	proceed     chan struct{}
}

func (u *tokenHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.calls++
	u.bodies = append(u.bodies, body)
	u.mu.Unlock()
	if u.entered != nil {
		u.entered <- struct{}{}
	}
	if u.proceed != nil {
		<-u.proceed
	}
	if u.err != nil {
		return nil, u.err
	}
	status := u.status
	if status == 0 {
		status = 200
	}
	ct := u.contentType
	if ct == "" {
		ct = "application/json"
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{ct}, "X-Request-Id": []string{"upstream-token-test"}}, Body: io.NopCloser(strings.NewReader(u.payload))}, nil
}
func (u *tokenHandlerUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func tokenHandlerContext(t *testing.T, key *service.APIKey, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(t.Context())
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
	return c, recorder
}

func newTokenRouteFixture(t *testing.T, platform string, balance float64) (*OpenAIGatewayHandler, *GatewayHandler, *service.APIKey, *tokenHandlerBillingRepo, *tokenHandlerUpstream) {
	return newTokenRouteFixtureOptions(t, platform, balance, nil)
}

func newTokenRouteFixtureOptions(t *testing.T, platform string, balance float64, configure func(*config.Config, *service.Account)) (*OpenAIGatewayHandler, *GatewayHandler, *service.APIKey, *tokenHandlerBillingRepo, *tokenHandlerUpstream) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxLineSize = 1 << 20
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cacheCfg := *cfg
	cacheCfg.RunMode = config.RunModeSimple
	cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &cacheCfg, nil)
	t.Cleanup(cache.Stop)
	groupID := int64(2)
	group := &service.Group{ID: groupID, Status: service.StatusActive, Platform: platform, RateMultiplier: 1, AllowMessagesDispatch: true}
	embeddingPrice := 0.00000002
	group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"text-embedding-3-small"}, BillingMode: service.BillingModeToken, InputPrice: &embeddingPrice, OutputPrice: &embeddingPrice, CacheWritePrice: &embeddingPrice, CacheReadPrice: &embeddingPrice}}
	if platform == service.PlatformGemini {
		price := 0.000001
		group.ModelPricing = append(group.ModelPricing, service.ChannelModelPricing{Models: []string{"gemini-2.5-flash"}, BillingMode: service.BillingModeToken, InputPrice: &price, OutputPrice: &price, CacheWritePrice: &price, CacheReadPrice: &price})
	}
	key := &service.APIKey{ID: 99, UserID: 100, GroupID: &groupID, Group: group, FailoverActive: true, User: &service.User{ID: 100, Status: service.StatusActive, Balance: 10000}}
	account := service.Account{ID: 1, Name: "token-test", Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 4, Credentials: map[string]any{"api_key": "unit-fixture", "base_url": "https://api.openai.com", "openai_capabilities": []any{"responses", "chat_completions", "embeddings"}}, Extra: map[string]any{"openai_passthrough": true}}
	if platform == service.PlatformAnthropic {
		account.Credentials["base_url"] = "https://api.anthropic.com"
	}
	if platform == service.PlatformGemini {
		account.Credentials["base_url"] = "https://generativelanguage.googleapis.com"
	}
	if configure != nil {
		configure(cfg, &account)
	}
	accounts := compatPartialUsageAccountRepo{openAIImagesFailoverAccountRepo{accounts: []service.Account{account}}}
	repo := &tokenHandlerBillingRepo{balance: balance}
	upstream := &tokenHandlerUpstream{}
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 100)}
	billing := service.NewBillingService(cfg, nil)
	resolver := service.NewModelPricingResolver(nil, billing)
	openai := service.NewOpenAIGatewayService(accounts, usage, repo, nil, nil, nil, nil, cfg, nil, nil, billing, nil, cache, upstream, &service.DeferredService{}, nil, nil, resolver, nil, nil, nil, nil)
	t.Cleanup(openai.CloseOpenAIWSPool)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync})
	pool.Stop()
	keys := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg)
	oh := NewOpenAIGatewayHandler(openai, service.NewConcurrencyService(nil), cache, keys, pool, nil, nil, nil, cfg)
	gateway := service.NewGatewayService(accounts, &fakeGroupRepo{group: group}, usage, repo, nil, nil, nil, nil, cfg, nil, nil, billing, nil, cache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil, nil)
	gemini := service.NewGeminiMessagesCompatService(accounts, &fakeGroupRepo{group: group}, nil, nil, nil, nil, upstream, nil, cfg, nil)
	gh := NewGatewayHandler(gateway, openai, gemini, nil, nil, service.NewConcurrencyService(nil), cache, nil, keys, pool, nil, nil, nil, cfg, nil)
	return oh, gh, key, repo, upstream
}

func TestTokenAdmissionRoutesRejectInsufficientBeforeUpstream(t *testing.T) {
	for _, route := range []string{"openai_responses", "codex_responses", "openai_chat", "openai_messages", "embeddings", "generic_messages", "generic_chat", "generic_responses", "gemini", "gemini_messages"} {
		t.Run(route, func(t *testing.T) {
			platform := service.PlatformOpenAI
			if strings.HasPrefix(route, "generic_") {
				platform = service.PlatformAnthropic
			}
			if strings.HasPrefix(route, "gemini") {
				platform = service.PlatformGemini
			}
			oh, gh, key, repo, upstream := newTokenRouteFixture(t, platform, 0)
			model := "gpt-5.4"
			if platform == service.PlatformAnthropic {
				model = "claude-sonnet-4-5"
			}
			if platform == service.PlatformGemini {
				model = "gemini-2.5-flash"
			}
			path, body := "/v1/responses", `{"model":"`+model+`","input":"hello","max_output_tokens":8}`
			var invoke func(*gin.Context) = oh.Responses
			switch route {
			case "codex_responses":
				path = "/backend-api/codex/responses"
			case "openai_chat", "generic_chat":
				path = "/v1/chat/completions"
				body = `{"model":"` + model + `","messages":[{"role":"user","content":"hello"}],"n":3,"max_tokens":8}`
				if route == "openai_chat" {
					invoke = oh.ChatCompletions
				} else {
					invoke = gh.ChatCompletions
				}
			case "openai_messages", "generic_messages", "gemini_messages":
				path = "/v1/messages"
				body = `{"model":"` + model + `","max_tokens":8,"messages":[{"role":"user","content":"hello"}]}`
				if route == "openai_messages" {
					invoke = oh.Messages
				} else {
					invoke = gh.Messages
				}
			case "embeddings":
				path = "/v1/embeddings"
				body = `{"model":"text-embedding-3-small","input":["hello","world"]}`
				invoke = oh.Embeddings
			case "generic_responses":
				invoke = gh.Responses
			case "gemini":
				path = "/v1beta/models/" + model + ":generateContent"
				body = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":8}}`
				invoke = gh.GeminiV1BetaModels
			}
			c, recorder := tokenHandlerContext(t, key, path, body)
			if route == "gemini" {
				c.Params = gin.Params{{Key: "modelAction", Value: model + ":generateContent"}}
			}
			invoke(c)
			require.Zero(t, upstream.calls, recorder.Body.String())
			require.Equal(t, 1, repo.reserveCalls, "the durable balance authority must be consulted: %s", recorder.Body.String())
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
		})
	}
}

func TestTokenAdmissionRouteSuccessSettlesAndFailureRefunds(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "confirmed_http_failure"}[failure], func(t *testing.T) {
			oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
			upstream.payload = `{"id":"resp_token","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`
			if failure {
				upstream.status = 400
				upstream.payload = `{"error":{"type":"invalid_request_error","message":"fixture rejected"}}`
			}
			c, recorder := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","max_output_tokens":8,"input":"hello"}`)
			oh.Responses(c)
			require.Equal(t, 1, upstream.calls, recorder.Body.String())
			require.Len(t, repo.reserved, 1)
			if failure {
				require.Equal(t, 1, repo.canceled)
				require.InDelta(t, 100, repo.balance, 1e-8)
				require.Empty(t, repo.commands)
			} else {
				require.Len(t, repo.commands, 1)
				require.Positive(t, repo.commands[0].BalanceCost)
				require.Less(t, repo.commands[0].BalanceCost, repo.reserved[0].Quote.Command.BalanceCost)
				require.Less(t, repo.balance, 100.0)
			}
			require.InDelta(t, 0, repo.frozen, 1e-8)
		})
	}
}

func TestTokenAdmissionRouteUnknownTransportRetainsHold(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	upstream.err = errors.New("connection interrupted after request was sent")
	c, _ := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","input":"hello"}`)
	oh.Responses(c)
	require.Equal(t, 1, upstream.calls)
	require.Len(t, repo.reserved, 1)
	require.Zero(t, repo.canceled)
	require.Positive(t, repo.frozen)
	require.Empty(t, repo.commands)
}

func TestTokenAdmissionWorkerAndWSTurnCapabilities(t *testing.T) {
	oh, _, key, repo, _ := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	input := service.TokenAdmissionInput{Body: []byte(`{"model":"gpt-5.4","input":"hello","n":3}`), Protocol: service.TokenAdmissionResponses, Model: "gpt-5.4", APIKey: key, Account: &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, ServerHistory: true}
	var turns tokenWSTurnAdmissions
	_, err := turns.admit(t.Context(), oh.gatewayService, 1, input)
	require.NoError(t, err)
	_, err = turns.admit(t.Context(), oh.gatewayService, 1, input)
	require.NoError(t, err)
	require.Len(t, repo.reserved, 1, "transport retries must reuse one authorized hold")
	r := turns.forTurn(1)
	require.Equal(t, 4096*3, r.Quote.Usage.OutputTokens)
	ctx := service.WithTokenBillingReservation(t.Context(), r)
	var observed *service.TokenBillingReservation
	oh.submitUsageRecordTask(ctx, func(workerCtx context.Context) { observed = service.TokenBillingReservationFromContext(workerCtx) })
	require.Same(t, r, observed)
	_, err = turns.admit(t.Context(), oh.gatewayService, 2, input)
	require.NoError(t, err)
	require.Len(t, repo.reserved, 2)
	require.NotEqual(t, turns.forTurn(1).ID, turns.forTurn(2).ID)
	turns.finishAll(t.Context(), oh.gatewayService)
	require.Positive(t, repo.frozen, "dispatched work must remain owned by settlement or recovery")
}

func TestTokenAdmissionConcurrentRoutesUseDurableBalance(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	body := `{"model":"gpt-5.4","max_output_tokens":8,"input":"hello"}`
	_, q, err := oh.gatewayService.QuoteTokenAdmission(t.Context(), service.TokenAdmissionInput{Body: []byte(body), Protocol: service.TokenAdmissionResponses, Model: "gpt-5.4", APIKey: key, Account: &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}})
	require.NoError(t, err)
	repo.balance = q.Command.BalanceCost * 1.1
	upstream.entered = make(chan struct{}, 32)
	upstream.proceed = make(chan struct{})
	upstream.payload = `{"id":"resp_concurrent","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		c, _ := tokenHandlerContext(t, key, "/v1/responses", body)
		oh.Responses(c)
	}()
	select {
	case <-upstream.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first admitted request did not reach upstream")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 16)
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			c, recorder := tokenHandlerContext(t, key, "/v1/responses", body)
			oh.Responses(c)
			codes <- recorder.Code
		})
	}
	wg.Wait()
	close(codes)
	for status := range codes {
		require.Equal(t, http.StatusForbidden, status)
	}
	upstream.mu.Lock()
	require.Equal(t, 1, upstream.calls)
	upstream.mu.Unlock()
	close(upstream.proceed)
	<-firstDone
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.reserved, 1)
	require.Len(t, repo.commands, 1)
	require.Equal(t, 17, repo.reserveCalls)
	require.InDelta(t, 0, repo.frozen, 1e-8)
}

func TestTokenAdmissionQueuedUsageRetainsHoldUntilWorkerSettles(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 4, TaskTimeout: 10 * time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync})
	t.Cleanup(pool.Stop)
	oh.usageRecordWorkerPool = pool
	workerEntered, unblockWorker := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(workerEntered); <-unblockWorker })
	<-workerEntered
	upstream.payload = `{"id":"resp_queued","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`
	c, recorder := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","max_output_tokens":8,"input":"hello"}`)
	oh.Responses(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	repo.mu.Lock()
	require.Positive(t, repo.frozen)
	require.Empty(t, repo.commands)
	require.Zero(t, repo.canceled)
	repo.mu.Unlock()
	close(unblockWorker)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.commands) == 1 && repo.frozen < 1e-8
	}, 5*time.Second, 10*time.Millisecond)
}

func TestTokenAdmissionPartialSSEFailureRetainsHold(t *testing.T) {
	oh, _, key, repo, _ := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	c, _ := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","input":"hello"}`)
	_, reservation, err := beginTokenAdmission(c, oh.gatewayService, service.TokenAdmissionInput{Body: []byte(`{"model":"gpt-5.4","input":"hello"}`), Protocol: service.TokenAdmissionResponses, Model: "gpt-5.4", APIKey: key, Account: &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}})
	require.NoError(t, err)
	c.Header("Content-Type", "text/event-stream")
	_, err = c.Writer.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	require.NoError(t, err)
	c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{Kind: "http_error", UpstreamStatusCode: 500}})
	finishOpenAITokenAttempt(c, oh.gatewayService, reservation, nil, errors.New("provider failed after output"))
	require.NoError(t, oh.gatewayService.FinishTokenBilling(c.Request.Context(), reservation))
	require.Zero(t, repo.canceled)
	require.Positive(t, repo.frozen)
}

func TestTokenAdmissionAPIKeyUnknownNativeModelWorksWithEnforcedCap(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	price := 0.00001
	key.Group.ModelPricing = append(key.Group.ModelPricing, service.ChannelModelPricing{Models: []string{"gpt-6-astra"}, BillingMode: service.BillingModeToken, InputPrice: &price, OutputPrice: &price, CacheWritePrice: &price, CacheReadPrice: &price})
	upstream.payload = `{"id":"resp_gpt6","model":"gpt-6-astra","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`
	c, recorder := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-6-astra","input":"hello"}`)
	oh.Responses(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Equal(t, int64(4096), gjson.GetBytes(upstream.bodies[0], "max_output_tokens").Int())
	require.Len(t, repo.commands, 1)
}

func TestTokenAdmissionRoutePartialStreamWithoutUsageRetainsHold(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	upstream.contentType = "text/event-stream"
	upstream.payload = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial output\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_partial\",\"status\":\"failed\",\"error\":{\"type\":\"server_error\",\"message\":\"provider failed after output\"}}}\n\n"
	c, recorder := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","input":"hello","stream":true}`)
	oh.Responses(c)
	require.Contains(t, recorder.Body.String(), "partial output")
	require.Zero(t, repo.canceled)
	require.Positive(t, repo.frozen)
	require.Empty(t, repo.commands)
}

func TestTokenAdmissionKnownFreeRouteSettlesZero(t *testing.T) {
	oh, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
	key.Group.RateMultiplier = 0
	upstream.payload = `{"id":"resp_free","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}`
	c, recorder := tokenHandlerContext(t, key, "/v1/responses", `{"model":"gpt-5.4","input":"hello"}`)
	oh.Responses(c)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, upstream.calls)
	require.Len(t, repo.reserved, 1)
	require.Len(t, repo.commands, 1)
	require.Zero(t, repo.commands[0].BalanceCost)
	require.InDelta(t, 100, repo.balance, 1e-8)
}

func TestTokenAdmissionWebSocketEveryTurnBeforeForward(t *testing.T) {
	for _, initialBalance := range []float64{0, 100} {
		t.Run(fmt.Sprintf("balance_%g", initialBalance), func(t *testing.T) {
			frames := make(chan []byte, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				conn, err := coderws.Accept(w, req, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				for turn := 1; turn <= 2; turn++ {
					_, payload, err := conn.Read(req.Context())
					if err != nil {
						return
					}
					frames <- payload
					response := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_ws_token_%d","model":"gpt-5.4","usage":{"input_tokens":10,"output_tokens":2}}}`, turn)
					if err := conn.Write(req.Context(), coderws.MessageText, []byte(response)); err != nil {
						return
					}
				}
			}))
			t.Cleanup(upstream.Close)
			oh, _, key, repo, _ := newTokenRouteFixtureOptions(t, service.PlatformOpenAI, initialBalance, func(cfg *config.Config, account *service.Account) {
				account.Credentials["base_url"] = upstream.URL
				account.Extra["openai_apikey_responses_websockets_v2_enabled"] = true
				account.Extra["openai_apikey_responses_websockets_v2_mode"] = service.OpenAIWSIngressModePassthrough
				cfg.Gateway.OpenAIWS.Enabled = true
				cfg.Gateway.OpenAIWS.APIKeyEnabled = true
				cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
				cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
			})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
			})
			router.GET("/openai/v1/responses", oh.ResponsesWebSocket)
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			for turn := 1; turn <= 2; turn++ {
				require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello"}`)))
				_, _, readErr := conn.Read(ctx)
				if initialBalance == 0 {
					require.Error(t, readErr)
					require.Empty(t, frames)
					repo.mu.Lock()
					require.Equal(t, 1, repo.reserveCalls)
					require.Empty(t, repo.reserved)
					repo.mu.Unlock()
					break
				}
				require.NoError(t, readErr)
				payload := <-frames
				require.Equal(t, int64(4096), gjson.GetBytes(payload, "max_output_tokens").Int(), "every final outbound turn must retain its cap")
			}
			if initialBalance > 0 {
				require.Eventually(t, func() bool {
					repo.mu.Lock()
					defer repo.mu.Unlock()
					return len(repo.commands) == 2 && repo.frozen < 1e-8
				}, 3*time.Second, 10*time.Millisecond)
				repo.mu.Lock()
				require.Len(t, repo.reserved, 2)
				require.NotEqual(t, repo.reserved[0].ID, repo.reserved[1].ID)
				repo.mu.Unlock()
			}
		})
	}
}

func TestTokenAdmissionBufferedCompatFailureSettlesReportedUsage(t *testing.T) {
	for _, variant := range []string{"chat", "messages", "chat_eof", "messages_eof"} {
		protocol := strings.TrimSuffix(variant, "_eof")
		t.Run(variant, func(t *testing.T) {
			h, _, key, repo, upstream := newTokenRouteFixture(t, service.PlatformOpenAI, 100)
			upstream.contentType = "text/event-stream"
			upstream.payload = `data: {"type":"response.failed","response":{"id":"resp_buffered","model":"gpt-5.4","status":"failed","error":{"type":"server_error","message":"upstream failed"},"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}}` + "\n\n"
			if strings.HasSuffix(variant, "_eof") {
				upstream.payload = `data: {"type":"response.created","response":{"id":"resp_buffered","model":"gpt-5.4","usage":{"input_tokens":7,"output_tokens":2}}}` + "\n\n"
			}

			path := "/v1/chat/completions"
			if protocol == "messages" {
				path = "/v1/messages"
			}
			c, _ := tokenHandlerContext(t, key, path, `{"model":"gpt-5.4","stream":false,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
			if protocol == "messages" {
				h.Messages(c)
			} else {
				h.ChatCompletions(c)
			}
			require.Equal(t, 1, upstream.calls)
			require.Len(t, repo.reserved, 1)
			require.Len(t, repo.commands, 1)
			receipt := repo.receipts[repo.reserved[0].ID]
			require.NotNil(t, receipt.outcome)
			require.Equal(t, 7, receipt.outcome.Usage.InputTokens)
			require.Equal(t, 2, receipt.outcome.Usage.OutputTokens)
			require.True(t, receipt.done)
			require.InDelta(t, 0, repo.frozen, 1e-8)
		})
	}
}
