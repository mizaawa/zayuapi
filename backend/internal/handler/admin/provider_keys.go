package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Provider-owned model API keys are resolved only on the server. The admin UI
// receives key metadata; a complete key is copied straight into account
// credentials and is never returned by these endpoints.

const (
	// providerKeyNamePrefix marks keys that this panel creates upstream.
	providerKeyNamePrefix       = "[杂鱼]"
	providerKeyBindingExtraKey  = "provider_binding"
	providerKeyPageSize         = 100
	providerKeyMaxPages         = 50
	providerKeyOperationTimeout = 25 * time.Second
	providerKeyResponseLimit    = 8 << 20
)

// providerSessionHTTPClient builds the DNS-pinned, redirect-free provider
// client for balance probes and key sessions. Tests replace it to reach a
// local TLS server.
var providerSessionHTTPClient = providerHTTPClient

var (
	errProviderKeyMissing           = errors.New("provider API key no longer exists")
	errProviderKeyUnavailable       = errors.New("provider API key is not active")
	errProviderKeySecretUnavailable = errors.New("provider did not return a complete API key")
	errProviderGroupUnavailable     = errors.New("provider group is no longer available")
	errProviderKeyListTooLarge      = errors.New("provider returned too many API keys")
	errProviderStoredLogin          = errors.New("stored provider login is unavailable")
)

// providerAPIError keeps an upstream application message, such as a refused
// group, so the operator can act on it. The message is sanitized and bounded.
type providerAPIError struct {
	status  int
	message string
}

func (e *providerAPIError) Error() string {
	return fmt.Sprintf("provider rejected the request (HTTP %d): %s", e.status, e.message)
}

// providerKeyCreateUncertainError reports a failed creation request whose
// upstream write may nevertheless have been committed.
type providerKeyCreateUncertainError struct{ cause error }

func (e *providerKeyCreateUncertainError) Error() string {
	return "provider API key creation result is unknown: " + e.cause.Error()
}

func (e *providerKeyCreateUncertainError) Unwrap() error { return e.cause }

