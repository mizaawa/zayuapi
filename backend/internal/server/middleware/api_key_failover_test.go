package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failoverRoutingStub struct {
	activations int
	target      *service.Group
}

func (s *failoverRoutingStub) PrepareAPIKeyFailover(_ context.Context, key *service.APIKey) (*service.APIKey, error) {
	clone := *key
	id := s.target.ID
	clone.FailoverPrimaryGroupID = key.GroupID
	clone.GroupID = &id
	clone.Group = s.target
	clone.FailoverActive = true
	return &clone, nil
}
func (s *failoverRoutingStub) StartAPIKeyFailoverCooldown(context.Context, *service.APIKey) error {
	s.activations++
	return nil
}

func failoverTestKey() *service.APIKey {
	primary, fallback := int64(1), int64(2)
	return &service.APIKey{
		ID: 10, UserID: 20, Status: service.StatusActive, GroupID: &primary,
		User:            &service.User{ID: 20, Status: service.StatusActive, Balance: 100},
		Group:           &service.Group{ID: primary, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1},
		FailoverEnabled: true, FailoverGroupID: &fallback, FailoverMaxRetries: 3, FailoverCooldownSeconds: 300,
	}
}

func failoverTestRouter(key *service.APIKey, stub *failoverRoutingStub, handler gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), key)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
		c.Next()
	})
	r.POST("/v1/responses", APIKeyFailover(stub, nil, &config.Config{RunMode: config.RunModeSimple}), handler)
	return r
}

func failoverTestResponse(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"hello"}`)))
	return w
}

func markFailoverTestFailure(c *gin.Context, status int, body string) {
	service.RecordAPIKeyFailoverUpstreamFailure(c.Request.Context(), status, []byte(body))
	service.SetOpsUpstreamError(c, status, "upstream failed", body)
	c.Data(status, "application/json", []byte(body))
}

func TestAPIKeyFailoverRoutesAfterExactAttemptLimit(t *testing.T) {
	key := failoverTestKey()
	stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 4}}
	var groups []int64
	r := failoverTestRouter(key, stub, func(c *gin.Context) {
		apiKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		groups = append(groups, *apiKey.GroupID)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-test","input":"hello"}`, string(body))
		if *apiKey.GroupID == 1 {
			c.Header("X-Failed-Attempt", "discard")
			markFailoverTestFailure(c, 429, `{"error":{"message":"upstream limited"}}`)
			return
		}
		group := c.Request.Context().Value(ctxkey.Group).(*service.Group)
		require.Equal(t, int64(2), group.ID)
		require.Equal(t, float64(4), group.RateMultiplier)
		subscription, _ := GetSubscriptionFromContext(c)
		require.Nil(t, subscription)
		c.JSON(200, gin.H{"ok": true})
	})
	w := failoverTestResponse(r)
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"ok":true}`, w.Body.String())
	require.Empty(t, w.Header().Get("X-Failed-Attempt"))
	require.Equal(t, []int64{1, 1, 1, 2}, groups)
	require.Equal(t, 1, stub.activations)
	require.Equal(t, int64(1), *key.GroupID)
	require.False(t, key.FailoverActive)
}

func TestAPIKeyFailoverFallbackHasSameAttemptLimit(t *testing.T) {
	key := failoverTestKey()
	stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive}}
	var groups []int64
	r := failoverTestRouter(key, stub, func(c *gin.Context) {
		apiKey, _ := GetAPIKeyFromContext(c)
		groups = append(groups, *apiKey.GroupID)
		markFailoverTestFailure(c, 503, `{"error":{"message":"unavailable"}}`)
	})
	w := failoverTestResponse(r)
	require.Equal(t, 503, w.Code)
	require.Equal(t, []int64{1, 1, 1, 2, 2, 2}, groups)
	require.Equal(t, 1, stub.activations)
}

func TestAPIKeyFailoverRetriesPostUpstreamResponseErrors(t *testing.T) {
	key := failoverTestKey()
	key.FailoverMaxRetries = 1
	stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive}}
	var groups []int64
	r := failoverTestRouter(key, stub, func(c *gin.Context) {
		current, _ := GetAPIKeyFromContext(c)
		groups = append(groups, *current.GroupID)
		if *current.GroupID == 1 {
			service.RecordAPIKeyFailoverUpstreamCall(c.Request.Context())
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid upstream response"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := failoverTestResponse(r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []int64{1, 2}, groups)
	require.Equal(t, 1, stub.activations)
}

func TestAPIKeyFailoverDoesNotRetryLocalOrMissingModelFailures(t *testing.T) {
	for _, test := range []struct {
		name     string
		upstream bool
		body     string
	}{
		{"local_quota", false, `{"error":{"message":"API key quota exhausted"}}`},
		{"model_missing", true, `{"error":{"code":"model_not_found","message":"Missing requested model"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := failoverTestKey()
			stub := &failoverRoutingStub{}
			calls := 0
			r := failoverTestRouter(key, stub, func(c *gin.Context) {
				calls++
				if test.upstream {
					markFailoverTestFailure(c, 404, test.body)
				} else {
					c.Data(403, "application/json", []byte(test.body))
					c.Abort()
				}
			})
			w := failoverTestResponse(r)
			require.Equal(t, 1, calls)
			require.Equal(t, 0, stub.activations)
			require.JSONEq(t, test.body, w.Body.String())
		})
	}
}

