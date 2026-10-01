package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type apiKeyFailoverService interface {
	PrepareAPIKeyFailover(context.Context, *service.APIKey) (*service.APIKey, error)
	StartAPIKeyFailoverCooldown(context.Context, *service.APIKey) error
}

func isAPIKeyFailoverCall(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	for _, suffix := range []string{"/messages", "/responses", "/responses/compact", "/chat/completions"} {
		if strings.HasSuffix(r.URL.Path, suffix) {
			return true
		}
	}
	return false
}

func isAPIKeyFailoverSupported(key *service.APIKey) bool {
	return key != nil && !key.IsManaged() && key.FailoverEnabled && key.Group != nil &&
		service.APIKeyFailoverPlatformSupported(key.Group.Platform)
}

// APIKeyFailover buffers unsuccessful responses only. Successful and streaming
// responses keep their normal write/flush behavior and can never be replayed.
func APIKeyFailover(keys apiKeyFailoverService, subscriptions *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := GetAPIKeyFromContext(c)
		if !ok || !isAPIKeyFailoverSupported(key) || !isAPIKeyFailoverCall(c.Request) {
			c.Next()
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status := http.StatusBadRequest
			var sizeErr *http.MaxBytesError
			if errors.As(err, &sizeErr) {
				status = http.StatusRequestEntityTooLarge
			}
			AbortWithError(c, status, "INVALID_REQUEST", "Failed to read request body")
			return
		}
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request = c.Request.WithContext(service.WithAPIKeyFailoverRequest(c.Request.Context()))
		base := c.Copy()
		base.Request = c.Request.Clone(c.Request.Context())
		next := c.Handler()
		originalWriter := c.Writer
		defer func() { c.Writer = originalWriter }()
		limit := key.FailoverMaxRetries
		if limit < 1 || limit > 10 {
			limit = service.DefaultAPIKeyFailoverMaxRetries
		}
		active := key.FailoverActive
		currentKey := key
		first := true
		for {
			for attempt := 0; attempt < limit; attempt++ {
				var call *gin.Context
				if first {
					call = c
				} else {
					call = base.Copy()
					call.Request = base.Request.Clone(base.Request.Context())
					call.Set(string(ContextKeyAPIKey), currentKey)
				}
				call.Request.Body = io.NopCloser(bytes.NewReader(body))
				call.Request = call.Request.WithContext(service.WithAPIKeyFailoverAttempt(call.Request.Context()))
				writer := newAPIKeyFailoverWriter(originalWriter)
				call.Writer = writer
				if first {
					first = false
					c.Next()
				} else {
					next(call)
				}
				if call != c {
					for name, value := range call.Keys {
						c.Set(name, value)
					}
				}
				failed := apiKeyFailoverCallFailed(call, writer)
				missingModel := apiKeyFailoverModelUnavailable(call, writer)
				if !failed || missingModel || c.Request.Context().Err() != nil {
					writer.commit()
					return
				}
				if !writer.committed() && attempt+1 < limit {
					continue
				}
				if active || writer.committed() && attempt+1 < limit {
					writer.commit()
					return
				}
				fallback, prepareErr := keys.PrepareAPIKeyFailover(call.Request.Context(), currentKey)
				if prepareErr != nil {
					writer.commit()
					return
				}
				billing := base.Copy()
				billing.Request = base.Request.Clone(base.Request.Context())
				if err := setAPIKeyFailoverBilling(billing, fallback, subscriptions, cfg); err != nil {
					writer.commit()
					return
				}
				if err := keys.StartAPIKeyFailoverCooldown(call.Request.Context(), fallback); err != nil {
					writer.commit()
					return
				}
				if writer.committed() {
					return
				}
				currentKey = fallback
				active = true
				base = billing
				break
			}
		}
	}
}

func apiKeyFailoverCallFailed(c *gin.Context, writer *apiKeyFailoverWriter) bool {
	if service.HasOpsClientBusinessLimited(c) {
		return false
	}
	if service.APIKeyFailoverAttemptFailed(c.Request.Context()) {
		return true
	}
	if streamErr, ok := service.GetOpsStreamError(c); ok && streamErr.CountTowardsSLA {
		return true
	}
	if writer.Status() < 400 {
		return false
	}
	if service.APIKeyFailoverUpstreamCalled(c.Request.Context()) {
		return true
	}
	_, upstreamStatus := c.Get(service.OpsUpstreamStatusCodeKey)
	_, upstreamMessage := c.Get(service.OpsUpstreamErrorMessageKey)
	_, upstreamEvents := c.Get(service.OpsUpstreamErrorsKey)
	return upstreamStatus || upstreamMessage || upstreamEvents
}

