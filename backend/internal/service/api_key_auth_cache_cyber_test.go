package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Cyber charge is billed from the group restored out of the auth snapshot,
// so both fields must survive build → L2 JSON → restore.
func TestAPIKeyAuthSnapshotCyberAutoChargeRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.Group.CyberAutoChargeEnabled = true
	apiKey.Group.CyberAutoChargeAmount = 0.75

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.CyberAutoChargeEnabled)
	require.Equal(t, 0.75, materialized.Group.CyberAutoChargeAmount)
	require.Equal(t, 0.75, materialized.Group.CyberAutoCharge())
}