func TestAPIKeyFailoverDoesNotReplayCommittedStream(t *testing.T) {
	key := failoverTestKey()
	key.FailoverMaxRetries = 1
	stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive}}
	calls := 0
	r := failoverTestRouter(key, stub, func(c *gin.Context) {
		calls++
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString("data: first chunk\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
		service.RecordAPIKeyFailoverUpstreamFailure(c.Request.Context(), 502, []byte(`{"error":"stream failed"}`))
	})
	w := failoverTestResponse(r)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, stub.activations)
	require.Equal(t, "data: first chunk\n\n", w.Body.String())
}

func TestAPIKeyFailoverExcludesCustomPlatform(t *testing.T) {
	for _, platform := range []string{service.PlatformCustom, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			key := failoverTestKey()
			key.Group.Platform = platform
			stub := &failoverRoutingStub{}
			calls := 0
			r := failoverTestRouter(key, stub, func(c *gin.Context) { calls++; markFailoverTestFailure(c, 500, `{"error":"failed"}`) })
			w := failoverTestResponse(r)
			require.Equal(t, 500, w.Code)
			require.Equal(t, 1, calls)
			require.Equal(t, 0, stub.activations)
		})
	}
}

func TestAPIKeyFailoverSupportsOtherProviderGroups(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformAntigravity} {
		t.Run(platform, func(t *testing.T) {
			key := failoverTestKey()
			key.Group.Platform = platform
			key.FailoverMaxRetries = 1
			stub := &failoverRoutingStub{target: &service.Group{ID: 2, Platform: platform, Status: service.StatusActive}}
			var groups []int64
			r := failoverTestRouter(key, stub, func(c *gin.Context) {
				current, _ := GetAPIKeyFromContext(c)
				groups = append(groups, *current.GroupID)
				if *current.GroupID == 1 {
					markFailoverTestFailure(c, 503, `{"error":"upstream unavailable"}`)
					return
				}
				c.JSON(200, gin.H{"ok": true})
			})
			w := failoverTestResponse(r)
			require.Equal(t, 200, w.Code)
			require.Equal(t, []int64{1, 2}, groups)
			require.Equal(t, 1, stub.activations)
		})
	}
}

func TestAPIKeyFailoverProtocolScope(t *testing.T) {
	for _, test := range []struct {
		method   string
		path     string
		eligible bool
	}{
		{http.MethodPost, "/v1/messages", true},
		{http.MethodPost, "/antigravity/v1/messages", true},
		{http.MethodPost, "/v1/responses", true},
		{http.MethodPost, "/openai/v1/responses/compact", true},
		{http.MethodPost, "/backend-api/codex/responses/compact", true},
		{http.MethodPost, "/v1/chat/completions", true},
		{http.MethodPost, "/v1/messages/count_tokens", false},
		{http.MethodPost, "/v1/embeddings", false},
		{http.MethodPost, "/v1/images/generations", false},
		{http.MethodPost, "/v1/alpha/search", false},
		{http.MethodPost, "/v1beta/models/gemini:generateContent", false},
		{http.MethodPost, "/v1/responses/unsupported", false},
		{http.MethodGet, "/v1/responses", false},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			require.Equal(t, test.eligible, isAPIKeyFailoverCall(httptest.NewRequest(test.method, test.path, nil)))
		})
	}
}