func apiKeyFailoverModelUnavailable(c *gin.Context, writer *apiKeyFailoverWriter) bool {
	if service.APIKeyFailoverAttemptModelUnavailable(c.Request.Context()) ||
		service.APIKeyFailoverModelUnavailable(writer.Status(), writer.body.Bytes()) {
		return true
	}
	status := c.GetInt(service.OpsUpstreamStatusCodeKey)
	if status < 400 {
		status = http.StatusBadRequest
	}
	for _, name := range []string{service.OpsUpstreamErrorMessageKey, service.OpsUpstreamErrorDetailKey} {
		if service.APIKeyFailoverModelUnavailable(status, []byte(c.GetString(name))) {
			return true
		}
	}
	if value, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*service.OpsUpstreamErrorEvent); ok {
			for _, event := range events {
				if event == nil {
					continue
				}
				status := event.UpstreamStatusCode
				if status < 400 {
					status = http.StatusBadRequest
				}
				for _, body := range []string{event.UpstreamResponseBody, event.Detail, event.Message} {
					if service.APIKeyFailoverModelUnavailable(status, []byte(body)) {
						return true
					}
				}
			}
		}
	}
	return false
}

func setAPIKeyFailoverBilling(c *gin.Context, key *service.APIKey, subscriptions *service.SubscriptionService, cfg *config.Config) error {
	if key == nil || key.User == nil || key.Group == nil || !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() {
		return service.ErrAPIKeyFailoverChanged
	}
	if !validateAPIKeyGroupAllowed(key) {
		return service.ErrGroupNotAllowed
	}
	if _, _, valid := validateAPIKeyGroupAvailable(key); !valid {
		return service.ErrAPIKeyFailoverChanged
	}
	c.Set(string(ContextKeyAPIKey), key)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
	c.Set(string(ContextKeySubscription), (*service.UserSubscription)(nil))
	if cfg != nil && cfg.RunMode == config.RunModeSimple {
		return nil
	}
	if key.Group.IsSubscriptionType() {
		if subscriptions == nil {
			return service.ErrAPIKeyFailoverInvalid
		}
		subscription, err := subscriptions.GetActiveSubscription(c.Request.Context(), key.UserID, key.Group.ID)
		if err != nil {
			return err
		}
		maintenance, err := subscriptions.ValidateAndCheckLimits(subscription, key.Group)
		if maintenance {
			subscription, err = subscriptions.EnsureWindowMaintenance(c.Request.Context(), subscription)
			if err != nil {
				return err
			}
			_, err = subscriptions.ValidateAndCheckLimits(subscription, key.Group)
		}
		if err != nil {
			return err
		}
		c.Set(string(ContextKeySubscription), subscription)
		return nil
	}
	if apiKeyBalanceBelowAuthThreshold(key.User.Balance, cfg) {
		return service.ErrInsufficientBalance
	}
	return nil
}

type apiKeyFailoverWriter struct {
	gin.ResponseWriter
	mu        sync.Mutex
	header    http.Header
	status    int
	size      int
	written   bool
	forwarded bool
	body      bytes.Buffer
}

func newAPIKeyFailoverWriter(parent gin.ResponseWriter) *apiKeyFailoverWriter {
	return &apiKeyFailoverWriter{ResponseWriter: parent, header: parent.Header().Clone(), status: http.StatusOK, size: -1}
}

func (w *apiKeyFailoverWriter) Header() http.Header { return w.header }
func (w *apiKeyFailoverWriter) Status() int         { w.mu.Lock(); defer w.mu.Unlock(); return w.status }
func (w *apiKeyFailoverWriter) Size() int           { w.mu.Lock(); defer w.mu.Unlock(); return w.size }
func (w *apiKeyFailoverWriter) Written() bool       { w.mu.Lock(); defer w.mu.Unlock(); return w.written }
func (w *apiKeyFailoverWriter) committed() bool     { w.mu.Lock(); defer w.mu.Unlock(); return w.forwarded }

func (w *apiKeyFailoverWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.written && status > 0 {
		w.status = status
	}
}

func (w *apiKeyFailoverWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeaderLocked()
}

func (w *apiKeyFailoverWriter) writeHeaderLocked() {
	if w.written {
		return
	}
	w.written = true
	w.size = 0
	if w.status < 400 {
		w.commitLocked()
	}
}

func (w *apiKeyFailoverWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeaderLocked()
	w.size += len(data)
	if w.forwarded {
		return w.ResponseWriter.Write(data)
	}
	n, err := w.body.Write(data)
	if w.body.Len() > 1<<20 {
		w.commitLocked()
	}
	return n, err
}

func (w *apiKeyFailoverWriter) WriteString(value string) (int, error) { return w.Write([]byte(value)) }
func (w *apiKeyFailoverWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeaderLocked()
	if w.forwarded {
		w.ResponseWriter.Flush()
	}
}

func (w *apiKeyFailoverWriter) commit() { w.mu.Lock(); defer w.mu.Unlock(); w.commitLocked() }
func (w *apiKeyFailoverWriter) commitLocked() {
	if w.forwarded {
		return
	}
	w.forwarded = true
	parentHeader := w.ResponseWriter.Header()
	for name := range parentHeader {
		delete(parentHeader, name)
	}
	for name, values := range w.header {
		parentHeader[name] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.ResponseWriter.WriteHeaderNow()
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}
