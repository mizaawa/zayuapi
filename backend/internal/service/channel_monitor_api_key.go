package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
)

const channelMonitorAPIKeyNamePrefix = "[channel-monitor] "

// CreateChannelMonitorKey creates an internal API key that exercises the same
// authenticated gateway path as a user key, without requiring an administrator
// to create and paste one into every monitor manually.
func (s *APIKeyService) CreateChannelMonitorKey(ctx context.Context, userID, groupID int64, monitorName string) (*APIKey, error) {
	return s.createManagedMonitorKey(ctx, userID, groupID, channelMonitorAPIKeyName(monitorName))
}

// createManagedMonitorKey creates an administrator-owned probe key bound to an
// active group. Every managed probe key shares the channel-monitor purpose so it
// inherits internal attestation, free probe billing and monitor accounting.
func (s *APIKeyService) createManagedMonitorKey(ctx context.Context, userID, groupID int64, keyName string) (*APIKey, error) {
	if s == nil || s.apiKeyRepo == nil || s.userRepo == nil || s.groupRepo == nil {
		return nil, fmt.Errorf("channel monitor api key service is not configured")
	}
	if userID <= 0 || groupID <= 0 {
		return nil, fmt.Errorf("invalid channel monitor api key owner or group")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get channel monitor api key owner: %w", err)
	}
	if user == nil || !user.IsActive() {
		return nil, ErrUserNotActive
	}
	if !user.IsAdmin() {
		return nil, ErrInsufficientPerms
	}
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get channel monitor group: %w", err)
	}
	if group == nil || group.Status != StatusActive {
		return nil, ErrChannelMonitorGroupInactive
	}

	key, err := s.GenerateKey()
	if err != nil {
		return nil, err
	}
	groupIDCopy := groupID
	apiKey := &APIKey{
		UserID:  userID,
		Key:     key,
		Name:    keyName,
		Purpose: APIKeyPurposeChannelMonitor,
		GroupID: &groupIDCopy,
		Status:  StatusAPIKeyActive,
	}
	if err := s.apiKeyRepo.Create(ctx, apiKey); err != nil {
		return nil, fmt.Errorf("create channel monitor api key: %w", err)
	}
	s.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	return apiKey, nil
}

// DeleteChannelMonitorKey removes only keys created by CreateChannelMonitorKey.
// The persisted purpose is the ownership guard; display names are intentionally
// not trusted because users can choose or edit ordinary key names.
func (s *APIKeyService) DeleteChannelMonitorKey(ctx context.Context, rawKey string, userID int64) error {
	if s == nil || s.apiKeyRepo == nil || strings.TrimSpace(rawKey) == "" {
		return nil
	}
	apiKey, err := s.apiKeyRepo.GetByKey(ctx, strings.TrimSpace(rawKey))
	if errors.Is(err, ErrAPIKeyNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get channel monitor api key: %w", err)
	}
	if apiKey == nil || apiKey.UserID != userID || apiKey.Purpose != APIKeyPurposeChannelMonitor {
		return ErrInsufficientPerms
	}
	if err := s.apiKeyRepo.DeleteWithAudit(ctx, apiKey.ID); err != nil {
		return fmt.Errorf("delete channel monitor api key: %w", err)
	}
	s.InvalidateAuthCacheByKey(ctx, apiKey.Key)
	s.lastUsedTouchL1.Delete(apiKey.ID)
	return nil
}

func channelMonitorAPIKeyName(monitorName string) string {
	return managedMonitorAPIKeyName(channelMonitorAPIKeyNamePrefix, monitorName)
}

func managedMonitorAPIKeyName(prefix, monitorName string) string {
	name := html.EscapeString(strings.TrimSpace(monitorName))
	if name == "" {
		name = "monitor"
	}
	runes := []rune(prefix + name)
	if len(runes) > maxChannelMonitorNameRunes {
		runes = runes[:maxChannelMonitorNameRunes]
	}
	return string(runes)
}
