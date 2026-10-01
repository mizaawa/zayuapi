package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	DefaultAPIKeyFailoverMaxRetries      = 3
	DefaultAPIKeyFailoverCooldownSeconds = 300
)

var (
	ErrAPIKeyFailoverInvalid = infraerrors.BadRequest("API_KEY_FAILOVER_INVALID", "invalid API key failover configuration")
	ErrAPIKeyFailoverChanged = infraerrors.Conflict("API_KEY_FAILOVER_CHANGED", "API key failover configuration changed during this request")
)

func invalidAPIKeyFailover(message string) error {
	return infraerrors.BadRequest("API_KEY_FAILOVER_INVALID", message)
}

// Separate from APIKeyRepository so existing repository implementations remain compatible.
type apiKeyFailoverRepository interface {
	GetAPIKeyFailover(ctx context.Context, id int64) (*APIKey, error)
	StartAPIKeyFailoverCooldown(ctx context.Context, key *APIKey, now time.Time) (*time.Time, error)
}

func sameAPIKeyGroupID(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func APIKeyFailoverPlatformSupported(platform string) bool {
	return platform != "" && platform != PlatformCustom && platform != PlatformComposite
}

func (s *APIKeyService) validateAPIKeyFailover(ctx context.Context, key *APIKey) (*Group, *User, error) {
	if key.GroupID == nil || key.FailoverGroupID == nil || *key.FailoverGroupID <= 0 ||
		sameAPIKeyGroupID(key.GroupID, key.FailoverGroupID) {
		return nil, nil, invalidAPIKeyFailover("failover requires a different group from the primary group")
	}
	primary, err := s.groupRepo.GetByID(ctx, *key.GroupID)
	if err != nil {
		return nil, nil, fmt.Errorf("get primary group: %w", err)
	}
	fallback, err := s.groupRepo.GetByID(ctx, *key.FailoverGroupID)
	if err != nil {
		return nil, nil, fmt.Errorf("get failover group: %w", err)
	}
	if !APIKeyFailoverPlatformSupported(primary.Platform) || fallback.Platform != primary.Platform {
		return nil, nil, invalidAPIKeyFailover("failover groups must share the same non-Custom platform")
	}
	if !fallback.IsActive() {
		return nil, nil, invalidAPIKeyFailover("failover group must be active")
	}
	user, err := s.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if !user.IsActive() {
		return nil, nil, ErrUserNotActive
	}
	if user.IsPublicGroupBlocked(fallback.ID, fallback.IsExclusive, fallback.SubscriptionType) {
		return nil, nil, ErrGroupBlocked
	}
	if !s.canUserBindGroup(ctx, user, fallback) {
		return nil, nil, ErrGroupNotAllowed
	}
	return fallback, user, nil
}

func (s *APIKeyService) applyAPIKeyFailoverUpdate(ctx context.Context, key *APIKey, req UpdateAPIKeyRequest, fields *APIKeyUpdateFields) error {
	changed := false
	if req.FailoverMaxRetries != nil {
		if *req.FailoverMaxRetries < 1 || *req.FailoverMaxRetries > 10 {
			return invalidAPIKeyFailover("failover maximum attempts must be between 1 and 10")
		}
		changed = changed || key.FailoverMaxRetries != *req.FailoverMaxRetries
		key.FailoverMaxRetries = *req.FailoverMaxRetries
	}
	if req.FailoverCooldownSeconds != nil {
		// PostgreSQL stores this field as an integer; bound it before constructing a duration.
		if *req.FailoverCooldownSeconds < 1 || int64(*req.FailoverCooldownSeconds) > 2147483647 {
			return invalidAPIKeyFailover("failover cooldown must be a positive number of seconds")
		}
		changed = changed || key.FailoverCooldownSeconds != *req.FailoverCooldownSeconds
		key.FailoverCooldownSeconds = *req.FailoverCooldownSeconds
	}
	if req.FailoverEnabled != nil {
		changed = changed || key.FailoverEnabled != *req.FailoverEnabled
		key.FailoverEnabled = *req.FailoverEnabled
	}
	if req.FailoverGroupID != nil {
		changed = changed || !sameAPIKeyGroupID(key.FailoverGroupID, req.FailoverGroupID)
		if *req.FailoverGroupID <= 0 {
			key.FailoverGroupID = nil
		} else {
			id := *req.FailoverGroupID
			key.FailoverGroupID = &id
		}
	}
	if !changed && !fields.GroupID && !req.ReleaseFailoverCooldown {
		return nil
	}
	if key.FailoverMaxRetries == 0 {
		key.FailoverMaxRetries = DefaultAPIKeyFailoverMaxRetries
	}
	if key.FailoverCooldownSeconds == 0 {
		key.FailoverCooldownSeconds = DefaultAPIKeyFailoverCooldownSeconds
	}
	// Changing the primary platform makes the previous fallback unusable.
	if fields.GroupID && key.FailoverGroupID != nil {
		primary, err := s.groupRepo.GetByID(ctx, *key.GroupID)
		if err != nil {
			return fmt.Errorf("get primary group: %w", err)
		}
		fallback, err := s.groupRepo.GetByID(ctx, *key.FailoverGroupID)
		if err != nil && !errors.Is(err, ErrGroupNotFound) {
			return fmt.Errorf("get failover group: %w", err)
		}
		if err != nil || fallback.Platform != primary.Platform ||
			!APIKeyFailoverPlatformSupported(primary.Platform) || sameAPIKeyGroupID(key.GroupID, key.FailoverGroupID) {
			key.FailoverEnabled = false
			key.FailoverGroupID = nil
			changed = true
		}
	}
	if key.FailoverEnabled && (changed || fields.GroupID) {
		if _, _, err := s.validateAPIKeyFailover(ctx, key); err != nil {
			return err
		}
	}
	fields.FailoverConfig = changed
	if changed || fields.GroupID || req.ReleaseFailoverCooldown {
		key.FailoverCooldownUntil = nil
		key.FailoverActive = false
		fields.FailoverCooldown = true
	}
	return nil
}

func (s *APIKeyService) freshAPIKeyFailover(ctx context.Context, key *APIKey) (*APIKey, error) {
	repo, ok := s.apiKeyRepo.(apiKeyFailoverRepository)
	if !ok {
		return nil, invalidAPIKeyFailover("API key repository does not support failover")
	}
	state, err := repo.GetAPIKeyFailover(ctx, key.ID)
	if err != nil {
		return nil, err
	}
	clone := *key
	clone.Status = state.Status
	clone.GroupID = state.GroupID
	clone.FailoverPrimaryGroupID = state.GroupID
	clone.FailoverEnabled = state.FailoverEnabled
	clone.FailoverGroupID = state.FailoverGroupID
	clone.FailoverMaxRetries = state.FailoverMaxRetries
	clone.FailoverCooldownSeconds = state.FailoverCooldownSeconds
	clone.FailoverCooldownUntil = state.FailoverCooldownUntil
	clone.FailoverRevision = state.FailoverRevision
	clone.FailoverActive = false
	if !sameAPIKeyGroupID(key.GroupID, state.GroupID) {
		clone.Group = nil
		if state.GroupID != nil {
			clone.Group, err = s.groupRepo.GetByID(ctx, *state.GroupID)
			if err != nil {
				return nil, err
			}
		}
		if clone.User != nil {
			user := *clone.User
			user.UserGroupRPMOverride = nil
			if state.GroupID != nil && s.userGroupRateRepo != nil {
				user.UserGroupRPMOverride, _ = s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, key.UserID, *state.GroupID)
			}
			clone.User = &user
		}
	}
	return &clone, nil
}

