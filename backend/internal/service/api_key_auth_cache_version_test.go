package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyService_RejectsV22AuthSnapshotWithoutCustomSystemPrompt(t *testing.T) {
	svc := &APIKeyService{}
	key, ok, err := svc.applyAuthCacheEntry("legacy-prompt", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 22},
	})
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, key)
}

func TestAPIKeyService_AuthSnapshotPreservesCustomSystemPrompt(t *testing.T) {
	svc := &APIKeyService{}
	source := &APIKey{
		ID: 1, UserID: 2, Key: "sk-custom-prompt", Status: StatusActive,
		CustomSystemPromptEnabled: true, CustomSystemPromptForce: true, CustomSystemPrompt: "  Project instructions\n",
		User: &User{ID: 2, Status: StatusActive},
	}
	encoded, err := json.Marshal(svc.snapshotFromAPIKey(t.Context(), source))
	require.NoError(t, err)
	var snapshot APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(encoded, &snapshot))
	got := svc.snapshotToAPIKey(source.Key, &snapshot)
	require.True(t, got.CustomSystemPromptEnabled)
	require.True(t, got.CustomSystemPromptForce)
	require.Equal(t, source.CustomSystemPrompt, got.CustomSystemPrompt)
}

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelsListConfig(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after models_list_config was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_AuthSnapshotPreservesManagedPurpose(t *testing.T) {
	svc := &APIKeyService{}
	source := &APIKey{
		ID:      1,
		UserID:  2,
		Key:     "sk-managed",
		Name:    "monitor",
		Purpose: APIKeyPurposeChannelMonitor,
		Status:  StatusActive,
		User: &User{
			ID:     2,
			Status: StatusActive,
		},
	}

	snapshot := svc.snapshotFromAPIKey(t.Context(), source)
	if snapshot == nil {
		t.Fatal("expected auth snapshot")
	}
	if snapshot.Version != apiKeyAuthSnapshotVersion {
		t.Fatalf("expected snapshot version %d, got %d", apiKeyAuthSnapshotVersion, snapshot.Version)
	}
	if snapshot.Purpose != APIKeyPurposeChannelMonitor {
		t.Fatalf("expected managed purpose in snapshot, got %q", snapshot.Purpose)
	}

	roundTrip := svc.snapshotToAPIKey(source.Key, snapshot)
	if roundTrip == nil || roundTrip.Purpose != APIKeyPurposeChannelMonitor {
		t.Fatalf("expected managed purpose after snapshot round trip, got %#v", roundTrip)
	}
}