type providerOptionView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	SiteType  string     `json:"site_type"`
	BaseURL   string     `json:"base_url"`
	Status    string     `json:"status"`
	State     string     `json:"state"`
	LastError string     `json:"last_error,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// providerImportState is the import picker's view of a provider's last
// observed connection: connected, sync_failed, pending or not_authenticated.
// Only not_authenticated keeps the provider from being chosen.
func providerImportState(record providerRecord) string {
	switch {
	case record.Status == "connected":
		return "connected"
	case record.Status == "":
		return "pending"
	case record.ErrorKind == "auth":
		return "not_authenticated"
	default:
		return "sync_failed"
	}
}

type providerKeyView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	GroupID         string   `json:"group_id"`
	GroupName       string   `json:"group_name"`
	GroupPlatform   string   `json:"group_platform,omitempty"`
	GroupMultiplier *float64 `json:"group_multiplier"`
	Status          string   `json:"status"`
	Available       bool     `json:"available"`
	// secret is the model API key. It is unexported and never serialized.
	secret string
}

type providerGroupView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Platform    string   `json:"platform,omitempty"`
	Description string   `json:"description,omitempty"`
	Multiplier  *float64 `json:"multiplier"`
	KeyName     string   `json:"key_name"`
}

type providerKeyCreateInput struct {
	GroupID string `json:"group_id"`
}

type providerCreatedKey struct {
	keyID string
	name  string
}

type providerResolvedKey struct {
	record  providerRecord
	key     providerKeyView
	baseURL string
}

// ListProviderOptions returns every provider as a compact choice for account import.
// GET /api/v1/admin/providers/options
func (h *AccountHandler) ListProviderOptions(c *gin.Context) {
	records, err := h.providerRecords(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load providers")
		return
	}
	options := make([]providerOptionView, 0, len(records))
	for _, record := range records {
		options = append(options, providerOptionView{
			ID: record.ID, Name: record.Name, SiteType: record.SiteType, BaseURL: record.BaseURL,
			Status: record.Status, State: providerImportState(record), LastError: record.LastError, UpdatedAt: record.UpdatedAt,
		})
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, options)
}

// ListProviderKeys returns the provider account's model API keys without secrets.
// GET /api/v1/admin/providers/:id/keys
func (h *AccountHandler) ListProviderKeys(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), providerKeyOperationTimeout)
	defer cancel()
	record, err := h.providerRecordByID(ctx, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var keys []providerKeyView
	err = h.withProviderSession(ctx, record, func(session *providerSession) error {
		var listErr error
		keys, listErr = session.listKeys(ctx)
		return listErr
	})
	if err != nil {
		response.ErrorFrom(c, providerKeyError(err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{
		"provider_id": record.ID, "site_type": record.SiteType, "endpoint": record.BaseURL,
		"keys": keys, "synced_at": time.Now().UTC(),
	})
}

// ListProviderKeyGroups returns the groups that a new provider key may use.
// GET /api/v1/admin/providers/:id/groups
func (h *AccountHandler) ListProviderKeyGroups(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), providerKeyOperationTimeout)
	defer cancel()
	record, err := h.providerRecordByID(ctx, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var groups []providerGroupView
	err = h.withProviderSession(ctx, record, func(session *providerSession) error {
		var listErr error
		groups, listErr = session.listGroups(ctx)
		return listErr
	})
	if err != nil {
		response.ErrorFrom(c, providerKeyError(err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{"provider_id": record.ID, "site_type": record.SiteType, "groups": groups})
}

// CreateProviderKey creates a model API key bound to one of the provider
// account's available groups. The creation request is never retried.
// POST /api/v1/admin/providers/:id/keys
func (h *AccountHandler) CreateProviderKey(c *gin.Context) {
	var input providerKeyCreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	groupID := strings.TrimSpace(input.GroupID)
	if !validProviderGroupID(groupID) {
		response.ErrorFrom(c, infraerrors.BadRequest("PROVIDER_GROUP_INVALID", "Choose a valid provider group"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), providerKeyOperationTimeout)
	defer cancel()
	record, err := h.providerRecordByID(ctx, c.Param("id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var group providerGroupView
	var created providerCreatedKey
	err = h.withProviderSession(ctx, record, func(session *providerSession) error {
		// Only a group the account can bind right now may be used.
		groups, listErr := session.listGroups(ctx)
		if listErr != nil {
			return listErr
		}
		for _, candidate := range groups {
			if candidate.ID == groupID {
				group = candidate
				break
			}
		}
		if group.ID == "" {
			return errProviderGroupUnavailable
		}
		var createErr error
		created, createErr = session.createKey(ctx, group)
		return createErr
	})
	if err != nil {
		response.ErrorFrom(c, providerKeyError(err))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{
		"provider_id": record.ID, "created": true, "name": created.name,
		"group_id": group.ID, "key_id": created.keyID,
	})
}

// applyProviderKeyBinding turns a provider key selection into account
// credentials. The browser names the selection; it never handles the key.
func (h *AccountHandler) applyProviderKeyBinding(ctx context.Context, req *CreateAccountRequest) error {
	if strings.TrimSpace(req.ProviderID) == "" && strings.TrimSpace(req.ProviderKeyID) == "" {
		return nil
	}
	if key, _ := req.Credentials["api_key"].(string); strings.TrimSpace(key) != "" {
		return infraerrors.BadRequest("PROVIDER_BINDING_CONFLICT", "A provider import cannot also include a manual API key")
	}
	resolved, err := h.resolveProviderSelection(ctx, req.ProviderID, req.ProviderKeyID, req.Platform)
	if err != nil {
		return err
	}
	if req.Credentials == nil {
		req.Credentials = map[string]any{}
	}
	req.Credentials["api_key"] = resolved.key.secret
	req.Credentials["base_url"] = resolved.baseURL
	if req.Extra == nil {
		req.Extra = map[string]any{}
	}
	req.Extra[providerKeyBindingExtraKey] = resolved.binding()
	return nil
}

// resolveProviderSelection reads one active provider key with its complete
// secret. Stale, inactive or masked keys are refused.
func (h *AccountHandler) resolveProviderSelection(ctx context.Context, providerID, keyID, platform string) (*providerResolvedKey, error) {
	providerID, keyID = strings.TrimSpace(providerID), strings.TrimSpace(keyID)
	if providerID == "" || keyID == "" {
		return nil, infraerrors.BadRequest("PROVIDER_BINDING_INCOMPLETE", "Choose both a provider and one of its API keys")
	}
	if !validProviderKeyID(keyID) {
		return nil, infraerrors.BadRequest("PROVIDER_KEY_ID_INVALID", "Invalid provider API key")
	}
	ctx, cancel := context.WithTimeout(ctx, providerKeyOperationTimeout)
	defer cancel()
	record, err := h.providerRecordByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	var key providerKeyView
	err = h.withProviderSession(ctx, record, func(session *providerSession) error {
		var resolveErr error
		key, resolveErr = session.resolveKey(ctx, keyID)
		return resolveErr
	})
	if err != nil {
		return nil, providerKeyError(err)
	}
	return &providerResolvedKey{record: record, key: key, baseURL: providerAccountBaseURL(record.BaseURL, platform)}, nil
}

// binding is the non-secret provenance kept in account.extra.
func (r *providerResolvedKey) binding() map[string]any {
	binding := map[string]any{
		"provider_id": r.record.ID, "provider_name": r.record.Name, "site_type": r.record.SiteType,
		"key_id": r.key.ID, "key_name": r.key.Name, "group_id": r.key.GroupID, "group_name": r.key.GroupName,
	}
	if r.key.GroupMultiplier != nil {
		binding["group_multiplier"] = *r.key.GroupMultiplier
	}
	return binding
}

// providerAccountBaseURL maps a provider site origin to the base URL that each
// account platform's request builder expects.
func providerAccountBaseURL(origin, platform string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	switch platform {
	case service.PlatformGrok, service.PlatformCustom:
		// These builders append endpoint paths directly to a /v1 base.
		return origin + "/v1"
	default:
		return origin
	}
}

func (h *AccountHandler) providerRecordByID(ctx context.Context, id string) (providerRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return providerRecord{}, infraerrors.BadRequest("PROVIDER_ID_INVALID", "Invalid provider")
	}
	var records []providerRecord
	if err := h.providerSettings.LoadProviderRecords(ctx, &records); err != nil {
		return providerRecord{}, infraerrors.InternalServer("PROVIDER_LOAD_FAILED", "Failed to load providers")
	}
	for _, record := range records {
		if record.ID == id {
			return record, nil
		}
	}
	return providerRecord{}, infraerrors.NotFound("PROVIDER_NOT_FOUND", "Provider not found; refresh the provider list")
}

// providerCreateFailure classifies a failed creation request. Only a definite
// refusal proves that nothing was created; any other failure may follow a
// committed write, so the operator must refresh before trying again.
func providerCreateFailure(err error) error {
	var refused *providerAPIError
	if errors.As(err, &refused) && refused.status < http.StatusInternalServerError {
		return err
	}
	var remote *providerRemoteError
	if errors.As(err, &remote) && remote.status >= http.StatusBadRequest && remote.status < http.StatusInternalServerError {
		return err
	}
	var scope *providerScopeError
	if errors.Is(err, errProviderAuthentication) || errors.Is(err, errProviderTurnstile) || errors.As(err, &scope) {
		return err
	}
	return &providerKeyCreateUncertainError{cause: err}
}

// providerKeyError converts a provider failure into a public API error. It
// never reports 401, which the console treats as its own expired session.
func providerKeyError(err error) error {
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) {
		return err
	}
	var uncertain *providerKeyCreateUncertainError
	var scope *providerScopeError
	switch {
	case errors.As(err, &uncertain):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_KEY_CREATE_UNCERTAIN",
			providerFailureMessage(uncertain.cause)+"; refresh the key list to confirm whether the key was created before trying again")
	case errors.Is(err, errProviderKeyMissing):
		return infraerrors.BadRequest("PROVIDER_KEY_NOT_FOUND", "The selected API key no longer exists on the provider; refresh the keys and choose again")
	case errors.Is(err, errProviderKeyUnavailable):
		return infraerrors.BadRequest("PROVIDER_KEY_UNAVAILABLE", "The selected API key is disabled, expired or out of quota; choose another key")
	case errors.Is(err, errProviderKeySecretUnavailable):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_KEY_SECRET_UNAVAILABLE", "The provider did not return the complete API key")
	case errors.Is(err, errProviderGroupUnavailable):
		return infraerrors.BadRequest("PROVIDER_GROUP_UNAVAILABLE", "This group is no longer available; refresh the groups and choose again")
	case errors.Is(err, errProviderKeyListTooLarge):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_KEY_LIST_TOO_LARGE", "The provider returned too many API keys")
	case errors.Is(err, errProviderStoredLogin):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_LOGIN_UNAVAILABLE", "The saved provider login could not be read; edit the provider and sign in again")
	case errors.As(err, &scope):
		denied := infraerrors.New(http.StatusBadGateway, "PROVIDER_SCOPE_DENIED", providerPublicError(err))
		if scope.scope != "" {
			denied = denied.WithMetadata(map[string]string{"scope": scope.scope})
		}
		return denied
	case errors.Is(err, errProviderTokenExpired):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_TOKEN_EXPIRED", providerPublicError(err))
	case errors.Is(err, errProviderTokenRetired):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_TOKEN_RETIRED", providerPublicError(err))
	case errors.Is(err, errProviderAuthentication):
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_LOGIN_FAILED", providerPublicError(err))
	default:
		return infraerrors.New(http.StatusBadGateway, "PROVIDER_REMOTE_FAILED", providerFailureMessage(err))
	}
}

func providerFailureMessage(err error) string {
	var refused *providerAPIError
	if errors.As(err, &refused) {
		return "Provider rejected the request: " + refused.message
	}
	return providerPublicError(err)
}