// ResolveAPIKeyFailover uses authoritative cooldown state without mutating an auth-cache entry.
func (s *APIKeyService) ResolveAPIKeyFailover(ctx context.Context, key *APIKey) (*APIKey, error) {
	if key == nil || !key.FailoverEnabled {
		return key, nil
	}
	clone, err := s.freshAPIKeyFailover(ctx, key)
	if err != nil {
		return nil, err
	}
	// Auth keeps its existing local rejection status and response shape even
	// when the authoritative key state changed since the auth-cache snapshot.
	if !clone.IsActive() || clone.IsExpired() || clone.IsQuotaExhausted() || !clone.IsFailoverActive() {
		return clone, nil
	}
	return s.selectAPIKeyFailover(ctx, clone)
}

func (s *APIKeyService) selectAPIKeyFailover(ctx context.Context, key *APIKey) (*APIKey, error) {
	group, user, err := s.validateAPIKeyFailover(ctx, key)
	if err != nil {
		return nil, err
	}
	clone := *key
	clone.FailoverPrimaryGroupID = key.GroupID
	id := group.ID
	clone.GroupID = &id
	clone.Group = group
	clone.User = user
	clone.User.UserGroupRPMOverride = nil
	if s.userGroupRateRepo != nil {
		clone.User.UserGroupRPMOverride, _ = s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, key.UserID, group.ID)
	}
	clone.FailoverActive = true
	return &clone, nil
}

// PrepareAPIKeyFailover validates current configuration before a gateway changes request routing.
func (s *APIKeyService) PrepareAPIKeyFailover(ctx context.Context, key *APIKey) (*APIKey, error) {
	if key == nil || !key.FailoverEnabled || key.FailoverActive {
		return nil, ErrAPIKeyFailoverChanged
	}
	clone, err := s.freshAPIKeyFailover(ctx, key)
	if err != nil {
		return nil, err
	}
	if !clone.IsActive() || !clone.FailoverEnabled || clone.FailoverRevision != key.FailoverRevision ||
		!sameAPIKeyGroupID(key.GroupID, clone.GroupID) {
		return nil, ErrAPIKeyFailoverChanged
	}
	return s.selectAPIKeyFailover(ctx, clone)
}

// StartAPIKeyFailoverCooldown conditionally activates cooling, preserving concurrent releases and edits.
func (s *APIKeyService) StartAPIKeyFailoverCooldown(ctx context.Context, key *APIKey) error {
	if key == nil || !key.FailoverEnabled {
		return ErrAPIKeyFailoverChanged
	}
	repo, ok := s.apiKeyRepo.(apiKeyFailoverRepository)
	if !ok {
		return ErrAPIKeyFailoverInvalid
	}
	until, err := repo.StartAPIKeyFailoverCooldown(ctx, key, time.Now())
	if err != nil {
		return err
	}
	key.FailoverCooldownUntil = until
	s.InvalidateAuthCacheByKey(ctx, key.Key)
	return nil
}
