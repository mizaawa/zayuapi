package service

import (
	"context"
	"errors"
	"fmt"
)

const degradationMonitorAPIKeyNamePrefix = "[degradation-monitor] "

// CreateDegradationMonitorKey creates the managed key used by one degradation
// monitor. The key is bound to the monitored group and can only be used by
// requests signed by this deployment's monitor attestor.
func (s *APIKeyService) CreateDegradationMonitorKey(ctx context.Context, userID, groupID int64, monitorName string) (*APIKey, error) {
	return s.createManagedMonitorKey(ctx, userID, groupID, managedMonitorAPIKeyName(degradationMonitorAPIKeyNamePrefix, monitorName))
}

// GetManagedMonitorKey loads a managed probe key by ID. Ordinary user keys are
// refused so a monitor row can never be pointed at someone else's credential.
func (s *APIKeyService) GetManagedMonitorKey(ctx context.Context, id int64) (*APIKey, error) {
	if s == nil || s.apiKeyRepo == nil {
		return nil, fmt.Errorf("managed monitor key service is not configured")
	}
	if id <= 0 {
		return nil, ErrAPIKeyNotFound
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if apiKey == nil {
		return nil, ErrAPIKeyNotFound
	}
	if apiKey.Purpose != APIKeyPurposeChannelMonitor {
		return nil, ErrInsufficientPerms
	}
	return apiKey, nil
}

// DeleteManagedMonitorKeyByID retires a managed probe key. A key that is
// already gone is not an error; ownership and purpose are still enforced.
func (s *APIKeyService) DeleteManagedMonitorKeyByID(ctx context.Context, id, ownerID int64) error {
	if s == nil || s.apiKeyRepo == nil || id <= 0 {
		return nil
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, id)
	if errors.Is(err, ErrAPIKeyNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get managed monitor api key: %w", err)
	}
	if apiKey == nil {
		return nil
	}
	return s.DeleteChannelMonitorKey(ctx, apiKey.Key, ownerID)
}
